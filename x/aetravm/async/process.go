package async

import (
	"encoding/hex"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
)

// storageRentOwed computes rent accrued on a contract's current state since
// it was last charged: the longer a contract sits idle, the more it owes the
// next time it is touched. Rent is proportional to state size and elapsed
// blocks, not to a flat per-message fee.
func storageRentOwed(contract ContractAccount, height uint64, feePerByte sdkmath.Int) (sdkmath.Int, uint64) {
	if len(contract.State) == 0 || feePerByte.IsNil() || !feePerByte.IsPositive() {
		return sdkmath.ZeroInt(), 0
	}
	if height <= contract.LastStorageChargeHeight {
		return sdkmath.ZeroInt(), 0
	}
	elapsedBlocks := height - contract.LastStorageChargeHeight
	// Use unsigned-safe conversions: int64(elapsedBlocks) would wrap negative
	// for elapsedBlocks > 2^63, which would make rent negative and mint balance.
	owed := feePerByte.Mul(sdkmath.NewIntFromUint64(uint64(len(contract.State)))).Mul(sdkmath.NewIntFromUint64(elapsedBlocks))
	return owed, elapsedBlocks
}

func (e *Executor) processNext() (ExecutionReceipt, error) {
	queued := e.queue[0]
	e.queue = e.queue[1:]
	e.removeFromMailboxes(queued)
	msg := queued.Envelope
	msg.ExecutionBlockHeight = e.blockHeight
	receipt := newExecutionReceipt(queued, e.blockHeight, EmptyAVMStateRoot())
	receipt.GasUsed = e.params.ExecutionGasPerMessage
	e.metrics.ProcessedMessages++
	e.metrics.GasUsed += e.params.ExecutionGasPerMessage

	if msg.DeadlineBlock != 0 && e.blockHeight > msg.DeadlineBlock {
		receipt.ResultCode = ResultExpired
		receipt.Error = "message expired"
		receipt.FailedPhase = FailedPhaseValidation
		e.metrics.FailedExecutions++
		e.handleFailure(msg, &receipt)
		e.appendReceipt(&receipt)
		return receipt, nil
	}

	contract, ok := e.contracts[string(msg.Destination)]
	if !ok {
		if msg.StateInit != nil {
			user, _, err := contracttypes.DeriveContractAddressFromStateInit(contracttypes.DefaultContractChainID, contracttypes.DefaultContractNamespace, addressing.FormatAccAddress(msg.Source), *msg.StateInit, contracttypes.DefaultParams())
			if err != nil {
				return receipt, err
			}
			if user != addressing.FormatAccAddress(msg.Destination) {
				receipt.ResultCode = ResultNoDestination
				receipt.Error = "destination contract not found"
				receipt.FailedPhase = FailedPhaseDispatch
				e.metrics.FailedExecutions++
				e.handleFailure(msg, &receipt)
				e.appendReceipt(&receipt)
				return receipt, nil
			}
			contract = ContractAccount{
				Address: append(sdk.AccAddress(nil), msg.Destination...),
				CodeHash: func() []byte {
					sum, err := contracttypes.HashStateInit(*msg.StateInit)
					if err != nil {
						return nil
					}
					decoded, err := hex.DecodeString(sum)
					if err != nil {
						return nil
					}
					return decoded
				}(),
				State:                 append([]byte(nil), msg.StateInit.InitData...),
				BalanceNaet:           msg.Value.Amount,
				Status:                ContractStatusActive,
				StorageRentDebtNaet:   sdkmath.ZeroInt(),
				LastStorageChargeHeight: e.blockHeight,
			}
			e.contracts[string(msg.Destination)] = contract
			ok = true
		}
		if !ok {
			receipt.ResultCode = ResultNoDestination
			receipt.Error = "destination contract not found"
			receipt.FailedPhase = FailedPhaseDispatch
			e.metrics.FailedExecutions++
			e.handleFailure(msg, &receipt)
			e.appendReceipt(&receipt)
			return receipt, nil
		}
	}
	receipt.StateRootBefore = ContractStateRoot(contract)
	receipt.StateRootAfter = receipt.StateRootBefore
	if contract.NormalizedStatus() == ContractStatusFrozen {
		receipt.ResultCode = ResultExecutionFailed
		receipt.Error = "destination contract frozen by storage rent"
		receipt.FailedPhase = FailedPhaseValidation
		e.metrics.FailedExecutions++
		e.handleFailure(msg, &receipt)
		e.appendReceipt(&receipt)
		return receipt, nil
	}

	handler := e.handlers[string(msg.Destination)]
	if handler == nil {
		receipt.ResultCode = ResultExecutionFailed
		receipt.Error = "destination contract has no handler"
		receipt.FailedPhase = FailedPhaseDispatch
		e.metrics.FailedExecutions++
		e.handleFailure(msg, &receipt)
		e.appendReceipt(&receipt)
		return receipt, nil
	}

	working := cloneContract(contract)
	working.BalanceNaet = working.BalanceNaet.Add(msg.Value.Amount)
	working.LogicalTime++
	result := handler(working, cloneMessage(msg))
	if result.GasUsed > 0 {
		receipt.GasUsed = result.GasUsed
	}
	if !e.acceptExecutionResult(&receipt, msg, result) {
		return receipt, nil
	}

	working.State = append([]byte(nil), result.NewState...)
	rentOwed, elapsedBlocks := storageRentOwed(contract, e.blockHeight, e.params.StorageFeePerByte)
	receipt.StorageFeeNaet = rentOwed
	if working.BalanceNaet.LT(receipt.StorageFeeNaet) {
		// The contract cannot cover its rent: seize everything it has and
		// record the shortfall as debt. Only the seized amount was actually
		// collected, so report that as the storage fee (not the full owed) —
		// otherwise EconomicActivity over-counts protocol revenue.
		seized := working.BalanceNaet
		unpaid := receipt.StorageFeeNaet.Sub(seized)
		receipt.StorageFeeNaet = seized
		frozen := cloneContract(contract)
		frozen.BalanceNaet = sdkmath.ZeroInt()
		if frozen.StorageRentDebtNaet.IsNil() {
			frozen.StorageRentDebtNaet = sdkmath.ZeroInt()
		}
		frozen.StorageRentDebtNaet = frozen.StorageRentDebtNaet.Add(unpaid)
		frozen.Status = ContractStatusFrozen
		frozen.LastStorageChargeHeight = e.blockHeight
		e.contracts[string(frozen.Address)] = frozen
		receipt.ResultCode = ResultExecutionFailed
		receipt.Error = "insufficient naet for storage fee; contract frozen by storage rent"
		receipt.FailedPhase = FailedPhaseStorage
		receipt.StateRootAfter = ContractStateRoot(frozen)
		receipt.StateCommitted = true
		receipt.Events = append(receipt.Events, contractFrozenEvent(frozen, e.blockHeight))
		e.metrics.FailedExecutions++
		e.handleFailure(msg, &receipt)
		e.appendReceipt(&receipt)
		return receipt, nil
	}
	working.BalanceNaet = working.BalanceNaet.Sub(receipt.StorageFeeNaet)
	working.Status = ContractStatusActive
	working.LastStorageChargeHeight = e.blockHeight
	if receipt.StorageFeeNaet.IsPositive() {
		receipt.Events = append(receipt.Events, rentPaidEvent(working, e.blockHeight, elapsedBlocks, receipt.StorageFeeNaet))
	}
	outgoing := make([]MessageEnvelope, len(result.Outgoing))
	outgoingTxIndex := e.nextTxIndex
	totalOutValue := sdkmath.ZeroInt()
	for i, out := range result.Outgoing {
		out.Source = append(sdk.AccAddress(nil), working.Address...)
		out.CreatedLogicalTime = working.LogicalTime
		out.ExecutionBlockHeight = 0
		out.Depth = msg.Depth + 1
		if err := out.Validate(e.params); err != nil {
			receipt.ResultCode = ResultLimitExceeded
			receipt.Error = err.Error()
			receipt.FailedPhase = FailedPhaseQueue
			e.metrics.FailedExecutions++
			e.handleFailure(msg, &receipt)
			e.appendReceipt(&receipt)
			return receipt, nil
		}
		if !out.Value.Amount.IsNil() {
			totalOutValue = totalOutValue.Add(out.Value.Amount)
		}
		outgoing[i] = out
	}
	// Value conservation: a contract can only send value it actually holds.
	// Debit the total outgoing value from the sender here; it is credited to
	// each destination on delivery. Without this, every hop that carries value
	// would mint naet from nothing.
	if working.BalanceNaet.LT(totalOutValue) {
		receipt.ResultCode = ResultInsufficientBalance
		receipt.Error = "insufficient contract balance to cover outgoing message value"
		receipt.FailedPhase = FailedPhaseExecution
		e.metrics.FailedExecutions++
		e.handleFailure(msg, &receipt)
		e.appendReceipt(&receipt)
		return receipt, nil
	}
	working.BalanceNaet = working.BalanceNaet.Sub(totalOutValue)
	if err := e.validateQueueCapacity(outgoing); err != nil {
		receipt.ResultCode = ResultLimitExceeded
		receipt.Error = err.Error()
		receipt.FailedPhase = FailedPhaseQueue
		e.metrics.FailedExecutions++
		e.handleFailure(msg, &receipt)
		e.appendReceipt(&receipt)
		return receipt, nil
	}
	e.contracts[string(working.Address)] = working
	receipt.StateRootAfter = ContractStateRoot(working)
	receipt.LogicalTime = working.LogicalTime
	receipt.StateCommitted = true
	if len(outgoing) > 0 {
		e.nextTxIndex++
	}
	for i, out := range outgoing {
		queuedOut, err := e.enqueueMessageWithOrder(out, e.blockHeight, outgoingTxIndex, uint32(i))
		if err != nil {
			receipt.ResultCode = ResultLimitExceeded
			receipt.Error = err.Error()
			receipt.FailedPhase = FailedPhaseQueue
			e.metrics.FailedExecutions++
			e.handleFailure(msg, &receipt)
			e.appendReceipt(&receipt)
			return receipt, nil
		}
		receipt.EmittedMessageIDs = append(receipt.EmittedMessageIDs, append([]byte(nil), queuedOut.MessageID...))
		receipt.ValueOutNaet = receipt.ValueOutNaet.Add(out.Value.Amount)
		receipt.Events = append(receipt.Events, messageQueuedEvent(queuedOut))
	}
	e.appendReceipt(&receipt)
	return receipt, nil
}

