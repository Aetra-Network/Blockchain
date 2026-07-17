// Command netsim is a multi-workload load driver for a running Aetra network.
//
// It differs from tools/loadgen in two ways that matter:
//
//   - it drives three real workloads (bank transfers, x/nominator-pool staking
//     deposits, and x/contracts AVM deploy/execute) instead of bank sends only;
//   - it proves EXECUTION, not just mempool admission. broadcast_tx_sync only
//     reports CheckTx; the message itself runs later in FinalizeBlock and can
//     fail there with a completely different code. netsim polls /tx?hash= for a
//     sample of accepted txs and reports finalize failures separately from
//     CheckTx rejects (same technique as tools/poolcheck's waitForTx).
//
// Every tx is pre-signed offline with locally-assigned sequences, so the send
// loop measures the chain and not the signer.
//
//	go run ./tools/netsim --count 500 --workload transfer,pool,contract
//
// Workload notes (verified against this repo, not assumed):
//
//   - Direct x/staking delegation is disabled chain-wide
//     (app/stakingpolicy/msg_server.go), so staking load can only go through
//     x/nominator-pool. That module ships no CLI and no REST gateway, so its
//     messages are built and signed here in Go.
//   - MsgDepositToStakingPool routes into the official-liquid-staking path
//     (x/nominator-pool/keeper/keeper.go depositToOfficialLiquidStakingLocked,
//     which rejects any pool with OfficialLiquidStaking=false), so the pool
//     this driver creates must be an official pool, i.e.
//     MsgCreateOfficialLiquidStakingPool.
//   - A contract throw is NOT a soft receipt code: the keeper turns a non-OK
//     AVM result into an error (x/contracts/keeper/keeper.go executeContract),
//     so the whole tx fails in FinalizeBlock. The counter example's @external
//     handler is nonce-stateful, so each executing wallet gets its own contract
//     instance; see buildContractJob.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	l1app "github.com/sovereign-l1/l1/app"
	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/app/appconfig"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
	pooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

const (
	denom = "naet"
	// oneAET is 1 AET in base units (naet); DisplayExponent is 9 on this chain.
	oneAET = int64(1_000_000_000)

	workloadTransfer = "transfer"
	workloadPool     = "pool"
	workloadContract = "contract"

	// avmExecGas is the MsgExecuteExternal.GasLimit field (the AVM's own gas
	// meter, distinct from the SDK tx gas below). Bounded by the contracts
	// module's max_gas_per_execution (100000000 in this genesis).
	avmExecGas = uint64(1_000_000)

	// contractInitialBalance seeds each deployed contract so storage rent
	// (storage_rent_per_byte_block=1) cannot push it into debt mid-run --
	// executeContract turns any rent debt into a hard tx failure.
	contractInitialBalance = uint64(100) * uint64(oneAET)
)

// Per-workload tx gas limits. These are NOT cosmetic, in both directions:
//
//   - too low and the tx dies in FinalizeBlock with "out of gas" even though
//     CheckTx happily accepted it (a pool deposit really costs ~430k, so
//     poolcheck's 400000 fails on execution);
//   - too high and x/fees' ante rejects it outright, because gas is capped at
//     max_tx_gas (1000000 on this chain, see docs/api.md:204); gasWanted is
//     also metered against max_block_gas (20000000), so a fat gas limit
//     directly lowers the achievable txs/block.
//
// The pool default carries real headroom over the observed ~430k on purpose:
// x/nominator-pool persists its WHOLE module state on every write, so a
// deposit's WritePerByte gas grows with the pools/delegators already in state.
var (
	transferGas = flag.Uint64("transfer-gas", 200_000, "tx gas limit for bank transfers")
	poolGas     = flag.Uint64("pool-gas", 900_000, "tx gas limit for nominator-pool msgs")
	contractGas = flag.Uint64("contract-gas", 1_000_000, "tx gas limit for x/contracts msgs (AVM needs the max, see docs/api.md:204)")
	feeOverride = flag.Int64("fee", 0, "flat fee in naet per tx (0 = the chain's base_fee_amount, i.e. what an ordinary wallet pays)")
)

