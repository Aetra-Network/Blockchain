package async

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// TestBuildBounceMessagePreservesOriginalOpcode ensures a bounce carries the
// original message's opcode (so a bounced handler's match() can dispatch on the
// original message type) while the envelope Opcode stays the BounceOpcode marker.
func TestBuildBounceMessagePreservesOriginalOpcode(t *testing.T) {
	original := testMessage(testAddr(1), testAddr(2), 7)
	original.Opcode = 0x5201
	refund := RefundCalculation{Amount: sdkmath.NewInt(5), Fee: sdkmath.ZeroInt()}

	bounce, err := BuildBounceMessage(original, refund, sdkmath.NewInt(1))
	require.NoError(t, err)
	require.Equal(t, BounceOpcode, bounce.Opcode, "envelope opcode stays the bounce marker")
	require.Equal(t, uint32(0x5201), bounce.OriginalOpcode, "original opcode preserved for dispatch")
	require.True(t, bounce.Bounced)
}

// TestOutgoingValueRequiresSufficientBalance ensures a contract cannot emit more
// value than it holds — value is conserved, not minted per hop.
func TestOutgoingValueRequiresSufficientBalance(t *testing.T) {
	executor := newTestExecutor(t)
	deployer := testAddr(1)
	contract := deployTestContract(t, executor, deployer, []byte("value-cons"))

	require.NoError(t, executor.RegisterHandler(contract, func(c ContractAccount, msg MessageEnvelope) ExecutionResult {
		out := testMessage(contract, testAddr(9), 1)
		out.Value = naetCoin(1_000_000_000) // far more than the deploy balance
		return ExecutionResult{
			NewState:   []byte("x"),
			Outgoing:   []MessageEnvelope{out},
			ResultCode: ResultOK,
		}
	}))

	msg := testMessage(testAddr(9), contract, 1)
	msg.Value = naetCoin(0)
	msg.Bounce = false // avoid a follow-up bounce receipt; assert the failure directly
	require.NoError(t, executor.EnqueueTxMessages([]MessageEnvelope{msg}))
	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.NotEmpty(t, receipts)
	require.Equal(t, ResultInsufficientBalance, receipts[0].ResultCode)
}

// TestMailboxesPrunedAfterProcessing ensures inbox/outbox do not accumulate
// messages for the life of the chain (unbounded state / super-linear re-sort).
func TestMailboxesPrunedAfterProcessing(t *testing.T) {
	executor := newTestExecutor(t)
	deployer := testAddr(1)
	contract := deployTestContract(t, executor, deployer, []byte("prune"))
	require.NoError(t, executor.RegisterHandler(contract, func(c ContractAccount, msg MessageEnvelope) ExecutionResult {
		return ExecutionResult{NewState: []byte("s"), ResultCode: ResultOK}
	}))

	msg := testMessage(testAddr(9), contract, 1)
	msg.Value = naetCoin(0)
	require.NoError(t, executor.EnqueueTxMessages([]MessageEnvelope{msg}))
	require.NotEmpty(t, executor.inbox, "inbox holds the pending message before processing")

	_, err := executor.ProcessBlock(1)
	require.NoError(t, err)

	for k, v := range executor.inbox {
		require.Emptyf(t, v, "inbox[%s] must be pruned after processing", k)
	}
	for k, v := range executor.outbox {
		require.Emptyf(t, v, "outbox[%s] must be pruned after processing", k)
	}
}

// TestTopUpUnfreezesContract ensures a contract frozen by storage-rent debt can
// recover: topping up enough to clear the debt reactivates it.
func TestTopUpUnfreezesContract(t *testing.T) {
	executor := newTestExecutor(t)
	deployer := testAddr(1)
	contract := deployTestContract(t, executor, deployer, []byte("unfreeze"))

	// Force the contract into a frozen state with outstanding debt.
	acct, ok := executor.contracts[string(contract)]
	require.True(t, ok)
	acct.Status = ContractStatusFrozen
	acct.StorageRentDebtNaet = sdkmath.NewInt(500)
	acct.BalanceNaet = sdkmath.ZeroInt()
	executor.contracts[string(contract)] = acct

	result, events, err := executor.TopUpContract(contract, sdkmath.NewInt(500))
	require.NoError(t, err)
	require.Equal(t, ContractStatusActive, result.NormalizedStatus())
	require.True(t, result.StorageRentDebtNaet.IsZero())
	require.NotEmpty(t, events, "an unfreeze event is emitted")
	require.Equal(t, EventContractUnfrozen, events[0].Type)
}
