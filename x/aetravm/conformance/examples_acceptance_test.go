package conformance

import (
	"encoding/hex"
	"math/big"
	"path/filepath"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
)

func TestAcceptanceCounterShouldBeExample(t *testing.T) {
	deployer := testAddress(0x11)
	res := compileExampleFile(t, "counter_should_be.atlx", compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	counterState := map[string]any{
		"counter":     int64(0),
		"owner":       addressing.FormatAccAddress(deployer),
		"target":      nil,
		"nonce":       uint32(0),
		"lastNow":     int64(0),
		"lastBalance": big.NewInt(0),
		"lastRandom":  uint64(0),
		"pingTicket":  nil,
		"box":         nil,
		"packed":      nil,
	}
	counterStorage := mustAVMStorage(t, counterState)
	counterSnapshot := avm.EncodeSnapshot(counterStorage)
	require.Equal(t, counterStorage, mustDecodeSnapshot(t, counterSnapshot))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	counterGetterOp := opcodeForGetter(t, res, "currentCounter")
	getExec, err := runner.Run(res.Module, counterStorage, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: counterGetterOp, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, getExec.ResultCode)
	gotCounter, err := getExec.ReturnValue.AsInt64()
	require.NoError(t, err)
	require.Equal(t, int64(0), gotCounter)

	executor := mustExecutor(t)
	counterAddr, err := executor.DeployContract(deployer, res.ModuleHash[:], []byte("counter"), counterSnapshot, sdkmath.NewInt(10_000))
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(counterAddr, runner.AsyncHandler(res.Module, nil, avm.RuntimeContext{})))

	setTargetBody := mustCodecBody(t, res.MessageBodies["SetTarget"], map[string]any{
		"target": addressing.FormatAccAddress(testAddress(0x22)),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      deployer,
		Destination: counterAddr,
		Value:       sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
		Opcode:      res.MessageBodyOpcodes["SetTarget"],
		QueryID:     11,
		Body:        setTargetBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
	}}))
	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equal(t, async.ResultOK, receipts[0].ResultCode)

	incBody := mustCodecBody(t, res.MessageBodies["Inc"], map[string]any{
		"by":     uint32(3),
		"ticket": uint32(7),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      deployer,
		Destination: counterAddr,
		Value:       sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
		Opcode:      res.MessageBodyOpcodes["Inc"],
		QueryID:     12,
		Body:        incBody,
		Bounce:      true,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
	}}))
	receipts, err = executor.ProcessBlock(2)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(receipts), 2)
	require.True(t, hasReceiptOpcode(receipts, async.BounceOpcode))

	counterContract, ok := executor.Contract(counterAddr)
	require.True(t, ok)
	counterRuntimeState := mustDecodeSnapshot(t, counterContract.State)
	counterGetterAfterBounce, err := runner.Run(res.Module, counterRuntimeState, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: counterGetterOp, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	gotAfterBounce, err := counterGetterAfterBounce.ReturnValue.AsInt64()
	require.NoError(t, err)
	require.Equal(t, int64(3), gotAfterBounce)

	rollbackBody := mustCodecBody(t, res.MessageBodies["Touch"], map[string]any{
		"nonce": uint32(99),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      testAddress(0x44),
		Destination: counterAddr,
		Value:       sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
		Opcode:      res.MessageBodyOpcodes["Touch"],
		QueryID:     13,
		Body:        rollbackBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
	}}))
	receipts, err = executor.ProcessBlock(3)
	require.NoError(t, err)
	require.NotEmpty(t, receipts)
	require.NotEqual(t, async.ResultOK, receipts[0].ResultCode)

	afterRollback, ok := executor.Contract(counterAddr)
	require.True(t, ok)
	require.Equal(t, counterContract.State, afterRollback.State)
}

