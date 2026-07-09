package keeper

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

const genericFamilyTemplate = `
const ERR_BAD_NONCE = 1003
const ERR_BAD_MSG = 0xFFFF
const ERR_BAD_AMOUNT = 1005

@storage
struct %[1]sStorage {
    value: uint64
    pending: uint64
    nonce: uint32
}

@message(0x7101)
struct Primary {
    amount: uint64
}

@message(0x7102)
struct Secondary {
    amount: uint64
}

@message(0x7103)
struct Touch {
    nonce: uint32
}

type InternalMsg = Primary | Secondary
type ExternalMsg = Touch

contract %[1]s {
    author: "Aetralis acceptance"
    description: "%[1]s family acceptance"
    version: "0.01.0"

    storage: %[1]sStorage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg

    @store
    func %[1]sStorage.load() {
        return %[1]sStorage.fromChunk(contract.getData())
    }

    @store
    func %[1]sStorage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy InternalMsg.fromSegment(in.body)

        match (msg) {
            Primary => {
                var st = lazy %[1]sStorage.load()
                st.value += msg.amount
                st.pending = msg.amount

                const outbound = buildMessage({
                    bounce: BounceMode.Only256BitsOfBody,
                    amount: 0,
                    receiver: getAddress(),
                    body: Primary {
                        amount: msg.amount,
                    }
                })

                outbound.send(SEND_BOUNCE_ON_FAIL)
                st.save()
            }

            Secondary => {
                var st = lazy %[1]sStorage.load()
                assert (st.value >= msg.amount) throw ERR_BAD_AMOUNT
                st.value -= msg.amount
                st.save()
            }

            else => {
                assert (in.body.isEmpty()) throw ERR_BAD_MSG
            }
        }
    }

    @bounced
    func onBouncedMessage(in: InMessageBounced) {
        in.bouncedBody.skipBouncedPrefix()

        const bounced = lazy Primary.fromSegment(in.bouncedBody)
        var st = lazy %[1]sStorage.load()

        if (st.pending != 0 && st.pending == bounced.amount) {
            st.value -= bounced.amount
            st.pending = 0
            st.save()
        }
    }

    @external(inMsg: Segment)
    func onExternalMessage(inMsg: Segment) {
        const msg = lazy Touch.fromSegment(inMsg)
        var st = lazy %[1]sStorage.load()

        assert (msg.nonce == st.nonce + 1) throw ERR_BAD_NONCE
        st.nonce = msg.nonce
        st.value += 1
        st.save()
    }

    @get
    func value(): uint64 {
        const st = lazy %[1]sStorage.load()
        return st.value
    }

    @get
    func nonce(): uint32 {
        const st = lazy %[1]sStorage.load()
        return st.nonce
    }
}
`