func main() {
	defaultScratch := filepath.Join(os.TempDir(), "claude")
	var (
		scratch     = flag.String("scratch", defaultScratch, "scratch base dir holding loadnet/ (node homes + wallets keyring)")
		node        = flag.String("node", "http://127.0.0.1:26657", "CometBFT RPC endpoint")
		api         = flag.String("api", "http://127.0.0.1:1317", "REST gateway endpoint (account lookups)")
		chainID     = flag.String("chain-id", "aetra-local-1", "chain id")
		duration    = flag.Duration("duration", 0, "keep firing for this long (mutually exclusive with --count)")
		count       = flag.Int("count", 0, "total number of txs to pre-sign and fire (mutually exclusive with --duration)")
		concurrency = flag.Int("concurrency", 64, "parallel broadcast workers")
		workloadCSV = flag.String("workload", "transfer,pool,contract", "comma list: transfer,pool,contract")
		walletCount = flag.Int("wallets", 100, "how many wallet<i> keys to drive")
		contractsN  = flag.Int("contracts", 4, "contract instances to deploy (each gets one dedicated executor wallet)")
		verifyN     = flag.Int("verify-sample", 60, "how many accepted txs to poll /tx?hash= for their real FinalizeBlock result (0=all)")
		settle      = flag.Duration("settle", 20*time.Second, "how long to watch blocks after the last send")
		batch       = flag.Int("batch", 1000, "txs pre-signed per round in --duration mode")
		source      = flag.String("source", "examples/avm/counter_should_be.atlx", "contract source for the contract workload")
		seed        = flag.Int64("seed", 0, "PRNG seed (0=time-based)")
	)
	flag.Parse()

	if *count > 0 && *duration > 0 {
		fatal(errors.New("set either --count or --duration, not both"))
	}
	if *count == 0 && *duration == 0 {
		*count = 500
	}
	if *seed == 0 {
		*seed = time.Now().UnixNano()
	}

	workloads, err := parseWorkloads(*workloadCSV)
	if err != nil {
		fatal(err)
	}

	appconfig.ConfigureSDK("aetrad")

	// A throwaway in-memory app is the supported way to get this chain's
	// codec/TxConfig with every custom interface registered (cmd/l1d/cmd/root.go
	// and tools/loadgen use the same trick).
	tempApp := l1app.NewL1App(log.NewNopLogger(), dbm.NewMemDB(), true,
		simtestutil.NewAppOptionsWithFlagHome(l1app.DefaultNodeHome))

	d := &driver{
		chainID:  *chainID,
		rpc:      &rpcClient{base: strings.TrimRight(*node, "/")},
		rest:     &restClient{base: strings.TrimRight(*api, "/")},
		txConfig: tempApp.TxConfig(),
		codec:    tempApp.AppCodec(),
		rng:      rand.New(rand.NewSource(*seed)),
		stats:    newStats(),
	}

	loadnet := filepath.Join(*scratch, "loadnet")
	if err := d.loadKeys(loadnet, *walletCount, tempApp); err != nil {
		fatal(err)
	}
	fmt.Printf("netsim: chain=%s node=%s wallets=%d workloads=%s seed=%d\n",
		d.chainID, *node, len(d.wallets), strings.Join(workloads, ","), *seed)

	if err := d.loadFeeParams(); err != nil {
		fatal(err)
	}
	if err := d.prefetchAccounts(); err != nil {
		fatal(err)
	}

	// --- setup ---------------------------------------------------------------
	// Anything the load depends on (stored code, deployed contracts, the pool)
	// is created and CONFIRMED here, before a single load tx is signed, so the
	// load phase never races its own prerequisites.
	if hasWorkload(workloads, workloadContract) {
		if err := d.setupContracts(*source, *contractsN); err != nil {
			fmt.Fprintf(os.Stderr, "netsim: contract workload disabled: %v\n", err)
			workloads = dropWorkload(workloads, workloadContract)
		}
	}
	if hasWorkload(workloads, workloadPool) {
		if err := d.setupPool(); err != nil {
			fmt.Fprintf(os.Stderr, "netsim: pool workload disabled: %v\n", err)
			workloads = dropWorkload(workloads, workloadPool)
		}
	}
	if len(workloads) == 0 {
		fatal(errors.New("no workload survived setup"))
	}

	startHeight, err := d.rpc.height()
	if err != nil {
		fatal(err)
	}

	// --- fire ----------------------------------------------------------------
	sendStart := time.Now()
	if *count > 0 {
		d.runRound(workloads, *count, *concurrency)
	} else {
		deadline := time.Now().Add(*duration)
		for time.Now().Before(deadline) {
			d.runRound(workloads, *batch, *concurrency)
		}
	}
	sendDur := time.Since(sendStart)
	fmt.Printf("\noffered %d txs in %s (%.0f tx/s offered)\n",
		d.stats.total(), sendDur.Round(time.Millisecond),
		float64(d.stats.total())/sendDur.Seconds())

	// --- verify --------------------------------------------------------------
	fmt.Printf("\nsettling %s before reading real execution results ...\n", *settle)
	time.Sleep(*settle)
	d.verify(*verifyN, *concurrency)

	endHeight, err := d.rpc.height()
	if err != nil {
		fatal(err)
	}
	report(d.rpc, startHeight, endHeight)
	d.stats.report()
}

// --- driver ----------------------------------------------------------------

type wallet struct {
	name   string
	kr     keyring.Keyring
	addr   sdk.AccAddress
	ae     string // AE… user-facing form: required by AVM/pool msg FIELDS
	raw    string // ae1… bech32 form: bank/staking/REST, and the pool Authority
	accNum uint64
	seq    uint64 // next sequence to sign with (local counter, never re-fetched)
}

type contractInstance struct {
	addressAE string
	// nonce is the value the NEXT Touch must carry. The counter example's
	// @external handler asserts msg.nonce == st.nonce+1, so this is tracked
	// per contract and each contract is driven by exactly one wallet.
	nonce uint32
	owner *wallet
}

type driver struct {
	chainID  string
	rpc      *rpcClient
	rest     *restClient
	txConfig client.TxConfig
	codec    codec.Codec

	wallets []*wallet
	node0   *wallet

	// fee is the flat per-tx fee, and maxTxGas the chain's hard gas ceiling;
	// both are read from the chain rather than guessed. See loadFeeParams.
	fee      int64
	maxTxGas uint64

	poolID    string
	contracts []*contractInstance
	// touchBody encodes a Touch{nonce} body with the contract's own canonical
	// ABI codec; hand-written bytes are not the ATLX wire format.
	touchBody   func(nonce uint32) ([]byte, error)
	touchOpcode uint32

	rng   *rand.Rand
	stats *stats
}

