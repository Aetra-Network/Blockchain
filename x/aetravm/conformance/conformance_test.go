package conformance

import (
	"bytes"
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
struct SenderState {
  count: u64 = 0
}

contract Sender {
  storage SenderState
  namespace "sender"
  chain "avm-local"
  deploy {
    set state.count = 0
    return 0
  }
  message internal Send() selector = 11 {
    send 0 to "AEreceiver" opcode = 77;
    return 0
  }
  message bounced Refund() selector = 12 {
    return 0
  }
  getter GetCount() -> u64 selector = 13 {
    return state.count
  }
}
`

const getterOnlySource = `
struct ReaderState {
  count: u64 = 7
}

contract Reader {
  storage ReaderState
  namespace "reader"
  chain "avm-local"
  deploy {
    return 0
  }
  message bounced Refund() selector = 12 {
    return 0
  }
  getter GetCount() -> u64 selector = 13 {
    return state.count
  }
}
`

const timerSource = `
struct TimerState {
  ticks: u64 = 0
}

contract Timer {
  storage TimerState
  namespace "timer"
  chain "avm-local"
  deploy {
    set state.ticks = 0
    return 0
  }
  message internal Tick() selector = 11 {
    set state.ticks = state.ticks + 1
    self 2
    return 0
  }
  message bounced Refund() selector = 12 {
    return 0
  }
  getter GetTicks() -> u64 selector = 13 {
    return state.ticks
  }
}
`

func TestConformanceDeployInternalBouncedRefundGetter(t *testing.T) {
	sender := compileConformance(t, senderSource)

	require.NoError(t, avm.VerifyInterface(sender.Module, sender.Manifest))

	deployExec := runConformanceModule(t, sender.Module, avm.RuntimeContext{
		Entry:    avm.EntryDeploy,
		Message:  async.MessageEnvelope{GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.Equal(t, async.ResultOK, deployExec.ResultCode)
	require.Equal(t, uint64(0), avm.DecodeU64(deployExec.State["count"]))

	executor, err := async.NewExecutor(async.DefaultParams())
	require.NoError(t, err)
	deployer := testAddress(1)
	senderAddr := deployConformanceContract(t, executor, deployer, []byte("sender"), avm.EncodeSnapshot(deployExec.State))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	emitExec, err := runner.Run(sender.Module, deployExec.State, avm.RuntimeContext{
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
		Message:  async.MessageEnvelope{Opcode: 13, GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.Equal(t, uint64(0), getExec.ReturnValue)
	require.Equal(t, deployExec.State["count"], getExec.State["count"])
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

func TestConformanceSelectorCollisionsAndABIVersionMismatch(t *testing.T) {
	colliding := `
struct CounterState {
  count: u64 = 0
}

contract Counter {
  storage CounterState
  message external First() selector = 11 {
    return 0
  }
  message internal Second() selector = 11 {
    return 0
  }
  message bounced Refund() selector = 12 {
    return 0
  }
}
`
	c := mustCompiler(t)
	_, err := c.Compile([]byte(colliding))
	require.ErrorContains(t, err, "selector")

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
	deployExec, err := runner.Run(getterOnly.Module, avm.Storage{"count": avm.EncodeU64(7)}, avm.RuntimeContext{
		Entry:    avm.EntryDeploy,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, deployExec.ResultCode)
	require.Equal(t, uint64(7), avm.DecodeU64(deployExec.State["count"]))

	getExec, err := runner.Run(getterOnly.Module, deployExec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: 13, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, getExec.ResultCode)
	require.Equal(t, uint64(7), getExec.ReturnValue)
	require.Empty(t, getExec.Outgoing)
	require.Equal(t, deployExec.State, getExec.State)

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

func compileConformance(t *testing.T, src string) *compiler.Result {
	t.Helper()
	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	return res
}

func mustCompiler(t *testing.T) *compiler.Compiler {
	t.Helper()
	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	return c
}

func runConformanceModule(t *testing.T, module avm.Module, ctx avm.RuntimeContext) avm.Execution {
	t.Helper()
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(module, avm.Storage{"count": avm.EncodeU64(0)}, ctx)
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