func (e *Executor) acceptExecutionResult(receipt *ExecutionReceipt, msg MessageEnvelope, result ExecutionResult) bool {
	if receipt.GasUsed > msg.GasLimit {
		receipt.ResultCode = ResultLimitExceeded
		receipt.Error = "message gas limit exceeded"
		receipt.FailedPhase = FailedPhaseExecution
		e.metrics.FailedExecutions++
		e.handleFailure(msg, receipt)
		e.appendReceipt(receipt)
		return false
	}
	receipt.ResultCode = result.ResultCode
	if result.ResultCode != ResultOK {
		if result.Error != "" {
			receipt.Error = result.Error
		} else {
			receipt.Error = "contract execution failed"
		}
		receipt.FailedPhase = FailedPhaseExecution
		e.metrics.FailedExecutions++
		e.handleFailure(msg, receipt)
		e.appendReceipt(receipt)
		return false
	}
	if len(result.NewState) > int(e.params.MaxStateSize) {
		receipt.ResultCode = ResultLimitExceeded
		receipt.Error = "contract state limit exceeded"
		receipt.FailedPhase = FailedPhaseStorage
		e.metrics.FailedExecutions++
		e.handleFailure(msg, receipt)
		e.appendReceipt(receipt)
		return false
	}
	if len(result.Outgoing) > int(e.params.MaxEmittedMessagesPerExec) {
		receipt.ResultCode = ResultLimitExceeded
		receipt.Error = "emitted message limit exceeded"
		receipt.FailedPhase = FailedPhaseQueue
		e.metrics.FailedExecutions++
		e.handleFailure(msg, receipt)
		e.appendReceipt(receipt)
		return false
	}
	if result.StorageWrites > e.params.MaxStorageWritesPerExec {
		receipt.ResultCode = ResultLimitExceeded
		receipt.Error = "storage write limit exceeded"
		receipt.FailedPhase = FailedPhaseStorage
		e.metrics.FailedExecutions++
		e.handleFailure(msg, receipt)
		e.appendReceipt(receipt)
		return false
	}
	return true
}