func (d *driver) loadKeys(loadnet string, walletCount int, tempApp *l1app.L1App) error {
	walletHome := filepath.Join(loadnet, "wallets")
	kr, err := keyring.New("aetrad", keyring.BackendTest, walletHome, os.Stdin, tempApp.AppCodec())
	if err != nil {
		return fmt.Errorf("open wallet keyring %s: %w", walletHome, err)
	}
	for i := 0; i < walletCount; i++ {
		name := fmt.Sprintf("wallet%d", i)
		w, err := newWallet(kr, name)
		if err != nil {
			return err
		}
		d.wallets = append(d.wallets, w)
	}
	if len(d.wallets) == 0 {
		return errors.New("no wallet keys found")
	}

	nodeHome := filepath.Join(loadnet, "node0", "aetrad")
	nodeKR, err := keyring.New("aetrad", keyring.BackendTest, nodeHome, os.Stdin, tempApp.AppCodec())
	if err != nil {
		return fmt.Errorf("open node0 keyring %s: %w", nodeHome, err)
	}
	d.node0, err = newWallet(nodeKR, "node0")
	return err
}

func newWallet(kr keyring.Keyring, name string) (*wallet, error) {
	rec, err := kr.Key(name)
	if err != nil {
		return nil, fmt.Errorf("key %q: %w", name, err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		return nil, err
	}
	ae, err := addressing.FormatUserFriendly(addr.Bytes())
	if err != nil {
		return nil, err
	}
	return &wallet{name: name, kr: kr, addr: addr, ae: ae, raw: addressing.Format(addr.Bytes())}, nil
}

// loadFeeParams reads the live x/fees params so the driver never guesses a
// fee. The admissible band is [RequiredFee, MaxFee] (x/fees/types/fee_model.go
// ValidateAdmission), and RequiredFee climbs quadratically toward MaxFee as the
// block fills (DynamicFeeAmount) -- so a flat "cheap" fee that sails through an
// idle chain gets rejected by the ante exactly when the load ramps up, which
// would silently cap the measured throughput. Paying MaxFee is admissible at
// every congestion level, so fee pressure can never be mistaken for a chain
// limit.
//
// NOTE: the documented GET /fees/estimate?gas=N (docs/api.md:199) answers
// 12/Not Implemented on this gateway; /l1/fees/v1/params does work.
func (d *driver) loadFeeParams() error {
	params, err := d.rest.feeParams()
	if err != nil {
		return fmt.Errorf("read /l1/fees/v1/params: %w", err)
	}
	d.maxTxGas = params.maxTxGas
	// Pay what an ordinary wallet pays: the chain's base fee (0.4 AET), not its
	// max fee (5 AET).
	//
	// Defaulting to max_fee_amount made every tx admissible at any congestion,
	// which is convenient for a load driver and ruinous for a measurement: the
	// fee is burned 50/50, so a run reported ~12.5x the real burn and made the
	// economy look like it destroys its whole supply in weeks. Under congestion
	// a base-fee tx can be rejected -- that is the real user experience and
	// belongs in the numbers, not papered over. Use --fee to override.
	d.fee = params.baseFee
	if *feeOverride > 0 {
		d.fee = *feeOverride
	}
	if d.fee <= 0 {
		d.fee = params.maxFee
	}
	for name, gas := range map[string]uint64{
		"--transfer-gas": *transferGas,
		"--pool-gas":     *poolGas,
		"--contract-gas": *contractGas,
	} {
		if d.maxTxGas > 0 && gas > d.maxTxGas {
			return fmt.Errorf("%s=%d exceeds the chain's max_tx_gas=%d", name, gas, d.maxTxGas)
		}
	}
	fmt.Printf("fees: fee=%dnaet (base=%d max=%d) max_tx_gas=%d max_block_gas=%d\n",
		d.fee, params.baseFee, params.maxFee, params.maxTxGas, params.maxBlockGas)
	return nil
}

// prefetchAccounts reads every signer's account number and sequence ONCE.
// Sequences are then assigned locally, so the send loop never round-trips.
func (d *driver) prefetchAccounts() error {
	all := append([]*wallet{d.node0}, d.wallets...)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []string
		sem  = make(chan struct{}, 16)
	)
	start := time.Now()
	for _, w := range all {
		wg.Add(1)
		go func(w *wallet) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			accNum, seq, err := d.rest.account(w.raw)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", w.name, err))
				return
			}
			w.accNum, w.seq = accNum, seq
		}(w)
	}
	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("prefetch accounts: %s", strings.Join(errs, "; "))
	}
	fmt.Printf("prefetched %d accounts in %s\n", len(all), time.Since(start).Round(time.Millisecond))
	return nil
}

// --- signing ---------------------------------------------------------------

func (d *driver) sign(w *wallet, msg sdk.Msg, gas uint64, fee int64) (raw []byte, hash string, err error) {
	b := d.txConfig.NewTxBuilder()
	if err := b.SetMsgs(msg); err != nil {
		return nil, "", err
	}
	b.SetGasLimit(gas)
	b.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(denom, fee)))
	factory := tx.Factory{}.
		WithChainID(d.chainID).
		WithKeybase(w.kr).
		WithTxConfig(d.txConfig).
		WithAccountNumber(w.accNum).
		WithSequence(w.seq).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT)
	if err := tx.Sign(context.Background(), factory, w.name, b, true); err != nil {
		return nil, "", err
	}
	raw, err = d.txConfig.TxEncoder()(b.GetTx())
	if err != nil {
		return nil, "", err
	}
	w.seq++
	sum := sha256.Sum256(raw)
	return raw, strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