const registryFamilyTemplate = `
const ERR_BAD_NONCE = 1003
const ERR_BAD_MSG = 0xFFFF
const ERR_BAD_AMOUNT = 1005

@storage
struct %[1]sStorage {
    value: uint64
    pending: uint64
    nonce: uint32
}

@message(0x7201)
struct Register {
    amount: uint64
}

@message(0x7202)
struct Secondary {
    amount: uint64
}

@message(0x7203)
struct Touch {
    nonce: uint32
}

type InternalMsg = Register | Secondary
type ExternalMsg = Touch

contract %[1]s {
    author: "Aetralis acceptance"
    description: "%[1]s family acceptance"
    version: "0.01.0"

    storage: %[1]sStorage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg

    @store
    func %[1]sStorage.load() {
        return %[1]sStorage.fromChunk(contract.getData())
    }

    @store
    func %[1]sStorage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy InternalMsg.fromSegment(in.body)

        match (msg) {
            Register => {
                var st = lazy %[1]sStorage.load()
                st.value += msg.amount
                st.pending = msg.amount

                const outbound = buildMessage({
                    bounce: BounceMode.Only256BitsOfBody,
                    amount: 0,
                    receiver: getAddress(),
                    body: Register {
                        amount: msg.amount,
                    }
                })

                outbound.send(SEND_BOUNCE_ON_FAIL)
                st.save()
            }

            Secondary => {
                var st = lazy %[1]sStorage.load()
                assert (st.value >= msg.amount) throw ERR_BAD_AMOUNT
                st.value -= msg.amount
                st.save()
            }

            else => {
                assert (in.body.isEmpty()) throw ERR_BAD_MSG
            }
        }
    }

    @bounced
    func onBouncedMessage(in: InMessageBounced) {
        in.bouncedBody.skipBouncedPrefix()

        const bounced = lazy Register.fromSegment(in.bouncedBody)
        var st = lazy %[1]sStorage.load()

        if (st.pending != 0 && st.pending == bounced.amount) {
            st.value -= bounced.amount
            st.pending = 0
            st.save()
        }
    }

    @external(inMsg: Segment)
    func onExternalMessage(inMsg: Segment) {
        const msg = lazy Touch.fromSegment(inMsg)
        var st = lazy %[1]sStorage.load()

        assert (msg.nonce == st.nonce + 1) throw ERR_BAD_NONCE
        var reg = Map.empty()
        reg = reg.set(getAddress(), getAddress())
        assert (reg.has(getAddress())) throw ERR_BAD_MSG
        const owner = reg.get(getAddress())
        assert (owner != null) throw ERR_BAD_MSG
        const keys = reg.keys(10)
        const entries = reg.entries(10)
        reg = reg.delete(getAddress())
        assert (!reg.has(getAddress())) throw ERR_BAD_MSG

        st.nonce = msg.nonce
        st.value += keys.len() + entries.len()
        st.save()
    }

    @get
    func value(): uint64 {
        const st = lazy %[1]sStorage.load()
        return st.value
    }

    @get
    func nonce(): uint32 {
        const st = lazy %[1]sStorage.load()
        return st.nonce
    }
}
`

const minerFamilyTemplate = `
const ERR_BAD_NONCE = 1003
const ERR_BAD_MSG = 0xFFFF
const ERR_BAD_AMOUNT = 1005

@storage
struct %[1]sStorage {
    value: uint64
    pending: uint64
    nonce: uint32
    bestHash: hash32
}

@message(0x7301)
struct Mine {
    amount: uint64
}

@message(0x7302)
struct Secondary {
    amount: uint64
}

@message(0x7303)
struct Touch {
    nonce: uint32
}

type InternalMsg = Mine | Secondary
type ExternalMsg = Touch

contract %[1]s {
    author: "Aetralis acceptance"
    description: "%[1]s family acceptance"
    version: "0.01.0"

    storage: %[1]sStorage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg

    @store
    func %[1]sStorage.load() {
        return %[1]sStorage.fromChunk(contract.getData())
    }

    @store
    func %[1]sStorage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy InternalMsg.fromSegment(in.body)

        match (msg) {
            Mine => {
                var st = lazy %[1]sStorage.load()
                st.value += msg.amount
                st.pending = msg.amount
                st.bestHash = hash(Chunk.fromHex("4142564d"))

                const outbound = buildMessage({
                    bounce: BounceMode.Only256BitsOfBody,
                    amount: 0,
                    receiver: getAddress(),
                    body: Mine {
                        amount: msg.amount,
                    }
                })

                outbound.send(SEND_BOUNCE_ON_FAIL)
                st.save()
            }

            Secondary => {
                var st = lazy %[1]sStorage.load()
                assert (st.value >= msg.amount) throw ERR_BAD_AMOUNT
                st.value -= msg.amount
                st.save()
            }

            else => {
                assert (in.body.isEmpty()) throw ERR_BAD_MSG
            }
        }
    }

    @bounced
    func onBouncedMessage(in: InMessageBounced) {
        in.bouncedBody.skipBouncedPrefix()

        const bounced = lazy Mine.fromSegment(in.bouncedBody)
        var st = lazy %[1]sStorage.load()

        if (st.pending != 0 && st.pending == bounced.amount) {
            st.value -= bounced.amount
            st.pending = 0
            st.save()
        }
    }

    @external(inMsg: Segment)
    func onExternalMessage(inMsg: Segment) {
        const msg = lazy Touch.fromSegment(inMsg)
        var st = lazy %[1]sStorage.load()

        assert (msg.nonce == st.nonce + 1) throw ERR_BAD_NONCE
        st.nonce = msg.nonce
        st.value += 1
        st.save()
    }

    @get
    func value(): uint64 {
        const st = lazy %[1]sStorage.load()
        return st.value
    }

    @get
    func bestHash(): hash32 {
        const st = lazy %[1]sStorage.load()
        return st.bestHash
    }
}
`