func (e *Executor) appendReceipt(receipt *ExecutionReceipt) {
	finalizeReceipt(receipt)
	e.receipts = append(e.receipts, cloneReceipt(*receipt))
}

func (e *Executor) handleFailure(msg MessageEnvelope, receipt *ExecutionReceipt) {
	if e.scheduleRetry(msg, receipt) {
		return
	}
	if msg.MaxRetries > 0 || msg.RetryCount > 0 {
		e.recordDeadLetter(msg, *receipt)
	}
	e.finalizeFailure(msg, receipt)
}

func (e *Executor) scheduleRetry(msg MessageEnvelope, receipt *ExecutionReceipt) bool {
	if !isRetryableFailure(receipt.ResultCode) {
		return false
	}
	if msg.Bounced || msg.Opcode == RefundOpcode {
		return false
	}
	if msg.MaxRetries == 0 || msg.RetryCount >= msg.MaxRetries {
		return false
	}
	delay := msg.RetryDelayBlocks
	if delay == 0 {
		delay = e.params.DefaultRetryDelayBlocks
	}
	if delay == 0 || delay > e.params.MaxRetryDelayBlocks {
		return false
	}
	deliverAt, overflow := safeAddBlock(e.blockHeight, delay)
	if overflow {
		return false
	}
	if msg.DeadlineBlock != 0 && deliverAt > msg.DeadlineBlock {
		return false
	}
	retry := cloneMessage(msg)
	retry.ExecutionBlockHeight = 0
	retry.DeliverAtBlock = deliverAt
	retry.RetryCount++
	queuedRetry, err := e.enqueueSingleMessage(retry)
	if err != nil {
		return false
	}
	receipt.EmittedMessageIDs = append(receipt.EmittedMessageIDs, append([]byte(nil), queuedRetry.MessageID...))
	receipt.Events = append(receipt.Events, messageQueuedEvent(queuedRetry))
	receipt.RetryScheduled = true
	e.metrics.RetriedMessages++
	return true
}

