package conformance

import (
	"bytes"
	"crypto/sha256"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

const senderSource = `
@storage
struct SenderState {
  count: u64 = 0
}

@message(11)
struct Send {}

@message(88)
struct Route {}

type SenderMsg = Send | Route

contract Sender {
  storage: SenderState
  incomingMessages: SenderMsg
  namespace "sender"
  chain "avm-local"

  @store
  func SenderState.load() {
    return SenderState.fromChunk(contract.getData())
  }

  @store
  func SenderState.save(self) {
    contract.setData(self.toChunk())
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy SenderMsg.fromSegment(in.body)

    match (msg) {
      Send => {
        send 0 to "AEreceiver" opcode = 77;
      }

      Route => {
        send 0 to "AEreceiver" opcode = 77;
      }

      else => {
        assert (in.body.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    const st = lazy SenderState.load()
    return st.count
  }
}
`

const getterOnlySource = `
@storage
struct ReaderState {
  count: u64 = 7
}

@message(11)
struct Poke {}

type ReaderMsg = Poke

contract Reader {
  storage: ReaderState
  incomingMessages: ReaderMsg
  namespace "reader"
  chain "avm-local"

  @store
  func ReaderState.load() {
    return ReaderState.fromChunk(contract.getData())
  }

  @store
  func ReaderState.save(self) {
    contract.setData(self.toChunk())
  }

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    const st = lazy ReaderState.load()
    return st.count
  }
}
`

const treasuryStandardSource = `
@storage
struct TreasuryState {
  balance: u64 = 0
  pending: u64 = 0
  owner: Address = "AEowner"
}

@message(21)
struct Deposit {}

@message(22)
struct Credit {}

type TreasuryExternalMsg = Deposit
type TreasuryInternalMsg = Credit

contract Treasury {
  storage: TreasuryState
  incomingMessages: TreasuryInternalMsg
  incomingExternal: TreasuryExternalMsg
  namespace "treasury"
  chain "avm-local"

  @store
  func TreasuryState.load() {
    return TreasuryState.fromChunk(contract.getData())
  }

  @store
  func TreasuryState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy TreasuryExternalMsg.fromSegment(inMsg)

    match (msg) {
      Deposit => {
        var st = lazy TreasuryState.load()
        st.balance += 1
        st.save()
      }

      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy TreasuryInternalMsg.fromSegment(in.body)

    match (msg) {
      Credit => {
        var st = lazy TreasuryState.load()
        st.pending += 1
        st.save()
        send 0 to "AErouter" opcode = 88;
      }

      else => {
        assert (in.body.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
    var st = lazy TreasuryState.load()
    st.pending = 0
    st.save()
  }

  @get
  func getBalance(): u64 {
    const st = lazy TreasuryState.load()
    return st.balance
  }
}
`

func TestConformanceDeployInternalBouncedRefundGetter(t *testing.T) {
	sender := compileConformance(t, senderSource)

	require.NoError(t, avm.VerifyInterface(sender.Module, sender.Manifest))

	initialState := avm.Storage{"count": avm.EncodeU64(0)}

	executor, err := async.NewExecutor(async.DefaultParams())
	require.NoError(t, err)
	deployer := testAddress(1)
	senderAddr := deployConformanceContract(t, executor, deployer, []byte("sender"), avm.EncodeSnapshot(initialState))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	emitExec, err := runner.Run(sender.Module, initialState, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		Message:         async.MessageEnvelope{Opcode: 11, GasLimit: 100_000},
		GasLimit:        100_000,
		EmitDestination: testAddress(2),
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, emitExec.ResultCode)
	require.Len(t, emitExec.Outgoing, 1)
	require.Equal(t, uint32(77), emitExec.Outgoing[0].Opcode)

	require.NoError(t, executor.RegisterHandler(senderAddr, senderRunner(sender.Module, testAddress(9))))
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{
		testMessage(deployer, senderAddr, 11, false),
	}))
	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 3)
	require.Equal(t, async.ResultOK, receipts[0].ResultCode)
	require.Equal(t, async.ResultNoDestination, receipts[1].ResultCode)
	require.Equal(t, async.BounceOpcode, receipts[2].Opcode)

	require.NoError(t, executor.RegisterHandler(senderAddr, senderRunner(sender.Module, testAddress(9))))
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{
		testMessage(deployer, senderAddr, 88, false),
	}))
	receipts, err = executor.ProcessBlock(2)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(receipts), 2)
	require.Equal(t, async.BounceOpcode, receipts[len(receipts)-1].Opcode)

	getExec := runConformanceModule(t, sender.Module, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, sender, "getCount"), GasLimit: 100_000},
		GasLimit: 100_000,
	}, initialState)
	gotReturn, err := getExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(0), gotReturn)
	require.Equal(t, initialState["count"], getExec.State["count"])
}