// sendConfirmed signs, broadcasts and waits for the tx's REAL FinalizeBlock
// result. Used for setup only -- the load phase never blocks on confirmation.
func (d *driver) sendConfirmed(w *wallet, msg sdk.Msg, gas uint64, fee int64, label string) (*txResult, error) {
	raw, hash, err := d.sign(w, msg, gas, fee)
	if err != nil {
		return nil, fmt.Errorf("sign %s: %w", label, err)
	}
	code, logMsg := d.rpc.broadcastSync(raw)
	if code != 0 {
		return nil, fmt.Errorf("%s rejected by CheckTx: code=%d log=%s", label, code, truncate(logMsg, 220))
	}
	res, err := d.rpc.waitForTx(hash, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if res.code != 0 {
		return nil, fmt.Errorf("%s failed in FinalizeBlock: code=%d log=%s", label, res.code, truncate(res.log, 220))
	}
	fmt.Printf("  setup ok: %-28s tx=%s\n", label, hash[:16])
	return res, nil
}

// --- setup: contracts ------------------------------------------------------

func (d *driver) setupContracts(sourcePath string, n int) error {
	if n < 1 {
		return errors.New("--contracts must be >= 1")
	}
	if n > len(d.wallets)-1 {
		n = len(d.wallets) - 1
	}
	src, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read contract source: %w", err)
	}
	c, err := compiler.New(compiler.DefaultOptions())
	if err != nil {
		return err
	}
	res, err := c.Compile(src)
	if err != nil {
		return fmt.Errorf("compile %s: %w", sourcePath, err)
	}
	bodyCodec, ok := res.MessageBodies["Touch"]
	if !ok {
		return fmt.Errorf("%s declares no Touch @message (external entrypoint)", sourcePath)
	}
	d.touchBody = func(nonce uint32) ([]byte, error) {
		return bodyCodec.Encode(map[string]any{"nonce": nonce})
	}
	d.touchOpcode = res.MessageBodyOpcodes["Touch"]

	// One wallet owns the code. x/contracts/keeper/keeper.go instantiateContract
	// requires code.Owner == creator, and StoreCode is content-addressed
	// (CodeID = CanonicalCodeHash(bytecode)) with an upsert that REPLACES the
	// owner -- so a second wallet storing the identical bytecode would silently
	// take ownership and break the first wallet's deploys. Hence: one storer,
	// who is also the deployer of every instance.
	owner := d.wallets[0]
	fmt.Printf("\ncontract setup: source=%s code_bytes=%d owner=%s\n", sourcePath, len(res.ModuleBytes), owner.name)

	// Every wallet that touches x/contracts must first be activated. Genesis
	// provisioning is NOT enough: see activateForContracts.
	if err := d.activateForContracts(append([]*wallet{owner}, d.wallets[1:1+n]...)); err != nil {
		return err
	}

	if _, err := d.sendConfirmed(owner, &contractstypes.MsgStoreCode{
		Authority: owner.ae,
		Bytecode:  res.ModuleBytes,
	}, *contractGas, d.fee, "StoreCode"); err != nil {
		return err
	}
	codeID := canonicalCodeHash(res.ModuleBytes)

	// InitPayload is the contract's INITIAL STORAGE encoded with the contract's
	// own storage codec (see x/contracts/keeper's counter acceptance test), not
	// a message body.
	initData, err := res.StorageCodec.Encode(map[string]any{
		"counter":     int64(0),
		"owner":       owner.ae,
		"target":      owner.ae,
		"nonce":       uint32(0),
		"lastNow":     int64(0),
		"lastBalance": uint64(0),
		"lastRandom":  uint64(0),
		"pingTicket":  nil,
		"box":         nil,
		"packed":      nil,
	})
	if err != nil {
		return fmt.Errorf("encode initial storage: %w", err)
	}

	runID := time.Now().UnixNano()
	for i := 0; i < n; i++ {
		height, err := d.rpc.height()
		if err != nil {
			return err
		}
		// Never send a stale/placeholder height: the keeper stores it verbatim
		// as the contract's last-rent-charge height, so a stale value makes the
		// first rent charge see a huge block span and freeze the contract
		// (cmd/l1d/cmd/avm.go avmBroadcastHeight documents the same trap).
		res, err := d.sendConfirmed(owner, &contractstypes.MsgDeployContract{
			Creator:        owner.ae,
			CodeID:         codeID,
			ChainID:        d.chainID,
			Salt:           fmt.Sprintf("netsim-%d-%d", runID, i),
			InitPayload:    initData,
			InitialBalance: contractInitialBalance,
			Admin:          owner.ae,
			Height:         uint64(height),
		}, *contractGas, d.fee, fmt.Sprintf("DeployContract[%d]", i))
		if err != nil {
			return err
		}
		addr, err := d.deployedAddress(res)
		if err != nil {
			return fmt.Errorf("deploy[%d]: %w", i, err)
		}
		// Each instance is driven by exactly one wallet: the counter's
		// @external handler asserts msg.nonce == st.nonce+1, so two wallets
		// racing one instance would fail each other's txs on a real chain.
		d.contracts = append(d.contracts, &contractInstance{
			addressAE: addr,
			nonce:     1,
			owner:     d.wallets[1+i],
		})
		fmt.Printf("  contract[%d] %s -> executor %s\n", i, addr, d.wallets[1+i].name)
	}
	return nil
}

