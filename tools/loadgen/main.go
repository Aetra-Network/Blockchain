// Command loadgen is a throughput probe for a running Aetra network.
//
// It pre-signs a batch of bank transfers offline (sequences assigned locally,
// so no round-trip per tx), fires them at a node's CometBFT RPC, then reads the
// resulting blocks back to report what the chain actually accepted: txs per
// block, gas per block, and TPS measured against real block timestamps.
//
// Pre-signing is what makes the number meaningful — signing inside the send
// loop measures the signer, not the chain.
//
//	go run ./tools/loadgen --home <node-home> --from node0 --count 2000
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	l1app "github.com/sovereign-l1/l1/app"
	"github.com/sovereign-l1/l1/app/appconfig"
)

func main() {
	var (
		home        = flag.String("home", "", "node home directory (holds the test keyring)")
		node        = flag.String("node", "http://127.0.0.1:26657", "CometBFT RPC endpoint")
		api         = flag.String("api", "http://127.0.0.1:1317", "REST gateway endpoint (for account lookup)")
		chainID     = flag.String("chain-id", "-19", "chain id")
		from        = flag.String("from", "node0", "key name in the test keyring")
		to          = flag.String("to", "", "recipient address (default: self)")
		count       = flag.Int("count", 500, "number of transfers to pre-sign and send")
		amount      = flag.Int64("amount", 1, "naet per transfer")
		gasLimit    = flag.Uint64("gas", 200000, "gas limit per tx")
		fee         = flag.Int64("fee", 500000000, "fee in naet per tx")
		concurrency = flag.Int("concurrency", 64, "parallel broadcast workers")
		settle      = flag.Duration("settle", 30*time.Second, "how long to watch blocks after the last send")
	)
	flag.Parse()

	if *home == "" {
		fatal(fmt.Errorf("--home is required"))
	}
	appconfig.ConfigureSDK("aetrad")

	// A throwaway in-memory app is the supported way to obtain this chain's
	// codec/tx config with all custom interfaces registered (same trick as
	// cmd/l1d/cmd/root.go).
	tempApp := l1app.NewL1App(log.NewNopLogger(), dbm.NewMemDB(), true,
		simtestutil.NewAppOptionsWithFlagHome(l1app.DefaultNodeHome))
	txConfig := tempApp.TxConfig()

	kr, err := keyring.New("aetrad", keyring.BackendTest, *home, os.Stdin, tempApp.AppCodec())
	if err != nil {
		fatal(err)
	}
	rec, err := kr.Key(*from)
	if err != nil {
		fatal(err)
	}
	fromAddr, err := rec.GetAddress()
	if err != nil {
		fatal(err)
	}
	toAddr := fromAddr
	if *to != "" {
		if toAddr, err = sdk.AccAddressFromBech32(*to); err != nil {
			fatal(err)
		}
	}

	rpc := &rpcClient{base: strings.TrimRight(*node, "/")}
	rest := &restClient{base: strings.TrimRight(*api, "/")}
	accNum, seq, err := rest.account(fromAddr.String())
	if err != nil {
		fatal(fmt.Errorf("fetch account: %w", err))
	}
	fmt.Printf("sender=%s account=%d start_sequence=%d\n", fromAddr, accNum, seq)

	// --- pre-sign -----------------------------------------------------------
	signStart := time.Now()
	raws := make([][]byte, 0, *count)
	for i := 0; i < *count; i++ {
		raw, err := signOne(txConfig, kr, *from, fromAddr, toAddr,
			*chainID, accNum, seq+uint64(i), *amount, *gasLimit, *fee)
		if err != nil {
			fatal(fmt.Errorf("sign %d: %w", i, err))
		}
		raws = append(raws, raw)
	}
	fmt.Printf("pre-signed %d txs in %s (%.0f tx/s signing)\n",
		len(raws), time.Since(signStart).Round(time.Millisecond),
		float64(len(raws))/time.Since(signStart).Seconds())

	startHeight, err := rpc.height()
	if err != nil {
		fatal(err)
	}

	// --- fire ---------------------------------------------------------------
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
		rejected = map[string]int{}
		jobs     = make(chan []byte)
	)
	sendStart := time.Now()
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for raw := range jobs {
				code, errMsg := rpc.broadcastSync(raw)
				mu.Lock()
				if code == 0 {
					accepted++
				} else {
					key := fmt.Sprintf("code=%d %s", code, truncate(errMsg, 90))
					rejected[key]++
				}
				mu.Unlock()
			}
		}()
	}
	for _, raw := range raws {
		jobs <- raw
	}
	close(jobs)
	wg.Wait()
	sendDur := time.Since(sendStart)

	fmt.Printf("\nbroadcast: %d accepted into mempool, %d rejected, in %s (%.0f tx/s offered)\n",
		accepted, len(raws)-accepted, sendDur.Round(time.Millisecond),
		float64(len(raws))/sendDur.Seconds())
	for reason, n := range rejected {
		fmt.Printf("  rejected x%d: %s\n", n, reason)
	}

	// --- observe ------------------------------------------------------------
	fmt.Printf("\nwatching blocks from height %d for %s ...\n", startHeight, *settle)
	time.Sleep(*settle)
	endHeight, err := rpc.height()
	if err != nil {
		fatal(err)
	}
	report(rpc, startHeight, endHeight)
}

