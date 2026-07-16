// Command poolcheck is a live verification probe for x/nominator-pool's
// bank+staking custody (deposit -> delegate -> undelegate -> settle -> claim).
// It signs and broadcasts real nominator-pool transactions against a running
// node -- the module's CLI (aetrad tx nominator-pool ...) is a stub (every
// subcommand is `RunE: cobra.NoArgs`, no tx-building logic at all), so this
// is the only way to drive it end to end short of an in-process Go test.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

func main() {
	var (
		home    = flag.String("home", "", "node home directory (holds the test keyring)")
		node    = flag.String("node", "http://127.0.0.1:26657", "CometBFT RPC endpoint")
		api     = flag.String("api", "http://127.0.0.1:1317", "REST gateway endpoint")
		chainID = flag.String("chain-id", "-19", "chain id")
		from    = flag.String("from", "node0", "key name that signs as the pool authority")
	)
	flag.Parse()
	if *home == "" {
		fatal(fmt.Errorf("--home is required"))
	}
	chainIDFlag = *chainID
	appconfig.ConfigureSDK("aetrad")

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
	authorityAddr, err := rec.GetAddress()
	if err != nil {
		fatal(err)
	}

	beneficiaryRec, err := kr.Key("beneficiary")
	if err != nil {
		fatal(fmt.Errorf("add a 'beneficiary' key to the test keyring first: %w", err))
	}
	beneficiaryAddr, err := beneficiaryRec.GetAddress()
	if err != nil {
		fatal(err)
	}

	rest := &restClient{base: strings.TrimRight(*api, "/")}
	rpc := &rpcClient{base: strings.TrimRight(*node, "/")}

	accNum, seq, err := rest.account(authorityAddr.String())
	if err != nil {
		fatal(fmt.Errorf("fetch authority account: %w", err))
	}
	fmt.Printf("authority=%s account=%d seq=%d\n", authorityAddr, accNum, seq)
	fmt.Printf("beneficiary=%s\n", beneficiaryAddr)

	poolID := fmt.Sprintf("poolcheck-%d", time.Now().UnixNano())
	validatorTargetRaw := os.Getenv("POOLCHECK_VALIDATOR_RAW")
	if validatorTargetRaw == "" {
		fatal(fmt.Errorf("set POOLCHECK_VALIDATOR_RAW to the validator's raw AE address"))
	}

	send := func(msg sdk.Msg, label string) {
		raw, err := signOne(txConfig, kr, *from, authorityAddr, accNum, seq, msg)
		if err != nil {
			fatal(fmt.Errorf("sign %s: %w", label, err))
		}
		seq++
		hashBz := sha256.Sum256(raw)
		txHash := strings.ToUpper(hex.EncodeToString(hashBz[:]))
		code, log := rpc.broadcastSync(raw)
		fmt.Printf("%s -> checkTx code=%d log=%s\n", label, code, truncate(log, 300))
		if code != 0 {
			fatal(fmt.Errorf("%s rejected by CheckTx: code=%d log=%s", label, code, log))
		}
		// broadcast_tx_sync only reports CheckTx (mempool admission) --
		// actual message execution happens later in FinalizeBlock, so poll
		// for the real per-tx result before trusting this "succeeded".
		realCode, realLog, err := rpc.waitForTx(txHash, 15*time.Second)
		if err != nil {
			fatal(fmt.Errorf("%s: never included in a block: %w", label, err))
		}
		fmt.Printf("%s -> deliver code=%d log=%s\n", label, realCode, truncate(realLog, 300))
		if realCode != 0 {
			fatal(fmt.Errorf("%s failed on execution: code=%d log=%s", label, realCode, realLog))
		}
		time.Sleep(2 * time.Second)
	}

	fundAmount := sdk.NewCoins(sdk.NewInt64Coin("naet", 200_000_000_000)) // 200 AET
	send(&banktypes.MsgSend{
		FromAddress: authorityAddr.String(),
		ToAddress:   beneficiaryAddr.String(),
		Amount:      fundAmount,
	}, "FundBeneficiary")

	send(&nominatorpooltypes.MsgCreateNominatorPool{
		Authority:         authorityAddr.String(),
		PoolID:            poolID,
		PoolOperator:      authorityAddr.String(),
		ValidatorTarget:   validatorTargetRaw,
		PoolCommissionBps: 500,
		Height:            0,
		ValidatorStatus:   "active",
	}, "CreateNominatorPool")

	depositAmount := uint64(50_000_000_000) // 50 AET
	send(&nominatorpooltypes.MsgDepositToPool{
		Authority: authorityAddr.String(),
		PoolID:    poolID,
		Delegator: beneficiaryAddr.String(),
		Amount:    depositAmount,
		Height:    0,
	}, "DepositToPool")

	fmt.Println("\n=== after deposit: checking real x/staking delegation on the pool module account ===")
	poolModuleAddr := "" // filled by querying auth module accounts below
	moduleAddrs, err := rest.moduleAccountAddress("nominator-pool")
	if err != nil {
		fmt.Println("WARN: could not query pool module account address:", err)
	} else {
		poolModuleAddr = moduleAddrs
		fmt.Println("pool module account:", poolModuleAddr)
		delegations, err := rest.delegations(poolModuleAddr)
		if err != nil {
			fmt.Println("WARN: delegation query failed:", err)
		} else {
			fmt.Println("real x/staking delegations for the pool module account:", delegations)
		}
	}

	send(&nominatorpooltypes.MsgRequestPoolWithdrawal{
		Authority:    authorityAddr.String(),
		PoolID:       poolID,
		WithdrawalID: "poolcheck-wd-1",
		Delegator:    beneficiaryAddr.String(),
		Shares:       depositAmount,
		Height:       0,
	}, "RequestPoolWithdrawal")

	beforeBal, _ := rest.balance(beneficiaryAddr.String())
	fmt.Println("\nbeneficiary balance right after withdrawal request:", beforeBal)

	fmt.Println("\nwaiting for real unbonding (20s) + settlement EndBlocker...")
	time.Sleep(40 * time.Second)

	afterBal, err := rest.balance(beneficiaryAddr.String())
	if err != nil {
		fatal(err)
	}
	fmt.Println("beneficiary balance after waiting:", afterBal)
	fmt.Println("\nDONE. Compare before/after balance -- a real payout means withdrawalCustody + the EndBlocker settlement genuinely moved money.")
}