// activateForContracts sends MsgActivateAccount for each wallet that will
// touch x/contracts, skipping any that is already activated.
//
// This is NOT redundant with genesis funding, and the reason is a genuine
// bootstrap gap in this network's provisioning:
//
//   - genesis writes its 110 native-account records under each account's PLAIN
//     address (the 46-char AE form of the 20 address bytes);
//   - but x/contracts' gate derives the account's v2 IDENTITY first
//     (x/contracts/keeper/keeper.go ensureActiveWallet -> normalizeAccountIdentity
//     -> addressing.NormalizeToAccountIdentity, a domain-separated hash giving a
//     62-char AE form) and looks THAT up;
//   - and the runtime activation path records accounts under exactly that
//     identity (x/native-account/types/activation.go ActivationAddressPair ->
//     addressing.DeriveAccountAddress).
//
// So the genesis records and the lookup key live in two different key spaces,
// and every genesis-provisioned account reads back as
// "contracts_account_inactive" until it is activated for real. MsgActivateAccount
// closes that gap; it is the same call a real wallet makes.
func (d *driver) activateForContracts(wallets []*wallet) error {
	activated := 0
	for _, w := range wallets {
		rec, err := w.kr.Key(w.name)
		if err != nil {
			return err
		}
		pubKey, err := rec.GetPubKey()
		if err != nil {
			return err
		}
		msg, err := nativeaccounttypes.NewMsgActivateAccountFromPubKey(pubKey, 0)
		if err != nil {
			return fmt.Errorf("build activation for %s: %w", w.name, err)
		}
		if _, err := d.sendConfirmed(w, &msg, *transferGas, d.fee, "ActivateAccount["+w.name+"]"); err != nil {
			// An account activated by an earlier run is already in the identity
			// key space, which is all this step needs.
			if strings.Contains(err.Error(), "already active") {
				continue
			}
			return err
		}
		activated++
	}
	fmt.Printf("  activated %d/%d wallets into the v2-identity key space\n", activated, len(wallets))
	return nil
}

// deployedAddress pulls the contract address out of a confirmed deploy tx.
// instantiateContract emits no address-bearing ABCI event, so the packed
// MsgDeployContractResponse in the tx's data blob is the only source
// (cmd/l1d/cmd/avm.go decodeDeployContractAddress does the same).
func (d *driver) deployedAddress(res *txResult) (string, error) {
	if len(res.data) == 0 {
		return "", errors.New("confirmed deploy tx carried no data blob")
	}
	var txMsgData sdk.TxMsgData
	if err := d.codec.Unmarshal(res.data, &txMsgData); err != nil {
		return "", fmt.Errorf("decode tx data: %w", err)
	}
	for _, msgResponse := range txMsgData.MsgResponses {
		if msgResponse == nil || msgResponse.TypeUrl != "/l1.contracts.v1.MsgDeployContractResponse" {
			continue
		}
		var deployResp contractstypes.InstantiateContractResponse
		if err := d.codec.Unmarshal(msgResponse.Value, &deployResp); err != nil {
			return "", fmt.Errorf("decode deploy response: %w", err)
		}
		if deployResp.ContractAddressUser == "" {
			return "", errors.New("deploy response carried an empty contract address")
		}
		return deployResp.ContractAddressUser, nil
	}
	return "", errors.New("no MsgDeployContractResponse in tx data")
}

func canonicalCodeHash(bytecode []byte) string {
	sum := sha256.Sum256(append([]byte("aetra-avm-code-v1/"), bytecode...))
	return hex.EncodeToString(sum[:])
}

// --- setup: pool -----------------------------------------------------------

func (d *driver) setupPool() error {
	// MsgDepositToStakingPool only accepts an OFFICIAL liquid-staking pool
	// (keeper.go depositToOfficialLiquidStakingLocked), so the pool is created
	// with MsgCreateOfficialLiquidStakingPool. NOTE: that message carries no
	// validator_target field at all (x/nominator-pool/types/state.go
	// MsgCreateOfficialLiquidStakingPool), so an official pool cannot name a
	// validator; only the plain MsgCreateNominatorPool can, and deposits to a
	// plain pool are rejected by the deposit path above.
	//
	// The pool's Authority must be the byte-for-byte string in
	// nominator-pool Params.Authority: Params.Authorize compares raw strings
	// (state.go: `if authority != p.Authority`), and genesis stores the ae1…
	// form -- so the AE… form would be rejected despite parsing to the same
	// account.
	d.poolID = fmt.Sprintf("netsim-pool-%d", time.Now().UnixNano())

	// The pool needs a contract address pair (AE + ae1 forms of the same
	// account). The keeper never checks that the address hosts a real
	// contract, so a deterministic address derived from the pool id is enough
	// when the contract workload is off; otherwise reuse a real deployed one.
	contractAE, contractRaw := d.poolContractAddresses()

	if _, err := d.sendConfirmed(d.node0, &pooltypes.MsgCreateOfficialLiquidStakingPool{
		Authority:           d.node0.raw,
		PoolID:              d.poolID,
		ContractAddressUser: contractAE,
		ContractAddressRaw:  contractRaw,
		PoolOperator:        d.node0.raw,
		PoolCommissionBps:   500,
		Height:              0, // msg_server defaultHeight() fills the real height
		// The validator this pool delegates deposits to. node0 is a genesis
		// validator, so its account address doubles as the operator address --
		// depositCustody parses the target as an account and casts the same
		// bytes to a ValAddress. Without a target the pool would take deposits
		// it can never stake, which is why creation now rejects an empty one.
		ValidatorTarget: d.node0.raw,
	}, *poolGas, d.fee, "CreateOfficialLiquidStakingPool"); err != nil {
		return err
	}
	fmt.Printf("  pool %s created (authority=%s)\n", d.poolID, d.node0.raw)
	return nil
}