func TestConformanceRefundDeterminism(t *testing.T) {
	msg := async.MessageEnvelope{
		Source:      testAddress(1),
		Destination: testAddress(2),
		Value:       sdk.NewCoin(params.BaseDenom, sdkmath.NewInt(20)),
		Opcode:      41,
		QueryID:     41,
		GasLimit:    100_000,
		ForwardFee:  sdk.NewCoin(params.BaseDenom, sdkmath.ZeroInt()),
	}
	receipt := async.ExecutionReceipt{
		ForwardFeeNaet: sdkmath.NewInt(7),
	}
	refund1, err := async.CalculateRefund(msg, receipt)
	require.NoError(t, err)
	refund2, err := async.CalculateRefund(msg, receipt)
	require.NoError(t, err)
	require.Equal(t, refund1, refund2)

	refundMsg1, err := async.BuildRefundMessage(msg, refund1, sdkmath.NewInt(3))
	require.NoError(t, err)
	refundMsg2, err := async.BuildRefundMessage(msg, refund2, sdkmath.NewInt(3))
	require.NoError(t, err)
	require.Equal(t, refundMsg1, refundMsg2)
	require.Equal(t, async.RefundOpcode, refundMsg1.Opcode)
}

func TestConformanceStorageAndSpillover(t *testing.T) {
	res := compileConformance(t, senderSource)
	encoded, err := res.StorageCodec.EncodeDefaults()
	require.NoError(t, err)
	var decoded struct {
		Count uint64
	}
	require.NoError(t, res.StorageCodec.Decode(encoded, &decoded))
	reencoded, err := res.StorageCodec.Encode(&decoded)
	require.NoError(t, err)
	require.Equal(t, encoded, reencoded)

	payload := bytes.Repeat([]byte("payload"), 1024)
	root, err := avm.BuildChunkPayload(payload, chunk.TypeSystem)
	require.NoError(t, err)
	flattened, err := avm.FlattenChunkPayload(root)
	require.NoError(t, err)
	require.Equal(t, payload, flattened)
}

func TestConformanceCanonicalLayoutAndPackageLockStability(t *testing.T) {
	resolver := testConformanceResolver{
		files: map[string]compiler.NamedSource{
			"lib/math@1.0.0": {Name: "math.atlx", Data: []byte(conformanceMathLibrary)},
			"lib/meta@1.0.0": {Name: "meta.atlx", Data: []byte(conformanceMetaLibrary)},
			"lib/math@2.0.0": {Name: "math.atlx", Data: []byte(conformanceMathLibrary)},
		},
	}

	base := compileConformanceSource(t, conformanceLayoutSource, resolver)
	reordered := compileConformanceSource(t, conformanceLayoutReorderedSource, resolver)
	require.Equal(t, base.ModuleHash, reordered.ModuleHash)
	require.Equal(t, base.ManifestHash, reordered.ManifestHash)
	require.Equal(t, base.StateInitHash, reordered.StateInitHash)
	require.Equal(t, base.SelectorRegistry.RegistryHash, reordered.SelectorRegistry.RegistryHash)
	require.Equal(t, base.StorageLayout.LayoutHash, reordered.StorageLayout.LayoutHash)
	require.Equal(t, base.StorageCodec.Hash, reordered.StorageCodec.Hash)
	require.Equal(t, base.DependencyLock.LockHash, reordered.DependencyLock.LockHash)

	layoutMutated := compileConformanceSource(t, conformanceLayoutFieldOrderSource, resolver)
	require.NotEqual(t, base.StorageLayout.LayoutHash, layoutMutated.StorageLayout.LayoutHash)
	require.NotEqual(t, base.StorageCodec.Hash, layoutMutated.StorageCodec.Hash)

	lockMutated := compileConformanceSource(t, conformanceLayoutVersionChangedSource, resolver)
	require.NotEqual(t, base.DependencyLock.LockHash, lockMutated.DependencyLock.LockHash)
}

