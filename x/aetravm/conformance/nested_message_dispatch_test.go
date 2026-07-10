package conformance

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// nestedMessageDispatchSource models the generic TON-style "notification
// carries a differently-typed inner message in its forward payload" pattern:
// AllowedMessageToNftCollection.TransferNotificationForRecipient carries a
// forwardPayload field holding an AllowedInnerMessage (DeployNft or
// BatchDeployNfts), wrapped with wrapMessage() so its own opcode travels on
// the wire alongside its fields. The contract sends the notification to
// itself (via buildMessage + wrapMessage + .send()) so the wire bytes are
// produced and decoded by the real, compiled AVM runtime end to end, not
// hand-assembled by the test.
const nestedMessageDispatchSource = `
const MSG_FORWARD_VALUE = aet("0.0001")

@storage
struct DispatchStorage {
    lastAction: uint64
    lastValue: uint64
    pingCount: uint64
}

@message(0x9001)
struct Ping {
    nonce: uint64
}

@message(0x9002)
struct Kickoff {
    isBatch: bool
    value: uint64
}

@message(0x9003)
struct TransferNotificationForRecipient {
    from: address
    forwardPayload: bytes
}

type AllowedMessageToNftCollection = Ping | Kickoff | TransferNotificationForRecipient

@message(0xA001)
struct DeployNft {
    itemIndex: uint64
}

@message(0xA002)
struct BatchDeployNfts {
    count: uint64
}

type AllowedInnerMessage = DeployNft | BatchDeployNfts

contract NotifyDispatch {
    author: "Aetralis test"
    description: "nested message dispatch conformance"
    version: "0.01.0"

    storage: DispatchStorage
    incomingMessages: AllowedMessageToNftCollection

    @store
    func DispatchStorage.load() {
        return DispatchStorage.fromChunk(contract.getData())
    }

    @store
    func DispatchStorage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy AllowedMessageToNftCollection.fromSegment(in.body)

        match (msg) {
            Ping => {
                var st = lazy DispatchStorage.load()
                st.pingCount += 1
                st.save()
            }

            Kickoff => {
                if (msg.isBatch) {
                    const notify = buildMessage({
                        mode: SEND_BOUNCE_ON_FAIL,
                        bounce: BounceMode.Only256BitsOfBody,
                        amount: MSG_FORWARD_VALUE,
                        receiver: getAddress(),
                        body: TransferNotificationForRecipient {
                            from: getAddress(),
                            forwardPayload: wrapMessage(BatchDeployNfts { count: msg.value }),
                        }
                    })
                    notify.send()
                } else {
                    const notify = buildMessage({
                        mode: SEND_BOUNCE_ON_FAIL,
                        bounce: BounceMode.Only256BitsOfBody,
                        amount: MSG_FORWARD_VALUE,
                        receiver: getAddress(),
                        body: TransferNotificationForRecipient {
                            from: getAddress(),
                            forwardPayload: wrapMessage(DeployNft { itemIndex: msg.value }),
                        }
                    })
                    notify.send()
                }
            }

            TransferNotificationForRecipient => {
                const forwardPayload = msg.forwardPayload
                const innerMsg = lazy AllowedInnerMessage.fromChunk(forwardPayload)
                var st = lazy DispatchStorage.load()

                match (innerMsg) {
                    DeployNft(itemIndex) => {
                        st.lastAction = 1
                        st.lastValue = itemIndex
                    }
                    BatchDeployNfts => {
                        st.lastAction = 2
                        st.lastValue = innerMsg.count
                    }
                }
                st.save()
            }
        }
    }

    @bounced
    func onBouncedMessage(in: InMessageBounced) {
    }

    @get
    func lastAction(): uint64 {
        const st = lazy DispatchStorage.load()
        return st.lastAction
    }

    @get
    func lastValue(): uint64 {
        const st = lazy DispatchStorage.load()
        return st.lastValue
    }

    @get
    func pingCount(): uint64 {
        const st = lazy DispatchStorage.load()
        return st.pingCount
    }
}
`