func (d *driver) poolContractAddresses() (string, string) {
	if len(d.contracts) > 0 {
		bz, err := addressing.Parse(d.contracts[0].addressAE)
		if err == nil {
			raw := addressing.Format(bz)
			return d.contracts[0].addressAE, raw
		}
	}
	sum := sha256.Sum256([]byte("netsim-pool-contract/" + d.poolID))
	bz := sum[:20]
	ae, err := addressing.FormatUserFriendly(bz)
	if err != nil {
		fatal(err)
	}
	return ae, addressing.Format(bz)
}

// --- load ------------------------------------------------------------------

type job struct {
	workload string
	raw      []byte
	hash     string
}

// runRound pre-signs n txs and then fires them. Pre-signing a whole round up
// front is the point: signing inside the send loop would measure the signer.
func (d *driver) runRound(workloads []string, n, concurrency int) {
	signStart := time.Now()
	jobs := make([]job, 0, n)
	for i := 0; i < n; i++ {
		w := workloads[i%len(workloads)]
		j, err := d.buildJob(w)
		if err != nil {
			d.stats.signFail(w, err)
			continue
		}
		jobs = append(jobs, j)
	}
	if len(jobs) == 0 {
		return
	}
	signDur := time.Since(signStart)
	fmt.Printf("pre-signed %d txs in %s (%.0f tx/s signing)\n",
		len(jobs), signDur.Round(time.Millisecond), float64(len(jobs))/signDur.Seconds())

	var wg sync.WaitGroup
	ch := make(chan job)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				code, logMsg := d.rpc.broadcastSync(j.raw)
				d.stats.checkTx(j, code, logMsg)
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}

func (d *driver) buildJob(workload string) (job, error) {
	switch workload {
	case workloadTransfer:
		return d.buildTransferJob()
	case workloadPool:
		return d.buildPoolJob()
	case workloadContract:
		return d.buildContractJob()
	}
	return job{}, fmt.Errorf("unknown workload %q", workload)
}

func (d *driver) buildTransferJob() (job, error) {
	from := d.wallets[d.rng.Intn(len(d.wallets))]
	to := from
	for to == from {
		to = d.wallets[d.rng.Intn(len(d.wallets))]
	}
	msg := banktypes.NewMsgSend(from.addr, to.addr, sdk.NewCoins(sdk.NewInt64Coin(denom, 1_000)))
	raw, hash, err := d.sign(from, msg, *transferGas, d.fee)
	if err != nil {
		return job{}, err
	}
	return job{workload: workloadTransfer, raw: raw, hash: hash}, nil
}

func (d *driver) buildPoolJob() (job, error) {
	w := d.wallets[d.rng.Intn(len(d.wallets))]
	// wallet_address is the signer field (types/signing.go
	// MsgDepositToStakingPoolSigners) and must be the AE user-facing form
	// (keeper ValidateUserFacingAEAddress). Amount must clear
	// Params.MinPoolDeposit (10 AET in this genesis).
	msg := &pooltypes.MsgDepositToStakingPool{
		PoolID:        d.poolID,
		WalletAddress: w.ae,
		Amount:        uint64(10 * oneAET),
		Height:        0, // msg_server defaultHeight() fills the real height
	}
	raw, hash, err := d.sign(w, msg, *poolGas, d.fee)
	if err != nil {
		return job{}, err
	}
	return job{workload: workloadPool, raw: raw, hash: hash}, nil
}

func (d *driver) buildContractJob() (job, error) {
	if len(d.contracts) == 0 {
		return job{}, errors.New("no contract deployed")
	}
	inst := d.contracts[d.rng.Intn(len(d.contracts))]
	body, err := d.touchBody(inst.nonce)
	if err != nil {
		return job{}, err
	}
	height, err := d.rpc.height()
	if err != nil {
		return job{}, err
	}
	msg := &contractstypes.MsgExecuteExternal{
		Sender:          inst.owner.ae,
		ContractAddress: inst.addressAE,
		Payload:         body,
		GasLimit:        avmExecGas,
		Height:          uint64(height),
		Opcode:          d.touchOpcode,
	}
	raw, hash, err := d.sign(inst.owner, msg, *contractGas, d.fee)
	if err != nil {
		return job{}, err
	}
	// The contract's stored nonce only advances if this tx actually executes,
	// but this wallet's txs are strictly ordered by account sequence, so
	// nonce N+1 follows nonce N in execution order too.
	inst.nonce++
	return job{workload: workloadContract, raw: raw, hash: hash}, nil
}

// --- verification ----------------------------------------------------------

