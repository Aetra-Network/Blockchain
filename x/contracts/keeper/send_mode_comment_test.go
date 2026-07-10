package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// vaultSource is a minimal "vault" contract: on a Withdraw external message it
// sends all its balance to the caller-provided sink and self-destructs, using
// the combined send mode SEND_DRAIN_BALANCE + SEND_DESTROY_IF_EMPTY and a
// textComment. The sink is a plain wallet that accepts any message.
const vaultSource = `
@storage
struct Storage {
    sink: address
}

@message(0x9001)
struct Withdraw {}

@message(0x9002)
struct Deposit {}

type InternalMsg = Deposit
type ExternalMsg = Withdraw

contract Vault {
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
            Withdraw => {
                const st = lazy Storage.load()
                const out = buildMessage({
                    bounce: false,
                    amount: 0,
                    receiver: st.sink,
                    mode: SEND_DRAIN_BALANCE + SEND_DESTROY_IF_EMPTY,
                    textComment: "vault closed",
                    body: Deposit {}
                })
                out.send(SEND_DEFAULT)
            }
            else => {}
        }
    }

    @get
    func sink(): address {
        const st = lazy Storage.load()
        return st.sink
    }
}
`

const sinkWalletSource = `
@storage
struct Storage {
    total: uint64
}

@message(0x9002)
struct Deposit {}

type InternalMsg = Deposit
type ExternalMsg = Deposit

contract Sink {
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
    }
}
`

// TestSendDrainBalanceDestroyAndComment is the regression + acceptance guard
// for the withdraw-everything-and-self-destruct idiom plus textComment: a
// vault, on Withdraw, drains its full balance to a sink with a memo and marks
// itself deleted. It proves (1) SEND_DRAIN_BALANCE sends the full balance,
// (2) SEND_DESTROY_IF_EMPTY deactivates the emptied source irreversibly,
// (3) the combined mode compiles and threads through, (4) the queued message
// carries the textComment for the explorer, (5) the sink actually receives
// the funds.
func TestSendDrainBalanceDestroyAndComment(t *testing.T) {
	owner := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})
	gs := k.ExportGenesis()
	gs.Params.MaxInternalMessageGasPerBlock = 1_000_000_000
	require.NoError(t, k.InitGenesis(gs))

	c := mustFamilyCompiler(t)
	sinkRes, err := c.Compile([]byte(sinkWalletSource))
	require.NoError(t, err)
	vaultRes, err := c.Compile([]byte(vaultSource))
	require.NoError(t, err)

	sinkCode := storeCompiledCode(t, &k, owner, sinkRes)
	vaultCode := storeCompiledCode(t, &k, owner, vaultRes)

	sinkInit, err := sinkRes.StorageCodec.Encode(map[string]any{"total": uint64(0)})
	require.NoError(t, err)
	sink, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner, CodeID: sinkCode, InitMsg: sinkInit, Funds: 1_000_000,
		Admin: owner, Salt: "sink", Height: 10,
	})
	require.NoError(t, err)

	const vaultStart = uint64(4_000_000_000)
	vaultInit, err := vaultRes.StorageCodec.Encode(map[string]any{"sink": sink.ContractAddressUser})
	require.NoError(t, err)
	vault, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner, CodeID: vaultCode, InitMsg: vaultInit, Funds: vaultStart,
		Admin: owner, Salt: "vault", Height: 11,
	})
	require.NoError(t, err)

	// External Withdraw: the vault drains + self-destructs.
	withdrawBody, err := vaultRes.MessageBodies["Withdraw"].Encode(map[string]any{})
	require.NoError(t, err)
	_, err = k.ExecuteExternal(types.MsgExecuteExternal{
		Sender: owner, ContractAddress: vault.ContractAddressUser,
		Payload: withdrawBody, Opcode: vaultRes.MessageBodyOpcodes["Withdraw"],
		GasLimit: k.Params().MaxGasPerExecution, Height: 12,
	})
	require.NoError(t, err)

	// The vault queued a drain message carrying the full balance + comment.
	queue := k.ExportGenesis().State.InternalMessages
	require.Len(t, queue, 1)
	drainMsg := queue[0]
	require.Equal(t, vault.ContractAddressUser, drainMsg.SourceContractUser)
	require.Equal(t, sink.ContractAddressUser, drainMsg.DestinationAccount)
	require.Equal(t, "vault closed", drainMsg.Comment, "textComment must flow to the queued message")
	require.Equal(t, uint32(128|32), drainMsg.Mode, "combined SEND_DRAIN_BALANCE|SEND_DESTROY_IF_EMPTY must be recorded")
	// The drained funds equal the vault's balance at emit (minus any rent
	// charged during execution); it should be close to the full start.
	require.Greater(t, drainMsg.Funds, vaultStart-10_000_000)

	// Deliver the drain message via the autonomous drain.
	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 13}, false, log.NewNopLogger())
	drainQueue(t, &k, ctx)

	// Vault is now destroyed: status deleted, balance 0, storage cleared.
	vaultQ, err := k.Contract(types.QueryContractRequest{ContractAddress: vault.ContractAddressUser})
	require.NoError(t, err)
	require.True(t, vaultQ.Found)
	require.Equal(t, types.ContractStatusDeleted, vaultQ.Contract.Status)
	require.Equal(t, uint64(0), vaultQ.Contract.Balance)
	require.Empty(t, vaultQ.Contract.Data)

	// The sink received the drained funds.
	sinkQ, err := k.Contract(types.QueryContractRequest{ContractAddress: sink.ContractAddressUser})
	require.NoError(t, err)
	require.Equal(t, drainMsg.Funds+1_000_000, sinkQ.Contract.Balance)
}

// TestSendModeIllogicalCombinationsRejected pins the compile-time validation
// of send-mode combinations: mutually exclusive or exclusive-only flags fail.
func TestSendModeIllogicalCombinationsRejected(t *testing.T) {
	c := mustFamilyCompiler(t)
	base := func(mode string) string {
		return `
@storage
struct Storage { x: uint64 }
@message(0x1) struct M {}
type InternalMsg = M
type ExternalMsg = M
contract C {
    storage: Storage
    incomingMessages: InternalMsg
    incomingExternal: ExternalMsg
    @store func Storage.load() { return Storage.fromChunk(contract.getData()) }
    @store func Storage.save(self) { contract.setData(self.toChunk()) }
    @internal func onInternalMessage(in: InMessage) {
        const out = buildMessage({ receiver: getAddress(), amount: 0, mode: ` + mode + `, body: M {} })
        out.send(SEND_DEFAULT)
    }
    @external func onExternalMessage(inMsg: Segment) {}
}`
	}
	// DRAIN + CARRY are mutually exclusive.
	_, err := c.Compile([]byte(base("SEND_DRAIN_BALANCE + SEND_CARRY_REMAINDER")))
	require.ErrorContains(t, err, "mutually exclusive")
	// ESTIMATE_ONLY cannot combine with others.
	_, err = c.Compile([]byte(base("SEND_ESTIMATE_ONLY + SEND_IGNORE_ERRORS")))
	require.ErrorContains(t, err, "cannot be combined")
	// A valid combination compiles.
	_, err = c.Compile([]byte(base("SEND_DRAIN_BALANCE + SEND_DESTROY_IF_EMPTY + SEND_IGNORE_ERRORS")))
	require.NoError(t, err)
}