// TestAcceptanceNestedMessageDispatch drives the generalized "op + forward
// payload" nesting pattern end to end: a Kickoff message causes the contract
// to build and send a TransferNotificationForRecipient to itself with a
// wrapMessage()-wrapped inner payload; the contract then decodes that
// forwardPayload as a *second*, differently-typed message
// (DeployNft/BatchDeployNfts) and dispatches on it via a nested match whose
// scrutinee is not the handler's own top-level incoming message. It also
// exercises a plain top-level `match (msg)` arm (Ping) in the same handler as
// an explicit regression guard for the unchanged ctx.Message.Opcode path.
func TestAcceptanceNestedMessageDispatch(t *testing.T) {
	deployer := testAddress(0x91)
	sender := testAddress(0x92)
	opts := compiler.Options{DeployerAddress: addressing.FormatAccAddress(deployer)}

	c, err := compiler.New(opts)
	require.NoError(t, err)
	res, err := c.Compile([]byte(nestedMessageDispatchSource))
	require.NoError(t, err)
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	storage := mustAVMStorage(t, map[string]any{
		"lastAction": uint64(0),
		"lastValue":  uint64(0),
		"pingCount":  uint64(0),
	})
	snapshot := avm.EncodeSnapshot(storage)

	executor := mustExecutor(t)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	addr, err := executor.DeployContract(deployer, res.ModuleHash[:], []byte("notify-dispatch"), snapshot, sdkmath.NewInt(1_000_000))
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(addr, runner.AsyncHandler(res.Module, nil, avm.RuntimeContext{})))

	// Regression guard: a plain top-level match(msg) arm (Ping) must still
	// dispatch off ctx.Message.Opcode exactly as every other example does.
	pingBody := mustCodecBody(t, res.MessageBodies["Ping"], map[string]any{
		"nonce": uint64(1),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      sender,
		Destination: addr,
		Value:       acceptanceZeroCoin(),
		Opcode:      res.MessageBodyOpcodes["Ping"],
		QueryID:     1,
		Body:        pingBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "ping failed: %s", receipts[0].Error)
	require.Equal(t, uint64(1), runGetterUint64(t, runner, res, contractState(t, executor, addr), "pingCount", addr))
	require.Equal(t, uint64(0), runGetterUint64(t, runner, res, contractState(t, executor, addr), "lastAction", addr))

	// Kickoff(isBatch=false) -> the contract sends itself a
	// TransferNotificationForRecipient whose forwardPayload is a
	// wrapMessage()-wrapped DeployNft. The nested match must land on the
	// DeployNft arm (which destructures itemIndex via a pattern binding).
	singleBody := mustCodecBody(t, res.MessageBodies["Kickoff"], map[string]any{
		"isBatch": false,
		"value":   uint64(42),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      sender,
		Destination: addr,
		Value:       acceptanceZeroCoin(),
		Opcode:      res.MessageBodyOpcodes["Kickoff"],
		QueryID:     2,
		Body:        singleBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(2)
	require.NoError(t, err)
	require.Len(t, receipts, 2, "expected the Kickoff and the self-delivered notification")
	for _, r := range receipts {
		require.Equalf(t, async.ResultOK, r.ResultCode, "receipt failed: %s", r.Error)
	}
	require.Equal(t, uint64(1), runGetterUint64(t, runner, res, contractState(t, executor, addr), "lastAction", addr), "expected the DeployNft branch to run")
	require.Equal(t, uint64(42), runGetterUint64(t, runner, res, contractState(t, executor, addr), "lastValue", addr))

	// Kickoff(isBatch=true) -> forwardPayload wraps BatchDeployNfts instead;
	// the nested match must land on the BatchDeployNfts arm this time (which
	// reads its field via plain `.count` access on the decoded local, not a
	// pattern binding), proving the discriminant extraction is real and not
	// accidentally always-true/always-false.
	batchBody := mustCodecBody(t, res.MessageBodies["Kickoff"], map[string]any{
		"isBatch": true,
		"value":   uint64(7),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      sender,
		Destination: addr,
		Value:       acceptanceZeroCoin(),
		Opcode:      res.MessageBodyOpcodes["Kickoff"],
		QueryID:     3,
		Body:        batchBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(3)
	require.NoError(t, err)
	require.Len(t, receipts, 2, "expected the Kickoff and the self-delivered notification")
	for _, r := range receipts {
		require.Equalf(t, async.ResultOK, r.ResultCode, "receipt failed: %s", r.Error)
	}
	require.Equal(t, uint64(2), runGetterUint64(t, runner, res, contractState(t, executor, addr), "lastAction", addr), "expected the BatchDeployNfts branch to run")
	require.Equal(t, uint64(7), runGetterUint64(t, runner, res, contractState(t, executor, addr), "lastValue", addr))

	// Final regression check: pingCount is untouched by the nested-dispatch
	// traffic, confirming the two match statements' codegen paths don't
	// interfere with each other.
	require.Equal(t, uint64(1), runGetterUint64(t, runner, res, contractState(t, executor, addr), "pingCount", addr))
}