func TestAcceptanceCounterShouldBeATLXExample(t *testing.T) {
	t.Helper()
	srcPath := filepath.Clean("../../../examples/avm/counter_should_be.atlx")
	src, err := os.ReadFile(srcPath)
	require.NoError(t, err)

	c := mustFamilyCompiler(t)
	res, err := c.Compile(src)
	require.NoError(t, err)
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	keeper := NewKeeperWithAccountStatus(testAccountStatus{aeAddress("11"): accountStatusActive})
	codeID := storeCompiledCode(t, &keeper, aeAddress("11"), res)
	initData, err := res.StorageCodec.Encode(map[string]any{
		"counter":    int64(0),
		"owner":      aeAddress("11"),
		"target":     aeAddress("22"),
		"nonce":      uint32(0),
		"lastNow":    int64(0),
		"lastBalance": uint64(0),
		"lastRandom": uint64(0),
		"pingTicket": nil,
		"box":        nil,
		"packed":     nil,
	})
	require.NoError(t, err)

	created, err := keeper.InstantiateContract(types.MsgInstantiateContract{
		Creator: aeAddress("11"),
		CodeID:  codeID,
		InitMsg: initData,
		Funds:   100_000_000,
		Admin:   aeAddress("11"),
		Salt:    "counter-acceptance",
		Height:  10,
	})
	require.NoError(t, err)

	query, err := keeper.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, initData, query.Contract.Data)

	internalBody, err := res.MessageBodies["Inc"].Encode(map[string]any{
		"by":     uint32(3),
		"ticket": uint32(77),
	})
	require.NoError(t, err)
	_, err = deliverInternalForTest(t, &keeper, types.InternalMessage{
		SourceContractUser: created.ContractAddressUser,
		DestinationAccount: created.ContractAddressUser,
		Funds:              0,
		Opcode:             res.MessageBodyOpcodes["Inc"],
		QueryID:            1,
		Body:               internalBody,
		Bounce:             true,
		GasLimit:           100_000,
		LogicalTime:        1,
		Height:             11,
	})
	require.NoError(t, err)

	query, err = keeper.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.NotEqual(t, initData, query.Contract.Data)
	require.Equal(t, types.ComputeContractStateRoot(query.Contract), query.Contract.StateRoot)

	queue, err := keeper.ContractQueue(types.QueryContractQueueRequest{ContractAddress: created.ContractAddressUser, Pagination: types.PageRequest{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, queue, 1)

	contractState, err := avm.DecodeSnapshot(query.Contract.Data)
	require.NoError(t, err)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	getter, err := runner.Run(res.Module, contractState, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 100_000,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res.Manifest, "currentCounter"), QueryID: 13, GasLimit: 100_000},
	})
	require.NoError(t, err)
	got, err := getter.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(3), got)

	afterInternal, err := keeper.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.NotEqual(t, initData, afterInternal.Contract.Data)

	badTargetBody, err := res.MessageBodies["SetTarget"].Encode(map[string]any{"target": aeAddress("33")})
	require.NoError(t, err)
	_, err = deliverInternalForTest(t, &keeper, types.InternalMessage{
		SourceContractUser: created.ContractAddressUser,
		DestinationAccount: created.ContractAddressUser,
		Funds:              0,
		Opcode:             res.MessageBodyOpcodes["SetTarget"],
		QueryID:            2,
		Body:               badTargetBody,
		Bounce:             true,
		GasLimit:           100_000,
		LogicalTime:        2,
		Height:             13,
	})
	require.Error(t, err)

	finalQuery, err := keeper.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, afterInternal.Contract.Data, finalQuery.Contract.Data)

	bouncedBody, err := res.MessageBodies["Ping"].Encode(map[string]any{
		"ticket":  uint32(77),
		"counter": int64(3),
	})
	require.NoError(t, err)
	bouncedExec, err := runner.Run(res.Module, contractState, avm.RuntimeContext{
		Entry:           avm.EntryReceiveBounced,
		ContractAddress: mustAccAddress(t, created.ContractAddressUser),
		GasLimit:        100_000,
		Message: async.MessageEnvelope{
			Opcode:  res.MessageBodyOpcodes["Ping"],
			QueryID: 3,
			Body:    bouncedBody,
			GasLimit: 100_000,
			Bounced: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, uint32(async.ResultOK), bouncedExec.ResultCode)
	require.Equal(t, uint64(3), mustUint64Field(t, bouncedExec.State, "counter"))
}

func TestAcceptanceFamilyContractsByFamilyLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name              string
		source            string
		expectedAfterTouch uint64
	}{
		{name: "NFT", source: familySource("NFT"), expectedAfterTouch: 1},
		{name: "SBT", source: familySource("SBT"), expectedAfterTouch: 1},
		{name: "DomainRegistry", source: familySource("DomainRegistry"), expectedAfterTouch: 1},
		{name: "DEX", source: familySource("DEX"), expectedAfterTouch: 1},
		{name: "MinerPoW", source: familySource("MinerPoW"), expectedAfterTouch: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mustFamilyCompiler(t)
			res, err := c.Compile([]byte(tc.source))
			require.NoError(t, err)
			require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

			initMap := map[string]any{
				"value":  uint64(0),
				"pending": uint64(0),
				"nonce":   uint32(0),
			}
			initData, err := res.StorageCodec.Encode(initMap)
			require.NoError(t, err)

			keeper := NewKeeperWithAccountStatus(testAccountStatus{aeAddress("11"): accountStatusActive})
			codeID := storeCompiledCode(t, &keeper, aeAddress("11"), res)
			created, err := keeper.InstantiateContract(types.MsgInstantiateContract{
				Creator: aeAddress("11"),
				CodeID:  codeID,
				InitMsg: initData,
				Funds:   100_000_000,
				Admin:   aeAddress("11"),
				Salt:    tc.name + "-acceptance",
				Height:  20,
			})
			require.NoError(t, err)

			query, err := keeper.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
			require.NoError(t, err)
			require.Equal(t, initData, query.Contract.Data)
			require.NotEmpty(t, query.Contract.StateRoot)

			body, err := res.MessageBodies["Touch"].Encode(map[string]any{"nonce": uint32(1)})
			require.NoError(t, err)
			_, err = keeper.ExecuteContract(types.MsgExecuteContract{
				Sender:          aeAddress("11"),
				ContractAddress: created.ContractAddressUser,
				Msg:             body,
				Height:          21,
			})
			require.NoError(t, err)

			afterExternal, err := keeper.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
			require.NoError(t, err)
			require.NotEqual(t, query.Contract.StateRoot, afterExternal.Contract.StateRoot)
			afterExternalStorage, err := avm.DecodeSnapshot(afterExternal.Contract.Data)
			require.NoError(t, err)
			require.Equal(t, tc.expectedAfterTouch, mustUint64Field(t, afterExternalStorage, "value"))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

			primaryBody, err := res.MessageBodies["Primary"].Encode(map[string]any{"amount": uint32(5)})
			require.NoError(t, err)
			_, err = deliverInternalForTest(t, &keeper, types.InternalMessage{
				SourceContractUser: created.ContractAddressUser,
				DestinationAccount: created.ContractAddressUser,
				Funds:              0,
				Opcode:             res.MessageBodyOpcodes["Primary"],
				QueryID:            2,
				Body:               primaryBody,
				Bounce:             true,
				GasLimit:           100_000,
				LogicalTime:        2,
				Height:             22,
			})
			require.NoError(t, err)

			internalQuery, err := keeper.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
			require.NoError(t, err)
			require.NotEqual(t, afterExternal.Contract.Data, internalQuery.Contract.Data)
			internalStorage, err := avm.DecodeSnapshot(internalQuery.Contract.Data)
			require.NoError(t, err)
			require.Equal(t, tc.expectedAfterTouch+5, mustUint64Field(t, internalStorage, "value"))

			secondaryBody, err := res.MessageBodies["Secondary"].Encode(map[string]any{"amount": uint32(1_000_000)})
			require.NoError(t, err)
			_, err = deliverInternalForTest(t, &keeper, types.InternalMessage{
				SourceContractUser: created.ContractAddressUser,
				DestinationAccount: created.ContractAddressUser,
				Funds:              0,
				Opcode:             res.MessageBodyOpcodes["Secondary"],
				QueryID:            3,
				Body:               secondaryBody,
				Bounce:             true,
				GasLimit:           100_000,
				LogicalTime:        3,
				Height:             23,
			})
			require.Error(t, err)

			rollbackQuery, err := keeper.Contract(types.QueryContractRequest{ContractAddress: created.ContractAddressUser})
			require.NoError(t, err)
			require.Equal(t, internalQuery.Contract.Data, rollbackQuery.Contract.Data)

			getExec, err := runner.Run(res.Module, afterExternalStorage, avm.RuntimeContext{
				Entry:    avm.EntryQuery,
				GasLimit: 100_000,
				Message:  async.MessageEnvelope{Opcode: getterSelector(t, res.Manifest, "value"), QueryID: 9, GasLimit: 100_000},
			})
			require.NoError(t, err)
			got, err := getExec.ReturnValue.AsUint64()
			require.NoError(t, err)
			require.Equal(t, tc.expectedAfterTouch, got)

			bouncedExec, err := runner.Run(res.Module, internalStorage, avm.RuntimeContext{
				Entry:           avm.EntryReceiveBounced,
				ContractAddress: mustAccAddress(t, created.ContractAddressUser),
				GasLimit:        100_000,
				Message: async.MessageEnvelope{
					Opcode:  res.MessageBodyOpcodes["Primary"],
					QueryID: 4,
					Body:    primaryBody,
					GasLimit: 100_000,
					Bounced: true,
				},
			})
			require.NoError(t, err)
			require.Equal(t, uint32(async.ResultOK), bouncedExec.ResultCode)
			require.Equal(t, tc.expectedAfterTouch, mustUint64Field(t, bouncedExec.State, "value"))
			require.Zero(t, mustUint64Field(t, bouncedExec.State, "pending"))
			require.NotNil(t, bouncedExec.State)

		})
	}
}