func TestAcceptanceTokenFamilyExample(t *testing.T) {
	deployer := testAddress(0x31)
	owner := testAddress(0x32)
	recipient := testAddress(0x33)
	opts := compiler.Options{DeployerAddress: addressing.FormatAccAddress(deployer)}

	walletRes := compileExampleFile(t, filepath.Join("token", "token_wallet.atlx"), opts)
	masterRes := compileExampleFile(t, filepath.Join("token", "token_master.atlx"), opts)
	require.NoError(t, avm.VerifyInterface(walletRes.Module, walletRes.Manifest))
	require.NoError(t, avm.VerifyInterface(masterRes.Module, masterRes.Manifest))

	masterState := map[string]any{
		"owner":           addressing.FormatAccAddress(deployer),
		"pendingOwner":    nil,
		"totalSupply":     uint64(0),
		"tokenWalletCode": walletRes.CodeChunk,
		"metadata":        nil,
	}
	masterStorage := mustAVMStorage(t, masterState)
	masterSnapshot := avm.EncodeSnapshot(masterStorage)

	executor := mustExecutor(t)
	masterRunner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	masterAddr, err := executor.DeployContract(deployer, masterRes.ModuleHash[:], []byte("master"), masterSnapshot, sdkmath.NewInt(10_000))
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(masterAddr, masterRunner.AsyncHandler(masterRes.Module, nil, avm.RuntimeContext{})))

	// walletAddressOf(owner: address) is ABI surface for off-chain SDKs;
	// AVM v1 queries carry only a u64 query_id argument, so exercising a
	// parametric address getter at runtime is out of scope here. On-chain
	// callers use the WalletAddressRequest internal message instead. Verify
	// dispatch with the parameterless owner() getter.
	masterRuntimeState := mustDecodeSnapshot(t, masterSnapshot)
	masterGetterOp := opcodeForGetter(t, masterRes, "owner")
	masterGetter, err := masterRunner.Run(masterRes.Module, masterRuntimeState, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: masterAddr,
		Message:         async.MessageEnvelope{Opcode: masterGetterOp, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	masterOwnerFromGetter, err := masterGetter.ReturnValue.AsAddress()
	require.NoError(t, err)
	require.Equal(t, addressing.FormatAccAddress(deployer), masterOwnerFromGetter)

	walletLocalInit := mustCodecBytes(t, walletRes.StorageCodec, map[string]any{
		"master":     addressing.FormatAccAddress(masterAddr),
		"owner":      addressing.FormatAccAddress(owner),
		"walletCode": walletRes.CodeChunk,
		"balance":    uint64(0),
	})
	localWalletInit := contracttypes.NewStateInit(
		addressing.FormatAccAddress(masterAddr),
		hex.EncodeToString(walletRes.ModuleHash[:]),
		walletLocalInit,
		"",
		0,
	)
	localWalletAddr, _, err := contracttypes.DeriveContractAddressFromStateInit(
		contracttypes.DefaultContractChainID,
		contracttypes.DefaultContractNamespace,
		addressing.FormatAccAddress(masterAddr),
		localWalletInit,
		contracttypes.DefaultParams(),
	)
	require.NoError(t, err)
	repeatLocalWalletAddr, _, err := contracttypes.DeriveContractAddressFromStateInit(
		contracttypes.DefaultContractChainID,
		contracttypes.DefaultContractNamespace,
		addressing.FormatAccAddress(masterAddr),
		localWalletInit,
		contracttypes.DefaultParams(),
	)
	require.NoError(t, err)
	require.Equal(t, localWalletAddr, repeatLocalWalletAddr)

	childState := map[string]any{
		"master":     addressing.FormatAccAddress(masterAddr),
		"owner":      addressing.FormatAccAddress(owner),
		"walletCode": walletRes.CodeChunk,
		"balance":    uint64(0),
	}
	childSnapshot := avm.EncodeSnapshot(mustAVMStorage(t, childState))
	childDeployInit := contracttypes.NewStateInit(
		addressing.FormatAccAddress(masterAddr),
		hex.EncodeToString(walletRes.ModuleHash[:]),
		childSnapshot,
		"",
		0,
	)
	childDerived, _, err := contracttypes.DeriveContractAddressFromStateInit(
		contracttypes.DefaultContractChainID,
		contracttypes.DefaultContractNamespace,
		addressing.FormatAccAddress(masterAddr),
		childDeployInit,
		contracttypes.DefaultParams(),
	)
	require.NoError(t, err)
	childAddr, err := addressing.ParseAccAddress(childDerived)
	require.NoError(t, err)

	transferSeed := mustCodecBody(t, walletRes.MessageBodies["WalletInternalTransfer"], map[string]any{
		"from":       addressing.FormatAccAddress(masterAddr),
		"amount":     uint64(5),
		"responseTo": nil,
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      masterAddr,
		Destination: childAddr,
		Value:       sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
		Opcode:      walletRes.MessageBodyOpcodes["WalletInternalTransfer"],
		QueryID:     21,
		Body:        transferSeed,
		StateInit:   &childDeployInit,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
	}}))
	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equal(t, async.ResultExecutionFailed, receipts[0].ResultCode)

	_, ok := executor.Contract(childAddr)
	require.True(t, ok)
	require.NoError(t, executor.RegisterHandler(childAddr, masterRunner.AsyncHandler(walletRes.Module, nil, avm.RuntimeContext{})))

	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      masterAddr,
		Destination: childAddr,
		Value:       sdk.NewCoin(appparams.BaseDenom, sdkmath.NewInt(10_000)),
		Opcode:      walletRes.MessageBodyOpcodes["WalletInternalTransfer"],
		QueryID:     22,
		Body:        transferSeed,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
	}}))
	receipts, err = executor.ProcessBlock(2)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "receipt error: %s", receipts[0].Error)

	updatedChild, ok := executor.Contract(childAddr)
	require.True(t, ok)
	childRuntimeState := mustDecodeSnapshot(t, updatedChild.State)
	balanceGetterOp := opcodeForGetter(t, walletRes, "balance")
	balanceExec, err := masterRunner.Run(walletRes.Module, childRuntimeState, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: childAddr,
		Message:         async.MessageEnvelope{Opcode: balanceGetterOp, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	childBalance, err := balanceExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(5), childBalance)

	walletTransferBody := mustCodecBody(t, walletRes.MessageBodies["WalletTransfer"], map[string]any{
		"to":         addressing.FormatAccAddress(recipient),
		"amount":     uint64(2),
		"responseTo": nil,
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      owner,
		Destination: childAddr,
		Value:       sdk.NewCoin(appparams.BaseDenom, sdkmath.NewInt(10_000)),
		Opcode:      walletRes.MessageBodyOpcodes["WalletTransfer"],
		QueryID:     23,
		Body:        walletTransferBody,
		Bounce:      true,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
	}}))
	receipts, err = executor.ProcessBlock(3)
	require.NoError(t, err)
	require.True(t, hasReceiptOpcode(receipts, async.BounceOpcode))

	finalChild, ok := executor.Contract(childAddr)
	require.True(t, ok)
	finalRuntimeState := mustDecodeSnapshot(t, finalChild.State)
	finalBalanceExec, err := masterRunner.Run(walletRes.Module, finalRuntimeState, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: childAddr,
		Message:         async.MessageEnvelope{Opcode: balanceGetterOp, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	finalBalance, err := finalBalanceExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	// WalletTransfer decrements optimistically (5 -> 3) and the recipient
	// wallet cascade bounces (never registered in this test). The bounce
	// restores the balance via `match (bounced) { WalletInternalTransfer
	// => st.balance += bounced.amount }` in onBouncedMessage: the bounced
	// handler now reads the original message opcode (carried in the bounce
	// envelope's OriginalOpcode) instead of the outer bounce marker, so the
	// restore arm matches and the debited amount (2) is returned — balance
	// back to 5. This exercises the bounced-dispatch fix end to end.
	require.Equal(t, uint64(5), finalBalance)

	// Mint through the master: the credit cascades master -> wallet, and the
	// master's counterfactual wallet derivation must land on the deployed
	// child address.
	mintBody := mustCodecBody(t, masterRes.MessageBodies["Mint"], map[string]any{
		"owner":      addressing.FormatAccAddress(owner),
		"amount":     uint64(7),
		"responseTo": nil,
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      deployer,
		// Attach enough naet to cover the master's forwarded MSG_FORWARD_VALUE
		// (0.0001 AET = 100_000 naet) to the wallet plus storage rent. Value is
		// now conserved (a contract can only forward value it actually holds),
		// so the mint must fund the onward cascade.
		Destination: masterAddr,
		Value:       sdk.NewCoin(appparams.BaseDenom, sdkmath.NewInt(1_000_000)),
		Opcode:      masterRes.MessageBodyOpcodes["Mint"],
		QueryID:     24,
		Body:        mintBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
	}}))
	receipts, err = executor.ProcessBlock(4)
	require.NoError(t, err)
	require.NotEmpty(t, receipts)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "mint receipt error: %s", receipts[0].Error)

	mintedChild, ok := executor.Contract(childAddr)
	require.True(t, ok)
	mintedState := mustDecodeSnapshot(t, mintedChild.State)
	mintedBalanceExec, err := masterRunner.Run(walletRes.Module, mintedState, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: childAddr,
		Message:         async.MessageEnvelope{Opcode: balanceGetterOp, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	mintedBalance, err := mintedBalanceExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	// Wallet balance was restored to 5 by the earlier bounce, so the mint of 7
	// credits it to 12 (was 10 back when the bounce failed to restore).
	require.Equal(t, uint64(12), mintedBalance, "mint must credit the existing wallet")

	// Burn from the wallet: the wallet reduces its balance and the burn
	// notification cascades wallet -> master, reducing totalSupply.
	burnBody := mustCodecBody(t, walletRes.MessageBodies["WalletBurn"], map[string]any{
		"amount":     uint64(2),
		"responseTo": nil,
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      owner,
		Destination: childAddr,
		Value:       sdk.NewCoin(appparams.BaseDenom, sdkmath.NewInt(10_000)),
		Opcode:      walletRes.MessageBodyOpcodes["WalletBurn"],
		QueryID:     25,
		Body:        burnBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
	}}))
	receipts, err = executor.ProcessBlock(5)
	require.NoError(t, err)
	require.NotEmpty(t, receipts)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "burn receipt error: %s", receipts[0].Error)

	burnedChild, ok := executor.Contract(childAddr)
	require.True(t, ok)
	burnedState := mustDecodeSnapshot(t, burnedChild.State)
	burnedBalanceExec, err := masterRunner.Run(walletRes.Module, burnedState, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: childAddr,
		Message:         async.MessageEnvelope{Opcode: balanceGetterOp, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	burnedBalance, err := burnedBalanceExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	// Balance was 12 after the mint; burning 2 leaves 10 (was 8 in the
	// pre-fix cascade where the earlier bounce did not restore the 2 tokens).
	require.Equal(t, uint64(10), burnedBalance, "burn must reduce the wallet balance")

	masterContract, ok := executor.Contract(masterAddr)
	require.True(t, ok)
	masterFinalState := mustDecodeSnapshot(t, masterContract.State)
	supplyGetterOp := opcodeForGetter(t, masterRes, "totalSupply")
	supplyExec, err := masterRunner.Run(masterRes.Module, masterFinalState, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: masterAddr,
		Message:         async.MessageEnvelope{Opcode: supplyGetterOp, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	totalSupply, err := supplyExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(5), totalSupply, "burn notification must reduce totalSupply on the master")
}

func compileExampleFile(t *testing.T, rel string, opts compiler.Options) *compiler.Result {
	t.Helper()
	c, err := compiler.New(opts)
	require.NoError(t, err)
	path := filepath.Clean(filepath.Join("..", "..", "..", "examples", "avm", rel))
	res, err := c.CompileFile(path)
	require.NoError(t, err)
	require.NoError(t, res.Manifest.Validate())
	return res
}

func mustCodecBytes(t *testing.T, codec compiler.Codec, value any) []byte {
	t.Helper()
	bz, err := codec.Encode(value)
	require.NoError(t, err)
	return bz
}

func mustAVMStorage(t *testing.T, values map[string]any) avm.Storage {
	t.Helper()
	out := make(avm.Storage, len(values))
	for key, value := range values {
		out[key] = mustRuntimeBytes(t, value)
	}
	return out
}

func mustDecodeSnapshot(t *testing.T, snapshot []byte) avm.Storage {
	t.Helper()
	st, err := avm.DecodeSnapshot(snapshot)
	require.NoError(t, err)
	return st
}

func mustRuntimeBytes(t *testing.T, value any) []byte {
	t.Helper()
	var rv avm.RuntimeValue
	switch v := value.(type) {
	case nil:
		rv = avm.ValueNull()
	case int:
		rv = avm.ValueInt64(int64(v))
	case int64:
		rv = avm.ValueInt64(v)
	case uint32:
		rv = avm.ValueUint32(v)
	case uint64:
		rv = avm.ValueUint64(v)
	case string:
		rv = avm.ValueAddress(v)
	case *big.Int:
		rv = avm.ValueCoins(v)
	case *chunk.Chunk:
		rv = avm.ValueChunkRef(v)
	case chunk.Chunk:
		rv = avm.ValueChunkRef(&v)
	case bool:
		rv = avm.ValueBool(v)
	default:
		t.Fatalf("unsupported runtime value type %T", value)
	}
	bz, err := avm.CanonicalEncode(rv)
	require.NoError(t, err)
	return bz
}

func mustCodecBody(t *testing.T, codec compiler.Codec, value any) []byte {
	t.Helper()
	return mustCodecBytes(t, codec, value)
}

func mustExecutor(t *testing.T) *async.Executor {
	t.Helper()
	executor, err := async.NewExecutor(async.DefaultParams())
	require.NoError(t, err)
	return executor
}

func opcodeForGetter(t *testing.T, result *compiler.Result, name string) uint32 {
	t.Helper()
	for _, method := range result.Manifest.Methods {
		if method.Name == name {
			return method.Opcode
		}
	}
	for _, entry := range result.SelectorRegistry.Entries {
		if entry.Name == name {
			return entry.Selector
		}
	}
	t.Fatalf("getter %q not found in selector registry", name)
	return 0
}

func hasReceiptOpcode(receipts []async.ExecutionReceipt, opcode uint32) bool {
	for _, receipt := range receipts {
		if receipt.Opcode == opcode {
			return true
		}
	}
	return false
}