func signOne(txConfig client.TxConfig, kr keyring.Keyring, keyName string, from sdk.AccAddress, accNum, seq uint64, msg sdk.Msg) ([]byte, error) {
	b := txConfig.NewTxBuilder()
	if err := b.SetMsgs(msg); err != nil {
		return nil, err
	}
	b.SetGasLimit(400000)
	b.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("naet", 800000000)))
	factory := tx.Factory{}.
		WithChainID(chainIDFlag).
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

var chainIDFlag string

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
	fmt.Sscanf(out.Account.AccountNumber, "%d", &accNum)
	fmt.Sscanf(out.Account.Sequence, "%d", &seq)
	return accNum, seq, nil
}

func (c *restClient) moduleAccountAddress(name string) (string, error) {
	var out struct {
		Account struct {
			BaseAccount struct {
				Address string `json:"address"`
			} `json:"base_account"`
		} `json:"account"`
	}
	if err := c.get("/cosmos/auth/v1beta1/module_accounts/"+name, &out); err != nil {
		return "", err
	}
	if out.Account.BaseAccount.Address == "" {
		return "", fmt.Errorf("empty module account address for %s", name)
	}
	return out.Account.BaseAccount.Address, nil
}

func (c *restClient) delegations(addr string) (string, error) {
	var out any
	if err := c.get("/cosmos/staking/v1beta1/delegations/"+addr, &out); err != nil {
		return "", err
	}
	bz, _ := json.Marshal(out)
	return string(bz), nil
}

func (c *restClient) balance(addr string) (string, error) {
	var out struct {
		Balances []struct {
			Denom  string `json:"denom"`
			Amount string `json:"amount"`
		} `json:"balances"`
	}
	if err := c.get("/cosmos/bank/v1beta1/balances/"+addr, &out); err != nil {
		return "", err
	}
	bz, _ := json.Marshal(out)
	return string(bz), nil
}

type rpcClient struct{ base string }

func (c *rpcClient) broadcastSync(raw []byte) (uint32, string) {
	resp, err := http.Get(fmt.Sprintf("%s/broadcast_tx_sync?tx=0x%x", c.base, raw))
	if err != nil {
		return 999, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Result struct {
			Code uint32 `json:"code"`
			Log  string `json:"log"`
		} `json:"result"`
		Error *struct {
			Data string `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 998, string(body)
	}
	if out.Error != nil {
		return 997, out.Error.Data
	}
	return out.Result.Code, out.Result.Log
}

// waitForTx polls /tx?hash= until CometBFT reports the transaction as
// included in a block, returning its REAL execution result (the code/log
// FinalizeBlock produced), not the CheckTx-only result broadcast_tx_sync
// returns.
func (c *rpcClient) waitForTx(hash string, timeout time.Duration) (uint32, string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("%s/tx?hash=0x%s", c.base, hash))
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var out struct {
				Result struct {
					TxResult struct {
						Code uint32 `json:"code"`
						Log  string `json:"log"`
					} `json:"tx_result"`
				} `json:"result"`
			}
			if json.Unmarshal(body, &out) == nil && (strings.Contains(string(body), "\"tx_result\"")) {
				return out.Result.TxResult.Code, out.Result.TxResult.Log, nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return 0, "", fmt.Errorf("timed out waiting for tx %s to be included", hash)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "poolcheck:", err)
	os.Exit(1)
}
