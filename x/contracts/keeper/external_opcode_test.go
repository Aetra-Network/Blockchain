package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// twoVariantExternalSource is a contract whose incomingExternal is a real
// two-variant union. Routing the right variant requires the message opcode:
// AddN(0x1001) adds its field to the counter, SubN(0x1002) subtracts. If the
// opcode is not threaded into external execution, the union match cannot tell
// AddN from SubN and silently falls to the no-op else arm.
const twoVariantExternalSource = `
@storage
struct Storage {
    total: uint64
}

@message(0x1001)
struct AddN {
    amount: uint64
}

@message(0x1002)
struct SubN {
    amount: uint64
}

type InternalMsg = AddN | SubN
type ExternalMsg = AddN | SubN

contract Accumulator {
    storage: Storage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg

    @store
    func Storage.load() {
        return Storage.fromChunk(contract.getData())
    }

    @store
    func Storage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        var st = lazy Storage.load()
        st.save()
    }

    @external
    func onExternalMessage(inMsg: Segment) {
        const msg = lazy ExternalMsg.fromSegment(inMsg)
        match (msg) {
            AddN => {
                var st = lazy Storage.load()
                st.total += msg.amount
                st.save()
            }
            SubN => {
                var st = lazy Storage.load()
                st.total -= msg.amount
                st.save()
            }
            else => {}
        }
    }

    @get
    func total(): uint64 {
        const st = lazy Storage.load()
        return st.total
    }
}
`

// TestExternalExecuteRoutesUnionMessageByOpcode is the regression guard for
// the live-discovered bug: MsgExecuteExternal carried no opcode, so the
// keeper passed 0 to the AVM and a union-typed incomingExternal could never
// route to the right variant — every external call fell to the no-op else
// arm while still bumping LogicalTime (making it look like it "ran"). With
// the opcode threaded through, AddN and SubN mutate state correctly, and a
// zero/unknown opcode routes to else (no state change).
func TestExternalExecuteRoutesUnionMessageByOpcode(t *testing.T) {
	owner := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})

	c := mustFamilyCompiler(t)
	res, err := c.Compile([]byte(twoVariantExternalSource))
	require.NoError(t, err)
	codeID := storeCompiledCode(t, &k, owner, res)

	initData, err := res.StorageCodec.Encode(map[string]any{"total": uint64(0)})
	require.NoError(t, err)
	deployed, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner, CodeID: codeID, InitMsg: initData, Funds: 10_000_000,
		Admin: owner, Salt: "accumulator", Height: 10,
	})
	require.NoError(t, err)

	addOp := res.MessageBodyOpcodes["AddN"]
	subOp := res.MessageBodyOpcodes["SubN"]
	require.NotEqual(t, addOp, subOp)

	addBody, err := res.MessageBodies["AddN"].Encode(map[string]any{"amount": uint64(7)})
	require.NoError(t, err)
	subBody, err := res.MessageBodies["SubN"].Encode(map[string]any{"amount": uint64(3)})
	require.NoError(t, err)

	// AddN(7): total 0 -> 7. Requires the opcode to route to the AddN arm.
	_, err = k.ExecuteExternal(types.MsgExecuteExternal{
		Sender: owner, ContractAddress: deployed.ContractAddressUser,
		Payload: addBody, Opcode: addOp, GasLimit: k.Params().MaxGasPerExecution, Height: 11,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(7), externalTotal(t, &k, deployed.ContractAddressUser))

	// SubN(3): total 7 -> 4. A different opcode must route to a different arm.
	_, err = k.ExecuteExternal(types.MsgExecuteExternal{
		Sender: owner, ContractAddress: deployed.ContractAddressUser,
		Payload: subBody, Opcode: subOp, GasLimit: k.Params().MaxGasPerExecution, Height: 12,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(4), externalTotal(t, &k, deployed.ContractAddressUser))

	// Opcode 0 (unroutable) falls to the else arm: no state change even
	// though the AddN body bytes are present. This is exactly the old broken
	// behavior, now reachable only when the caller omits the opcode.
	_, err = k.ExecuteExternal(types.MsgExecuteExternal{
		Sender: owner, ContractAddress: deployed.ContractAddressUser,
		Payload: addBody, Opcode: 0, GasLimit: k.Params().MaxGasPerExecution, Height: 13,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(4), externalTotal(t, &k, deployed.ContractAddressUser))
}

func externalTotal(t *testing.T, k *Keeper, address string) uint64 {
	t.Helper()
	q, err := k.Contract(types.QueryContractRequest{ContractAddress: address})
	require.NoError(t, err)
	require.True(t, q.Found)
	st, err := avm.DecodeSnapshot(q.Contract.Data)
	require.NoError(t, err)
	return avm.DecodeU64(st["total"])
}