func familySource(name string) string {
	return fmt.Sprintf(genericFamilyTemplate, name)
}

func mustFamilyCompiler(t *testing.T) *compiler.Compiler {
	t.Helper()
	c, err := compiler.New(compiler.DefaultOptions())
	require.NoError(t, err)
	return c
}

func storeCompiledCode(t *testing.T, k *Keeper, owner string, res *compiler.Result) string {
	t.Helper()
	response, err := k.StoreCode(types.MsgStoreCode{
		Authority: owner,
		Bytecode:  res.ModuleBytes,
	})
	require.NoError(t, err)
	return response.CodeID
}

func getterSelector(t *testing.T, manifest avm.InterfaceManifest, name string) uint32 {
	t.Helper()
	for _, method := range manifest.GetMethods {
		if method.Name == name {
			return method.Selector
		}
	}
	t.Fatalf("getter %q not found", name)
	return 0
}

func mustAccAddress(t *testing.T, address string) sdk.AccAddress {
	t.Helper()
	addr, err := addressing.ParseAccAddress(address)
	require.NoError(t, err)
	return addr
}

func mustUint64Field(t *testing.T, storage avm.Storage, field string) uint64 {
	t.Helper()
	value, ok := storage[field]
	require.True(t, ok)
	return avm.DecodeU64(value)
}