type blockStat struct {
	height  int64
	txs     int
	ts      time.Time
	gasUsed int64
}

func report(rpc *rpcClient, from, to int64) {
	stats := []blockStat{}
	for h := from; h <= to; h++ {
		b, err := rpc.block(h)
		if err != nil {
			continue
		}
		r, err := rpc.blockResults(h)
		if err != nil {
			continue
		}
		stats = append(stats, blockStat{height: h, txs: b.txs, ts: b.ts, gasUsed: r})
	}
	if len(stats) < 2 {
		fmt.Println("not enough blocks observed")
		return
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].height < stats[j].height })

	fmt.Printf("\n%-8s %-8s %-14s %s\n", "height", "txs", "gas_used", "delta_s")
	var totalTx int
	var maxTx int
	var maxGas int64
	for i, s := range stats {
		delta := ""
		if i > 0 {
			delta = fmt.Sprintf("%.3f", s.ts.Sub(stats[i-1].ts).Seconds())
		}
		fmt.Printf("%-8d %-8d %-14d %s\n", s.height, s.txs, s.gasUsed, delta)
		totalTx += s.txs
		if s.txs > maxTx {
			maxTx = s.txs
		}
		if s.gasUsed > maxGas {
			maxGas = s.gasUsed
		}
	}
	span := stats[len(stats)-1].ts.Sub(stats[0].ts).Seconds()
	nonEmpty := 0
	for _, s := range stats {
		if s.txs > 0 {
			nonEmpty++
		}
	}
	fmt.Printf("\n--- summary ---\n")
	fmt.Printf("blocks observed      : %d (%d with txs)\n", len(stats), nonEmpty)
	fmt.Printf("txs included         : %d\n", totalTx)
	fmt.Printf("max txs in one block : %d\n", maxTx)
	fmt.Printf("max gas in one block : %d\n", maxGas)
	fmt.Printf("wall span            : %.2f s\n", span)
	if span > 0 {
		fmt.Printf("sustained TPS        : %.2f\n", float64(totalTx)/span)
	}
	if nonEmpty > 0 {
		avgBlock := span / float64(len(stats)-1)
		fmt.Printf("avg block time       : %.3f s\n", avgBlock)
		fmt.Printf("peak TPS (max block) : %.2f\n", float64(maxTx)/avgBlock)
	}
}

// --- signing ---------------------------------------------------------------