func TestConformancePurityAndRecursionRulesAreEnforced(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "pure function cannot write state",
			src:  pureFunctionMutationSource,
			want: "pure functions cannot write state",
		},
		{
			name: "pure function cannot emit events",
			src:  pureFunctionEmitSource,
			want: "pure functions cannot emit events",
		},
		{
			name: "pure function cannot send or schedule",
			src:  pureFunctionSendSource,
			want: "pure functions cannot send/refund/schedule self",
		},
		{
			name: "recursive function cycle is rejected",
			src:  recursiveFunctionSource,
			want: "recursive function call cycle detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileConformanceMaybeErr(t, tt.src)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestConformanceSelectorCollisionsAndABIVersionMismatch(t *testing.T) {
	// Two @message structs pinned to the same opcode must be rejected: message
	// opcodes are the only dispatch selectors left in ATLX.
	colliding := `
struct CounterState {
  count: u64 = 0
}

@message(11)
struct First {}

@message(11)
struct Second {}

type CounterMsg = First | Second

contract Counter {
  storage: CounterState
  incomingMessages: CounterMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	c := mustCompiler(t)
	_, err := c.Compile([]byte(colliding))
	require.ErrorContains(t, err, "is already bound to message schema")

	res := compileConformance(t, senderSource)
	badManifest := res.Manifest
	badManifest.Version++
	require.ErrorContains(t, avm.VerifyInterface(res.Module, badManifest), "metadata hash mismatch")
}

func TestConformanceGetterOnlyAndScheduledSelfStandardPackages(t *testing.T) {
	getterOnly := compileConformance(t, getterOnlySource)
	require.NoError(t, avm.VerifyInterface(getterOnly.Module, getterOnly.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	initialState := avm.Storage{"count": avm.EncodeU64(7)}

	getExec, err := runner.Run(getterOnly.Module, initialState, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, getterOnly, "getCount"), GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, getExec.ResultCode)
	gotReturn, err := getExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(7), gotReturn)
	require.Empty(t, getExec.Outgoing)
	require.Equal(t, initialState, getExec.State)

	timerModule := avm.Module{
		Version: avm.Version,
		Imports: []avm.HostFunction{
			avm.HostReadStorage,
			avm.HostWriteStorage,
			avm.HostScheduleSelf,
			avm.HostReturn,
		},
		Exports: map[avm.Entrypoint]uint32{
			avm.EntryReceiveInternal: 0,
		},
		Code: []avm.Instruction{
			{Op: avm.OpReadStorage, Data: []byte("ticks")},
			{Op: avm.OpPushU64, Arg: 1},
			{Op: avm.OpAdd},
			{Op: avm.OpWriteStorage, Data: []byte("ticks")},
			{Op: avm.OpScheduleSelf, Arg: 2, Data: []byte("resume")},
			{Op: avm.OpReturn, Arg: uint64(async.ResultOK)},
		},
	}
	tickExec, err := runner.Run(timerModule, avm.Storage{"ticks": avm.EncodeU64(0)}, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: testAddress(3),
		BlockHeight:     5,
		GasLimit:        100_000,
		Message:         async.MessageEnvelope{Opcode: 11, QueryID: 11, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, tickExec.ResultCode)
	require.Equal(t, uint64(1), avm.DecodeU64(tickExec.State["ticks"]))
	require.Len(t, tickExec.Outgoing, 1)
	require.Equal(t, uint64(7), tickExec.Outgoing[0].DeliverAtBlock)
	require.True(t, tickExec.Outgoing[0].Destination.Equals(testAddress(3)))
}

func TestConformanceTreasuryStandardPackageCoversLifecycle(t *testing.T) {
	treasury := compileConformance(t, treasuryStandardSource)
	require.NoError(t, avm.VerifyInterface(treasury.Module, treasury.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	initialState := avm.Storage{
		"balance": avm.EncodeU64(0),
		"pending": avm.EncodeU64(0),
	}

	externalExec, err := runner.Run(treasury.Module, initialState, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: 21, QueryID: 21, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, externalExec.ResultCode)
	require.Equal(t, uint64(1), avm.DecodeU64(externalExec.State["balance"]))

	internalExec, err := runner.Run(treasury.Module, externalExec.State, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		GasLimit:        100_000,
		EmitDestination: testAddress(4),
		Message:         async.MessageEnvelope{Opcode: 22, QueryID: 22, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, internalExec.ResultCode)
	require.Len(t, internalExec.Outgoing, 1)
	require.True(t, internalExec.Outgoing[0].Destination.Equals(testAddress(4)))
	require.Equal(t, uint64(1), avm.DecodeU64(internalExec.State["pending"]))

	bouncedExec, err := runner.Run(treasury.Module, internalExec.State, avm.RuntimeContext{
		Entry:    avm.EntryReceiveBounced,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: 23, QueryID: 23, GasLimit: 100_000, Bounced: true},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, bouncedExec.ResultCode)
	require.Equal(t, uint64(0), avm.DecodeU64(bouncedExec.State["pending"]))

	// `message migrate` has no source surface anymore (EntryMigrate is
	// runtime-only), so the lifecycle stops at external + internal + bounced.

	getExec, err := runner.Run(treasury.Module, bouncedExec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, treasury, "getBalance"), QueryID: 25, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, getExec.ResultCode)
	gotReturn, err := getExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(1), gotReturn)
	require.Empty(t, getExec.Outgoing)
}

func compileConformance(t *testing.T, src string) *compiler.Result {
	t.Helper()
	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	return res
}

func compileConformanceSource(t *testing.T, src string, resolver testConformanceResolver) *compiler.Result {
	t.Helper()
	c, err := compiler.New(compiler.Options{Resolver: resolver})
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	return res
}

func compileConformanceMaybeErr(t *testing.T, src string) (*compiler.Result, error) {
	t.Helper()
	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	return c.Compile([]byte(src))
}

func mustCompiler(t *testing.T) *compiler.Compiler {
	t.Helper()
	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	return c
}

func runConformanceModule(t *testing.T, module avm.Module, ctx avm.RuntimeContext, storage avm.Storage) avm.Execution {
	t.Helper()
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(module, storage, ctx)
	require.NoError(t, err)
	return exec
}

func senderRunner(module avm.Module, emitDestination sdk.AccAddress) async.Handler {
	runner, err := avm.NewRunner(avm.DefaultParams())
	if err != nil {
		panic(err)
	}
	return runner.AsyncHandler(module, nil, avm.RuntimeContext{EmitDestination: emitDestination})
}

func deployConformanceContract(t *testing.T, executor *async.Executor, deployer sdk.AccAddress, salt []byte, state []byte) sdk.AccAddress {
	t.Helper()
	if len(state) == 0 {
		state = nil
	}
	address, err := executor.DeployContract(deployer, bytes.Repeat([]byte{salt[0]}, async.CodeHashLength), salt, state, sdkmath.NewInt(10_000))
	require.NoError(t, err)
	return address
}

func testMessage(source, destination sdk.AccAddress, opcode uint32, bounce bool) async.MessageEnvelope {
	return async.MessageEnvelope{
		Source:             append(sdk.AccAddress(nil), source...),
		Destination:        append(sdk.AccAddress(nil), destination...),
		Value:              sdk.NewCoin(params.BaseDenom, sdkmath.ZeroInt()),
		Opcode:             opcode,
		QueryID:            uint64(opcode),
		Body:               []byte("in"),
		Bounce:             bounce,
		CreatedLogicalTime: uint64(opcode),
		GasLimit:           100_000,
		ForwardFee:         sdk.NewCoin(params.BaseDenom, sdkmath.ZeroInt()),
	}
}

func testAddress(fill byte) sdk.AccAddress {
	return sdk.AccAddress(bytes.Repeat([]byte{fill}, 20))
}

type testConformanceResolver struct {
	files map[string]compiler.NamedSource
}

func (r testConformanceResolver) ResolveImport(imp compiler.ImportDecl) (compiler.ResolvedDependency, *compiler.SourceFile, error) {
	src, ok := r.files[imp.Path+"@"+imp.Version]
	sum := sha256.Sum256([]byte(imp.Path + "@" + imp.Version))
	dep := compiler.ResolvedDependency{
		Path:    imp.Path,
		Version: imp.Version,
		Alias:   imp.Alias,
	}
	dep.ABIHash = sum
	dep.SourceHash = sum
	dep.LockHash = sum
	if !ok {
		return dep, nil, nil
	}
	parsed, err := compiler.ParseSourceNamed(src.Name, string(src.Data))
	if err != nil {
		return compiler.ResolvedDependency{}, nil, err
	}
	return dep, parsed, nil
}

const conformanceMathLibrary = `
package lib.math

func plusOne(x: u64) -> u64 {
  return x + 1
}
`

const conformanceMetaLibrary = `
package lib.meta

func decorate(x: u64) -> u64 {
  return x
}
`

const conformanceLayoutSource = `
package app.counter
import "lib/math@1.0.0"
import "lib/meta@1.0.0"

@storage
struct CounterState {
  count: u64 = 0
  owner: Address = "AEowner"
}

@message(11)
struct Increment {}

type CounterExternalMsg = Increment

contract Counter {
  storage: CounterState
  incomingExternal: CounterExternalMsg
  namespace "counter"
  chain "avm-local"

  @store
  func CounterState.load() {
    return CounterState.fromChunk(contract.getData())
  }

  @store
  func CounterState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy CounterExternalMsg.fromSegment(inMsg)

    match (msg) {
      Increment => {
        var st = lazy CounterState.load()
        st.count += 1
        st.save()
      }

      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    const st = lazy CounterState.load()
    return st.count
  }
}
`

const conformanceLayoutReorderedSource = `
package app.counter
import "lib/meta@1.0.0"
import "lib/math@1.0.0"

@storage
struct CounterState {
  count: u64 = 0
  owner: Address = "AEowner"
}

@message(11)
struct Increment {}

type CounterExternalMsg = Increment

contract Counter {
  storage: CounterState
  incomingExternal: CounterExternalMsg
  namespace "counter"
  chain "avm-local"

  @get
  func getCount(): u64 {
    const st = lazy CounterState.load()
    return st.count
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy CounterExternalMsg.fromSegment(inMsg)

    match (msg) {
      Increment => {
        var st = lazy CounterState.load()
        st.count += 1
        st.save()
      }

      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @store
  func CounterState.save(self) {
    contract.setData(self.toChunk())
  }

  @store
  func CounterState.load() {
    return CounterState.fromChunk(contract.getData())
  }
}
`

const conformanceLayoutFieldOrderSource = `
package app.counter
import "lib/math@1.0.0"
import "lib/meta@1.0.0"

@storage
struct CounterState {
  owner: Address = "AEowner"
  count: u64 = 0
}

@message(11)
struct Increment {}

type CounterExternalMsg = Increment

contract Counter {
  storage: CounterState
  incomingExternal: CounterExternalMsg
  namespace "counter"
  chain "avm-local"

  @store
  func CounterState.load() {
    return CounterState.fromChunk(contract.getData())
  }

  @store
  func CounterState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy CounterExternalMsg.fromSegment(inMsg)

    match (msg) {
      Increment => {
        var st = lazy CounterState.load()
        st.count += 1
        st.save()
      }

      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    const st = lazy CounterState.load()
    return st.count
  }
}
`

const conformanceLayoutVersionChangedSource = `
package app.counter
import "lib/math@2.0.0"
import "lib/meta@1.0.0"

@storage
struct CounterState {
  count: u64 = 0
  owner: Address = "AEowner"
}

@message(11)
struct Increment {}

type CounterExternalMsg = Increment

contract Counter {
  storage: CounterState
  incomingExternal: CounterExternalMsg
  namespace "counter"
  chain "avm-local"

  @store
  func CounterState.load() {
    return CounterState.fromChunk(contract.getData())
  }

  @store
  func CounterState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy CounterExternalMsg.fromSegment(inMsg)

    match (msg) {
      Increment => {
        var st = lazy CounterState.load()
        st.count += 1
        st.save()
      }

      else => {
        assert (inMsg.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func getCount(): u64 {
    const st = lazy CounterState.load()
    return st.count
  }
}
`

const pureFunctionMutationSource = `
struct CounterState {
  count: u64 = 0
}

func mutate() -> u64 {
  set state.count = 1
  return 0
}

@message(11)
struct Ping {}

type CounterMsg = Ping

contract Counter {
  storage: CounterState
  incomingMessages: CounterMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

const pureFunctionEmitSource = `
struct CounterState {
  count: u64 = 0
}

func emit_event() -> u64 {
  emit Changed(1)
  return 0
}

@message(11)
struct Ping {}

type CounterMsg = Ping

contract Counter {
  storage: CounterState
  incomingMessages: CounterMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

const pureFunctionSendSource = `
struct CounterState {
  count: u64 = 0
}

func send_message() -> u64 {
  send 0 to "AEreceiver" opcode = 77;
  return 0
}

@message(11)
struct Ping {}

type CounterMsg = Ping

contract Counter {
  storage: CounterState
  incomingMessages: CounterMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`

const recursiveFunctionSource = `
struct CounterState {
  count: u64 = 0
}

func first() -> u64 {
  return second()
}

func second() -> u64 {
  return first()
}

@message(11)
struct Ping {}

type CounterMsg = Ping

contract Counter {
  storage: CounterState
  incomingMessages: CounterMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