func (e *Executor) recordDeadLetter(msg MessageEnvelope, receipt ExecutionReceipt) {
	dead := DeadLetter{
		Sequence:	e.nextDeadLetterSequence,
		FailedSequence:	receipt.Sequence,
		RecordedBlock:	e.blockHeight,
		Envelope:	cloneMessage(msg),
		Receipt:	cloneReceipt(receipt),
		Reason:		receipt.Error,
	}
	dead.Envelope.ExecutionBlockHeight = 0
	if uint32(len(e.deadLetters)) >= e.params.MaxDeadLetters {
		e.deadLetters = e.deadLetters[1:]
	}
	e.deadLetters = append(e.deadLetters, dead)
	e.nextDeadLetterSequence++
	e.metrics.DeadLetterMessages++
}

func isRetryableFailure(resultCode uint32) bool {
	switch resultCode {
	case ResultNoDestination, ResultExecutionFailed:
		return true
	default:
		return false
	}
}

func safeAddBlock(height, delay uint64) (uint64, bool) {
	if delay > ^uint64(0)-height {
		return 0, true
	}
	return height + delay, false
}

func (e *Executor) finalizeFailure(msg MessageEnvelope, receipt *ExecutionReceipt) {
	if msg.Bounced || msg.Opcode == RefundOpcode || msg.RefundOfSequence != 0 {
		receipt.ResultCode = resultCodeWithSuppressedRefund(receipt.ResultCode, msg)
		receipt.RefundReason = "refund suppressed for bounced/refund message"
		return
	}
	refund, err := CalculateRefund(msg, *receipt)
	if err != nil {
		receipt.RefundReason = err.Error()
		return
	}
	if msg.Bounce {
		bounce, err := BuildBounceMessage(msg, refund, e.params.ForwardingFee)
		if err != nil {
			receipt.RefundReason = err.Error()
			return
		}
		sequence := e.nextSequence
		bounce.RefundOfSequence = receipt.Sequence
		queuedBounce, err := e.enqueueSingleMessage(bounce)
		if err != nil {
			receipt.RefundReason = err.Error()
			return
		}
		receipt.EmittedMessageIDs = append(receipt.EmittedMessageIDs, append([]byte(nil), queuedBounce.MessageID...))
		receipt.ValueOutNaet = receipt.ValueOutNaet.Add(bounce.Value.Amount)
		receipt.BounceCreated = true
		e.metrics.BouncedMessages++
		if err := MarkRefunded(receipt, refund, "bounce", sequence); err != nil {
			receipt.RefundReason = err.Error()
			return
		}
		receipt.Events = append(receipt.Events, messageQueuedEvent(queuedBounce), messageBouncedEvent(*receipt, queuedBounce))
		return
	}
	if !msg.Value.Amount.IsPositive() {
		return
	}
	refundMsg, err := BuildRefundMessage(msg, refund, e.params.ForwardingFee)
	if err != nil {
		receipt.RefundReason = err.Error()
		return
	}
	var sequence uint64
	refundMsg.RefundOfSequence = receipt.Sequence
	if refund.Amount.IsPositive() {
		sequence = e.nextSequence
		queuedRefund, err := e.enqueueSingleMessage(refundMsg)
		if err != nil {
			receipt.RefundReason = err.Error()
			return
		}
		receipt.EmittedMessageIDs = append(receipt.EmittedMessageIDs, append([]byte(nil), queuedRefund.MessageID...))
		receipt.ValueOutNaet = receipt.ValueOutNaet.Add(refundMsg.Value.Amount)
		receipt.Events = append(receipt.Events, messageQueuedEvent(queuedRefund))
		receipt.RefundCreated = true
		e.metrics.RefundMessages++
	}
	if err := MarkRefunded(receipt, refund, "refund", sequence); err != nil {
		receipt.RefundReason = err.Error()
		return
	}
}

func resultCodeWithSuppressedRefund(resultCode uint32, msg MessageEnvelope) uint32 {
	if msg.Bounced {
		return ResultBounceSuppressed
	}
	if msg.Opcode == RefundOpcode || msg.RefundOfSequence != 0 {
		return ResultRefundSuppressed
	}
	return resultCode
}