func signOne(txConfig client.TxConfig, kr keyring.Keyring, keyName string,
	from, to sdk.AccAddress, chainID string, accNum, seq uint64,
	amount int64, gasLimit uint64, fee int64,
) ([]byte, error) {
	b := txConfig.NewTxBuilder()
	msg := banktypes.NewMsgSend(from, to, sdk.NewCoins(sdk.NewInt64Coin("naet", amount)))
	if err := b.SetMsgs(msg); err != nil {
		return nil, err
	}
	b.SetGasLimit(gasLimit)
	b.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("naet", fee)))

	factory := tx.Factory{}.
		WithChainID(chainID).
		WithKeybase(kr).
		WithTxConfig(txConfig).
		WithAccountNumber(accNum).
		WithSequence(seq).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)

	if err := tx.Sign(context.Background(), factory, keyName, b, true); err != nil {
		return nil, err
	}
	return txConfig.TxEncoder()(b.GetTx())
}

// --- rpc -------------------------------------------------------------------

type rpcClient struct{ base string }

func (c *rpcClient) get(path string, out any) error {
	resp, err := http.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (c *rpcClient) height() (int64, error) {
	var out struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := c.get("/status", &out); err != nil {
		return 0, err
	}
	var h int64
	fmt.Sscanf(out.Result.SyncInfo.LatestBlockHeight, "%d", &h)
	return h, nil
}

// account reads the signer's account number and sequence from the REST
// gateway. Both are needed before signing and neither changes during a run,
// so this is a single call, not a per-tx round-trip.
func (c *restClient) account(addr string) (accNum, seq uint64, err error) {
	var out struct {
		Account struct {
			AccountNumber string `json:"account_number"`
			Sequence      string `json:"sequence"`
		} `json:"account"`
	}
	if err := c.get("/cosmos/auth/v1beta1/accounts/"+addr, &out); err != nil {
		return 0, 0, err
	}
	if out.Account.AccountNumber == "" {
		return 0, 0, fmt.Errorf("account %s not found (fund it first)", addr)
	}
	fmt.Sscanf(out.Account.AccountNumber, "%d", &accNum)
	fmt.Sscanf(out.Account.Sequence, "%d", &seq)
	return accNum, seq, nil
}

type restClient struct{ base string }

func (c *restClient) get(path string, out any) error {
	resp, err := http.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (c *rpcClient) broadcastSync(raw []byte) (uint32, string) {
	var out struct {
		Result struct {
			Code uint32 `json:"code"`
			Log  string `json:"log"`
		} `json:"result"`
		Error *struct {
			Data string `json:"data"`
		} `json:"error"`
	}
	path := fmt.Sprintf("/broadcast_tx_sync?tx=0x%x", raw)
	if err := c.get(path, &out); err != nil {
		return 999, err.Error()
	}
	if out.Error != nil {
		return 998, out.Error.Data
	}
	return out.Result.Code, out.Result.Log
}

type blockInfo struct {
	txs int
	ts  time.Time
}

func (c *rpcClient) block(h int64) (blockInfo, error) {
	var out struct {
		Result struct {
			Block struct {
				Header struct {
					Time time.Time `json:"time"`
				} `json:"header"`
				Data struct {
					Txs []string `json:"txs"`
				} `json:"data"`
			} `json:"block"`
		} `json:"result"`
	}
	if err := c.get(fmt.Sprintf("/block?height=%d", h), &out); err != nil {
		return blockInfo{}, err
	}
	return blockInfo{txs: len(out.Result.Block.Data.Txs), ts: out.Result.Block.Header.Time}, nil
}

func (c *rpcClient) blockResults(h int64) (int64, error) {
	var out struct {
		Result struct {
			TxsResults []struct {
				GasUsed string `json:"gas_used"`
			} `json:"txs_results"`
		} `json:"result"`
	}
	if err := c.get(fmt.Sprintf("/block_results?height=%d", h), &out); err != nil {
		return 0, err
	}
	var total int64
	for _, r := range out.Result.TxsResults {
		var g int64
		fmt.Sscanf(r.GasUsed, "%d", &g)
		total += g
	}
	return total, nil
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "loadgen:", err)
	os.Exit(1)
}