// verify polls /tx?hash= for accepted txs and records their REAL FinalizeBlock
// result. CheckTx code 0 only means the mempool took the tx; the message runs
// later and can fail with an entirely different code, which is exactly the
// difference this driver exists to surface.
func (d *driver) verify(sample, concurrency int) {
	accepted := d.stats.acceptedJobs()
	if len(accepted) == 0 {
		fmt.Println("nothing to verify: no tx was accepted by CheckTx")
		return
	}
	if sample > 0 && sample < len(accepted) {
		d.rng.Shuffle(len(accepted), func(i, j int) { accepted[i], accepted[j] = accepted[j], accepted[i] })
		accepted = accepted[:sample]
	}
	fmt.Printf("verifying %d/%d accepted txs against their real FinalizeBlock result ...\n",
		len(accepted), d.stats.totalAccepted())

	var wg sync.WaitGroup
	ch := make(chan verifyTarget)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range ch {
				res, err := d.rpc.waitForTx(t.hash, 15*time.Second)
				if err != nil {
					d.stats.finalizeMissing(t.workload)
					continue
				}
				d.stats.finalize(t.workload, res.code, res.log)
			}
		}()
	}
	for _, t := range accepted {
		ch <- t
	}
	close(ch)
	wg.Wait()
}

type verifyTarget struct {
	workload string
	hash     string
}

// --- stats -----------------------------------------------------------------

type workloadStats struct {
	signFail        int
	checktxOK       int
	checktxReject   int
	finalizeOK      int
	finalizeFail    int
	finalizeMissing int
	checktxErrs     map[string]int
	finalizeErrs    map[string]int
	signErrs        map[string]int
}

type stats struct {
	mu       sync.Mutex
	byLoad   map[string]*workloadStats
	accepted []verifyTarget
}

func newStats() *stats { return &stats{byLoad: map[string]*workloadStats{}} }

func (s *stats) get(workload string) *workloadStats {
	ws, ok := s.byLoad[workload]
	if !ok {
		ws = &workloadStats{
			checktxErrs:  map[string]int{},
			finalizeErrs: map[string]int{},
			signErrs:     map[string]int{},
		}
		s.byLoad[workload] = ws
	}
	return ws
}

func (s *stats) signFail(workload string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := s.get(workload)
	ws.signFail++
	ws.signErrs[truncate(err.Error(), 110)]++
}

func (s *stats) checkTx(j job, code uint32, logMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := s.get(j.workload)
	if code == 0 {
		ws.checktxOK++
		s.accepted = append(s.accepted, verifyTarget{workload: j.workload, hash: j.hash})
		return
	}
	ws.checktxReject++
	ws.checktxErrs[fmt.Sprintf("code=%d %s", code, truncate(logMsg, 100))]++
}

func (s *stats) finalize(workload string, code uint32, logMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := s.get(workload)
	if code == 0 {
		ws.finalizeOK++
		return
	}
	ws.finalizeFail++
	ws.finalizeErrs[fmt.Sprintf("code=%d %s", code, truncate(logMsg, 100))]++
}

func (s *stats) finalizeMissing(workload string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.get(workload).finalizeMissing++
}

func (s *stats) acceptedJobs() []verifyTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]verifyTarget(nil), s.accepted...)
}

func (s *stats) totalAccepted() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.accepted)
}

func (s *stats) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, ws := range s.byLoad {
		n += ws.checktxOK + ws.checktxReject
	}
	return n
}

func (s *stats) report() {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.byLoad))
	for name := range s.byLoad {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Printf("\n--- by workload ---\n")
	fmt.Printf("%-10s %-12s %-16s %-13s %-14s %s\n",
		"workload", "checktx_ok", "checktx_reject", "finalize_ok", "finalize_fail", "unconfirmed")
	for _, name := range names {
		ws := s.byLoad[name]
		fmt.Printf("%-10s %-12d %-16d %-13d %-14d %d\n",
			name, ws.checktxOK, ws.checktxReject, ws.finalizeOK, ws.finalizeFail, ws.finalizeMissing)
	}
	fmt.Println("\nfinalize_ok/fail count only the verified sample (--verify-sample); " +
		"checktx counts cover every tx. A CheckTx accept is mempool admission, NOT execution.")

	for _, name := range names {
		ws := s.byLoad[name]
		printErrs(name, "sign errors", ws.signErrs)
		printErrs(name, "CheckTx rejects", ws.checktxErrs)
		printErrs(name, "FinalizeBlock failures", ws.finalizeErrs)
	}
}

