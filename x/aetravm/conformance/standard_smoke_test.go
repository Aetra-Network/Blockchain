package conformance

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/app/wasmconfig"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

func reviewGateForwardFee() sdk.Coin {
	return sdk.NewCoin(appparams.BaseDenom, async.DefaultParams().ForwardingFee)
}

const counterStandardSource = `
@storage
struct CounterState {
  count: u64 = 0
  owner: Address = "AEowner"
}

@message(11)
struct Increment {
  amount: u64
}

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
        st.count = msg.amount
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

const tokenStandardSource = `
@storage
struct TokenState {
  supply: u64 = 0
  vault: u64 = 0
  version: u64 = 1
}

@message(31)
struct Mint {}

type TokenExternalMsg = Mint

contract Token {
  storage: TokenState
  incomingExternal: TokenExternalMsg
  namespace "token"
  chain "avm-local"

  @store
  func TokenState.load() {
    return TokenState.fromChunk(contract.getData())
  }

  @store
  func TokenState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy TokenExternalMsg.fromSegment(inMsg)

    match (msg) {
      Mint => {
        var st = lazy TokenState.load()
        st.supply += 1
        st.vault += 1
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
  func totalSupply(): u64 {
    const st = lazy TokenState.load()
    return st.supply
  }
}
`

const nftStandardSource = `
@storage
struct NFTState {
  token_id: u64 = 1
  owner_id: u64 = 1
  version: u64 = 1
}

@message(41)
struct Transfer {}

type NFTExternalMsg = Transfer

contract NFT {
  storage: NFTState
  incomingExternal: NFTExternalMsg
  namespace "nft"
  chain "avm-local"

  @store
  func NFTState.load() {
    return NFTState.fromChunk(contract.getData())
  }

  @store
  func NFTState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy NFTExternalMsg.fromSegment(inMsg)

    match (msg) {
      Transfer => {
        var st = lazy NFTState.load()
        st.owner_id = 2
        st.token_id += 1
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
  func ownerId(): u64 {
    const st = lazy NFTState.load()
    return st.owner_id
  }
}
`

const dexStandardSource = `
@storage
struct DexState {
  reserve_a: u64 = 0
  reserve_b: u64 = 0
  lp_supply: u64 = 0
  version: u64 = 1
}

@message(51)
struct AddLiquidity {}

type DexExternalMsg = AddLiquidity

contract Dex {
  storage: DexState
  incomingExternal: DexExternalMsg
  namespace "dex"
  chain "avm-local"

  @store
  func DexState.load() {
    return DexState.fromChunk(contract.getData())
  }

  @store
  func DexState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy DexExternalMsg.fromSegment(inMsg)

    match (msg) {
      AddLiquidity => {
        var st = lazy DexState.load()
        st.reserve_a += 10
        st.reserve_b += 20
        st.lp_supply += 30
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
  func reserves(): u64 {
    const st = lazy DexState.load()
    return st.reserve_a + st.reserve_b + st.lp_supply
  }
}
`

func TestConformanceTokenStandardPackageCoversLifecycle(t *testing.T) {
	res := compileConformance(t, tokenStandardSource)
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	initialState := avm.Storage{
		"supply":  avm.EncodeU64(0),
		"vault":   avm.EncodeU64(0),
		"version": avm.EncodeU64(1),
	}

	mintExec, err := runner.Run(res.Module, initialState, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: 31, QueryID: 31, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, mintExec.ResultCode)
	require.Equal(t, uint64(1), avm.DecodeU64(mintExec.State["supply"]))
	require.Equal(t, uint64(1), avm.DecodeU64(mintExec.State["vault"]))

	// `message migrate` has no source surface anymore (EntryMigrate is
	// runtime-only), so the compiled lifecycle covers external + getter.

	getExec, err := runner.Run(res.Module, mintExec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "totalSupply"), QueryID: 34, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, getExec.ResultCode)
	gotReturn, err := getExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(1), gotReturn)
	require.Equal(t, getExec.State, mintExec.State)
}

func TestConformanceNFTStandardPackageCoversLifecycle(t *testing.T) {
	res := compileConformance(t, nftStandardSource)
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	initialState := avm.Storage{
		"token_id": avm.EncodeU64(1),
		"owner_id": avm.EncodeU64(1),
		"version":  avm.EncodeU64(1),
	}

	transferExec, err := runner.Run(res.Module, initialState, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: 41, QueryID: 41, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, transferExec.ResultCode)
	require.Equal(t, uint64(2), avm.DecodeU64(transferExec.State["token_id"]))
	require.Equal(t, uint64(2), avm.DecodeU64(transferExec.State["owner_id"]))

	// `message migrate` has no source surface anymore (EntryMigrate is
	// runtime-only), so the compiled lifecycle covers external + getter.

	ownerExec, err := runner.Run(res.Module, transferExec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "ownerId"), QueryID: 44, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, ownerExec.ResultCode)
	gotReturn, err := ownerExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(2), gotReturn)
	require.Equal(t, transferExec.State, ownerExec.State)
}

func TestConformanceDEXStandardPackageCoversLifecycle(t *testing.T) {
	res := compileConformance(t, dexStandardSource)
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	initialState := avm.Storage{
		"reserve_a": avm.EncodeU64(0),
		"reserve_b": avm.EncodeU64(0),
		"lp_supply": avm.EncodeU64(0),
		"version":   avm.EncodeU64(1),
	}

	addExec, err := runner.Run(res.Module, initialState, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: 51, QueryID: 51, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, addExec.ResultCode)
	require.Equal(t, uint64(10), avm.DecodeU64(addExec.State["reserve_a"]))
	require.Equal(t, uint64(20), avm.DecodeU64(addExec.State["reserve_b"]))
	require.Equal(t, uint64(30), avm.DecodeU64(addExec.State["lp_supply"]))

	// `message migrate` has no source surface anymore (EntryMigrate is
	// runtime-only), so the compiled lifecycle covers external + getter.

	getExec, err := runner.Run(res.Module, addExec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "reserves"), QueryID: 55, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, getExec.ResultCode)
	gotReturn, err := getExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(60), gotReturn)
	require.Equal(t, addExec.State, getExec.State)
}

const externalContextSurfaceSource = `
const ERR_BAD_NONCE = 401
const ERR_BAD_MSG = 402

@storage
struct ExternalState {
  nonce: uint32 = 7
  lastNow: int64 = 0
  lastLogicalTime: uint64 = 0
  lastBlockLogicalTime: uint64 = 0
}

@message(0x2001)
struct Touch {}

type GateExternalMsg = Touch

contract ExternalGate {
  storage: ExternalState
  incomingExternal: GateExternalMsg
  namespace "external-gate"
  chain "avm-local"

  @store
  func ExternalState.load() {
    return ExternalState.fromChunk(contract.getData())
  }

  @store
  func ExternalState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy GateExternalMsg.fromSegment(inMsg)

    match (msg) {
      Touch => {
        var st = lazy ExternalState.load()
        assert (st.nonce == 7) throw ERR_BAD_NONCE
        st.nonce += 1
        st.lastNow += 1
        st.lastLogicalTime = logicalTime()
        st.lastBlockLogicalTime = currentBlockLogicalTime()
        st.save()
      }

      else => {
        assert (inMsg.isEmpty()) throw ERR_BAD_MSG
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get
  func currentNonce(): uint64 {
    const st = lazy ExternalState.load()
    return st.nonce
  }
}
`

const mapSurfaceSource = `
@storage
struct MapState {
  total: uint64 = 0
}

@message(61)
struct Build {}

type MapExternalMsg = Build

contract MapDemo {
  storage: MapState
  incomingExternal: MapExternalMsg
  namespace "map-demo"
  chain "avm-local"

  @store
  func MapState.load() {
    return MapState.fromChunk(contract.getData())
  }

  @store
  func MapState.save(self) {
    contract.setData(self.toChunk())
  }

  @external(inMsg: Segment)
  func onExternalMessage(inMsg: Segment) {
    const msg = lazy MapExternalMsg.fromSegment(inMsg)

    match (msg) {
      Build => {
        var m = Map.empty()
        m = m.set(getAddress(), getAddress())
        assert (m.has(getAddress())) throw 700
        const owner = m.get(getAddress())
        assert (owner != null) throw 701
        const keys = m.keys(10)
        const entries = m.entries(10)
        m = m.delete(getAddress())
        assert (!m.has(getAddress())) throw 702
        assert (keys.len() == entries.len()) throw 703
        var st = lazy MapState.load()
        st.total = m.len() + keys.len() + entries.len()
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
  func total(): uint64 {
    const st = lazy MapState.load()
    return st.total
  }
}
`

func TestConformanceExternalContextAndRollback(t *testing.T) {
	res := compileConformance(t, externalContextSurfaceSource)
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	encode := func(v avm.RuntimeValue) []byte {
		t.Helper()
		bz, err := avm.CanonicalEncode(v)
		require.NoError(t, err)
		return bz
	}
	storage := avm.Storage{
		"nonce":                encode(avm.ValueUint32(7)),
		"lastNow":              encode(avm.ValueInt64(0)),
		"lastLogicalTime":      encode(avm.ValueUint64(0)),
		"lastBlockLogicalTime": encode(avm.ValueUint64(0)),
	}

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	ctx := avm.RuntimeContext{
		Entry:           avm.EntryReceiveExternal,
		ContractAddress: testAddress(7),
		Message: async.MessageEnvelope{
			Opcode:   0x2001,
			QueryID:  8,
			GasLimit: 100_000,
		},
		BlockTimestamp:          1700000123,
		LogicalTime:             555,
		CurrentBlockLogicalTime: 777,
		GasLimit:                100_000,
	}

	exec, err := runner.Run(res.Module, storage, ctx)
	require.NoError(t, err)
	require.Equal(t, uint32(async.ResultOK), exec.ResultCode)

	nonceValue, err := avm.CanonicalDecodeExact(exec.State["nonce"])
	require.NoError(t, err)
	nonce, err := nonceValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(8), nonce)

	lastNowValue, err := avm.CanonicalDecodeExact(exec.State["lastNow"])
	require.NoError(t, err)
	lastNow, err := lastNowValue.AsInt64()
	require.NoError(t, err)
	require.Equal(t, int64(1), lastNow)

	lastLogicalTimeValue, err := avm.CanonicalDecodeExact(exec.State["lastLogicalTime"])
	require.NoError(t, err)
	lastLogicalTime, err := lastLogicalTimeValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(555), lastLogicalTime)

	lastBlockLogicalTimeValue, err := avm.CanonicalDecodeExact(exec.State["lastBlockLogicalTime"])
	require.NoError(t, err)
	lastBlockLogicalTime, err := lastBlockLogicalTimeValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(777), lastBlockLogicalTime)

	badExec, err := runner.Run(res.Module, exec.State, ctx)
	require.ErrorContains(t, err, "AVM abort with exit code 401")
	require.Equal(t, uint32(401), badExec.ResultCode)
	require.Equal(t, exec.State, badExec.State)
}

func TestConformanceMapDeterministicIterationAndDeletion(t *testing.T) {
	res := compileConformance(t, mapSurfaceSource)
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	initialState := avm.Storage{"total": avm.EncodeU64(0)}

	externalExec, err := runner.Run(res.Module, initialState, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: 61, QueryID: 61, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, externalExec.ResultCode)
	require.Equal(t, uint64(2), avm.DecodeU64(externalExec.State["total"]))

	getExec, err := runner.Run(res.Module, externalExec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "total"), QueryID: 63, GasLimit: 100_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, getExec.ResultCode)
	gotReturn, err := getExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(2), gotReturn)
	require.Equal(t, externalExec.State, getExec.State)
}

func TestConformanceStandardTrackArtifactsAreStable(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{name: "counter", src: counterStandardSource},
		{name: "treasury", src: treasuryStandardSource},
		{name: "token", src: tokenStandardSource},
		{name: "nft", src: nftStandardSource},
		{name: "dex", src: dexStandardSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := compileConformance(t, tc.src)
			second := compileConformance(t, tc.src)
			require.NoError(t, avm.VerifyInterface(first.Module, first.Manifest))
			require.NoError(t, avm.VerifyInterface(second.Module, second.Manifest))

			require.Equal(t, first.ModuleBytes, second.ModuleBytes)
			require.Equal(t, first.ModuleHash, second.ModuleHash)
			require.Equal(t, first.ManifestHash, second.ManifestHash)
			require.Equal(t, first.StateInitHash, second.StateInitHash)
			require.Equal(t, first.CodeChunkHash, second.CodeChunkHash)
			require.Equal(t, first.SelectorRegistry.RegistryHash, second.SelectorRegistry.RegistryHash)
			require.Equal(t, first.StorageLayout.LayoutHash, second.StorageLayout.LayoutHash)
			require.Equal(t, first.StorageCodec.Hash, second.StorageCodec.Hash)
			require.Equal(t, first.DependencyLock.LockHash, second.DependencyLock.LockHash)
			require.Equal(t, first.Manifest.WalletActions, second.Manifest.WalletActions)
			require.Equal(t, first.Manifest.GetMethods, second.Manifest.GetMethods)
		})
	}
}

func TestConformanceStandardTrackRejectsSelectorCollisionsGetterMutationAndGasExhaustion(t *testing.T) {
	// Message opcodes are the only dispatch selectors left in ATLX; two
	// @message structs pinned to the same opcode must be rejected.
	collisionSource := `
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
  namespace "counter"
  chain "avm-local"

  @internal
  func onInternalMessage(in: InMessage) {
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }
}
`
	_, err := compileConformanceMaybeErr(t, collisionSource)
	require.ErrorContains(t, err, "is already bound to message schema")

	getterMutation := avm.Module{
		Version: avm.Version,
		Imports: []avm.HostFunction{
			avm.HostWriteStorage,
			avm.HostReturn,
		},
		Exports: map[avm.Entrypoint]uint32{
			avm.EntryQuery: 0,
		},
		Code: []avm.Instruction{
			{Op: avm.OpPushU64, Arg: 1},
			{Op: avm.OpWriteStorage, Data: []byte("count")},
			{Op: avm.OpReturn, Arg: uint64(async.ResultOK)},
		},
	}
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	_, err = runner.Run(getterMutation, avm.Storage{"count": avm.EncodeU64(0)}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: 13, GasLimit: 100_000},
	})
	require.ErrorContains(t, err, "getter entrypoint cannot write storage")

	token := compileConformance(t, tokenStandardSource)
	runner, err = avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	_, err = runner.Run(token.Module, avm.Storage{
		"supply":  avm.EncodeU64(0),
		"vault":   avm.EncodeU64(0),
		"version": avm.EncodeU64(1),
	}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		GasLimit: 0,
		Message:  async.MessageEnvelope{GasLimit: 0},
	})
	require.ErrorContains(t, err, "gas limit")
}

func TestConformanceReviewGateMeasuresGasMemoryCodeQueueAndStateGrowth(t *testing.T) {
	policy := wasmconfig.DefaultPolicy()
	require.NoError(t, policy.Validate())

	gasModel := avm.DefaultGasSafetyModel()
	require.NoError(t, gasModel.ValidateGasLimit(gasModel.MaxGasTotal))

	token := compileConformance(t, tokenStandardSource)
	treasury := compileConformance(t, treasuryStandardSource)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	initialState := avm.Storage{
		"supply":  avm.EncodeU64(0),
		"vault":   avm.EncodeU64(0),
		"version": avm.EncodeU64(1),
	}

	execGas, err := runner.Run(token.Module, initialState, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		GasLimit: gasModel.MaxGasTotal,
		Message:  async.MessageEnvelope{GasLimit: gasModel.MaxGasTotal},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, execGas.ResultCode)
	require.Greater(t, execGas.GasUsed, uint64(0))
	require.LessOrEqual(t, execGas.GasUsed, gasModel.MaxGasTotal)
	require.LessOrEqual(t, uint64(len(token.ModuleBytes)), policy.MaxContractSizeBytes)

	require.LessOrEqual(t, uint64(len(treasury.ModuleBytes)), policy.MaxProposalContractSizeBytes)

	sandboxMemoryBytes := uint64(policy.MemoryCacheSizeMiB) * 1024 * 1024
	require.Greater(t, sandboxMemoryBytes, uint64(0))
	require.Greater(t, sandboxMemoryBytes/uint64(64*1024), uint64(0)) // >= one 64 KiB memory page

	queueParams := async.DefaultParams()
	require.NoError(t, queueParams.Validate())
	require.Greater(t, queueParams.MaxMessagesPerTx, uint32(0))
	require.Greater(t, queueParams.MaxMessagesPerBlock, uint32(0))
	require.Greater(t, queueParams.MaxQueuedMessagesPerContract, uint32(0))

	queueExec, err := async.NewExecutor(queueParams)
	require.NoError(t, err)
	queueContract := testAddress(3)
	for i := uint32(0); i < queueParams.MaxQueuedMessagesPerContract; i++ {
		require.NoError(t, queueExec.EnqueueTxMessages([]async.MessageEnvelope{{
			Source:             testAddress(4),
			Destination:        queueContract,
			Value:              sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
			Opcode:             11,
			QueryID:            uint64(i + 1),
			GasLimit:           100_000,
			ForwardFee:         reviewGateForwardFee(),
			CreatedLogicalTime: uint64(i + 1),
		}}))
	}
	require.ErrorContains(t, queueExec.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:             testAddress(4),
		Destination:        queueContract,
		Value:              sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
		Opcode:             11,
		QueryID:            uint64(queueParams.MaxQueuedMessagesPerContract + 1),
		GasLimit:           100_000,
		ForwardFee:         reviewGateForwardFee(),
		CreatedLogicalTime: uint64(queueParams.MaxQueuedMessagesPerContract + 1),
	}}), "queued messages per contract")

	stateGrowthParams := appparams.DefaultStateGrowthParams()
	stateGrowthParams.HighGrowthThresholdBytes = 1_000
	stateGrowthParams.SurchargeStepBps = 1_000
	stateGrowthParams.MaxSurchargeBps = 5_000
	stateGrowthParams.StateMaintenanceReserveBps = 2_000
	stateGrowthParams.DeleteRefundDecayBpsPerPeriod = 1_000

	stateGrowth, err := appparams.ComputeStateGrowthTelemetry(appparams.StateGrowthTelemetryInput{
		BlockHeight: 11,
		EpochID:     1,
		AccountDeltas: []appparams.StateGrowthAccountDelta{
			{ID: "contract-a", BytesAdded: 640, BytesRemoved: 128},
			{ID: "contract-b", BytesAdded: 512, BytesRemoved: 0},
			{ID: "contract-c", BytesAdded: 256, BytesRemoved: 64},
		},
		PreviousEpochNetGrowthBytes: 1_000,
		BaseStorageExpansionFeeNaet: sdkmath.NewInt(1_000),
		DeleteOriginalCostNaet:      sdkmath.NewInt(100),
		DeleteRefundNaet:            sdkmath.NewInt(25),
		StorageAgePeriods:           3,
		Params:                      stateGrowthParams,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1_216), stateGrowth.NetGrowthBytes)
	require.Greater(t, stateGrowth.SurchargeBps, int64(0))
	require.Greater(t, stateGrowth.StateMaintenanceReserveNaet.Int64(), int64(0))
	require.Len(t, stateGrowth.TopStateGrowthAccounts, 3)
}