func printErrs(workload, title string, errs map[string]int) {
	if len(errs) == 0 {
		return
	}
	type kv struct {
		msg string
		n   int
	}
	list := make([]kv, 0, len(errs))
	for msg, n := range errs {
		list = append(list, kv{msg, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	if len(list) > 5 {
		list = list[:5]
	}
	fmt.Printf("\ntop %s [%s]:\n", title, workload)
	for _, e := range list {
		fmt.Printf("  x%-6d %s\n", e.n, e.msg)
	}
}

// --- block report ----------------------------------------------------------

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
		gas, err := rpc.blockResults(h)
		if err != nil {
			continue
		}
		stats = append(stats, blockStat{height: h, txs: b.txs, ts: b.ts, gasUsed: gas})
	}
	if len(stats) < 2 {
		fmt.Println("not enough blocks observed")
		return
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].height < stats[j].height })

	fmt.Printf("\n%-8s %-8s %-14s %s\n", "height", "txs", "gas_used", "delta_s")
	var (
		totalTx int
		maxTx   int
		maxGas  int64
		peakTPS float64
	)
	for i, s := range stats {
		delta := ""
		if i > 0 {
			dt := s.ts.Sub(stats[i-1].ts).Seconds()
			delta = fmt.Sprintf("%.3f", dt)
			if dt > 0 && float64(s.txs)/dt > peakTPS {
				peakTPS = float64(s.txs) / dt
			}
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
	fmt.Printf("\n--- chain ---\n")
	fmt.Printf("blocks observed      : %d (%d with txs)\n", len(stats), nonEmpty)
	fmt.Printf("txs included         : %d\n", totalTx)
	fmt.Printf("max txs in one block : %d\n", maxTx)
	fmt.Printf("max gas in one block : %d\n", maxGas)
	fmt.Printf("wall span            : %.2f s\n", span)
	if span > 0 {
		fmt.Printf("sustained TPS        : %.2f\n", float64(totalTx)/span)
	}
	if len(stats) > 1 {
		fmt.Printf("avg block time       : %.3f s\n", span/float64(len(stats)-1))
	}
	fmt.Printf("peak TPS (one block) : %.2f\n", peakTPS)
}

// --- rpc / rest ------------------------------------------------------------

var httpClient = &http.Client{Timeout: 20 * time.Second}

type rpcClient struct{ base string }

func (c *rpcClient) get(path string, out any) error {
	resp, err := httpClient.Get(c.base + path)
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
	if h == 0 {
		return 0, errors.New("node reported height 0")
	}
	return h, nil
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
	if err := c.get(fmt.Sprintf("/broadcast_tx_sync?tx=0x%x", raw), &out); err != nil {
		return 999, err.Error()
	}
	if out.Error != nil {
		return 998, out.Error.Data
	}
	return out.Result.Code, out.Result.Log
}

type txResult struct {
	code uint32
	log  string
	data []byte
}

// waitForTx polls /tx?hash= until CometBFT reports the tx as included in a
// block, returning its REAL execution result -- the code/log/data FinalizeBlock
// produced, not the CheckTx-only result broadcast_tx_sync returns.
func (c *rpcClient) waitForTx(hash string, timeout time.Duration) (*txResult, error) {
	deadline := time.Now().Add(timeout)
	for {
		var out struct {
			Result *struct {
				TxResult struct {
					Code uint32 `json:"code"`
					Log  string `json:"log"`
					Data string `json:"data"`
				} `json:"tx_result"`
			} `json:"result"`
		}
		if err := c.get("/tx?hash=0x"+hash, &out); err == nil && out.Result != nil {
			res := &txResult{code: out.Result.TxResult.Code, log: out.Result.TxResult.Log}
			// CometBFT returns tx_result.data base64-encoded.
			if raw, err := base64.StdEncoding.DecodeString(out.Result.TxResult.Data); err == nil {
				res.data = raw
			}
			return res, nil
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("timed out waiting for tx %s to be included", hash)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (c *rpcClient) block(h int64) (struct {
	txs int
	ts  time.Time
}, error) {
	var zero struct {
		txs int
		ts  time.Time
	}
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
		return zero, err
	}
	zero.txs = len(out.Result.Block.Data.Txs)
	zero.ts = out.Result.Block.Header.Time
	return zero, nil
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

type restClient struct{ base string }

func (c *restClient) get(path string, out any) error {
	resp, err := httpClient.Get(c.base + path)
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

type feeParams struct {
	baseFee     int64
	maxFee      int64
	maxTxGas    uint64
	maxBlockGas uint64
}

func (c *restClient) feeParams() (feeParams, error) {
	var out struct {
		Params struct {
			BaseFeeAmount string `json:"base_fee_amount"`
			MaxFeeAmount  string `json:"max_fee_amount"`
			MaxTxGas      string `json:"max_tx_gas"`
			MaxBlockGas   string `json:"max_block_gas"`
		} `json:"params"`
	}
	if err := c.get("/l1/fees/v1/params", &out); err != nil {
		return feeParams{}, err
	}
	if out.Params.MaxFeeAmount == "" {
		return feeParams{}, errors.New("fee params response carried no max_fee_amount")
	}
	var p feeParams
	fmt.Sscanf(out.Params.BaseFeeAmount, "%d", &p.baseFee)
	fmt.Sscanf(out.Params.MaxFeeAmount, "%d", &p.maxFee)
	fmt.Sscanf(out.Params.MaxTxGas, "%d", &p.maxTxGas)
	fmt.Sscanf(out.Params.MaxBlockGas, "%d", &p.maxBlockGas)
	if p.maxFee <= 0 {
		return feeParams{}, fmt.Errorf("unusable max_fee_amount %q", out.Params.MaxFeeAmount)
	}
	return p, nil
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
	if out.Account.AccountNumber == "" {
		return 0, 0, fmt.Errorf("account %s not found (fund it first)", addr)
	}
	fmt.Sscanf(out.Account.AccountNumber, "%d", &accNum)
	fmt.Sscanf(out.Account.Sequence, "%d", &seq)
	return accNum, seq, nil
}

// --- misc ------------------------------------------------------------------

func parseWorkloads(csv string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(csv, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		switch part {
		case workloadTransfer, workloadPool, workloadContract:
		default:
			return nil, fmt.Errorf("unknown workload %q (want transfer, pool or contract)", part)
		}
		if !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("--workload selected nothing")
	}
	return out, nil
}

func hasWorkload(list []string, want string) bool {
	for _, w := range list {
		if w == want {
			return true
		}
	}
	return false
}

func dropWorkload(list []string, drop string) []string {
	out := list[:0]
	for _, w := range list {
		if w != drop {
			out = append(out, w)
		}
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "netsim:", err)
	os.Exit(1)
}
