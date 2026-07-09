package async

import (
	"errors"
	"fmt"

	sdkmath "cosmossdk.io/math"
)

func DefaultParams() Params {
	return Params{
		// A single transaction may carry up to 256 messages so a batch action
		// (e.g. mint 255 NFTs behind one signature = 1 + 255) fits. Fan-out
		// stays bounded at every layer regardless of this cap: each emit costs
		// OpEmitInternal gas (100) plus ExecutionGasPerMessage (10_000) to
		// process, the runtime gas ceiling (MaxRuntimeGasLimit) hard-limits
		// total work, MaxMessagesPerBlock throttles the chain, and the queue is
		// bounded by MaxInternalMessageQueueDepth / MaxQueuedMessagesPerContract.
		MaxMessagesPerTx:		256,
		MaxMessagesPerBlock:		4096,
		MaxQueuedMessagesPerContract:	1024,
		MaxProcessingAttempts:		4,
		MaxRecursionDepth:		8,
		MaxBodySize:			4096,
		MaxStateSize:			64 * 1024,
		// A batch mint of one item-contract per NFT can deploy up to 256 in a
		// single tx (1 signature + 255 items). Each deploy still costs
		// ContractDeploymentCost plus ongoing storage rent, so state growth is
		// economically bounded; the absolute ceiling caps misconfiguration.
		MaxContractDeploysPerTx:	256,
		MaxContractDeploysPerBlock:	4096,
		// One execution may emit up to 256 messages so a batch handler can fan
		// a single call out to the whole batch. Gas per emit keeps it bounded.
		MaxEmittedMessagesPerExec:	256,
		MaxStorageWritesPerExec:	64,
		MaxActionsPerExecution:		512,
		MaxRetriesPerMessage:		3,
		DefaultRetryDelayBlocks:	1,
		MaxRetryDelayBlocks:		64,
		MaxDeadLetters:			1024,
		ExecutionGasPerMessage:		10_000,
		StorageFeePerByte:		sdkmath.NewInt(1),
		ForwardingFee:			sdkmath.NewInt(1),
		ContractDeploymentCost:		sdkmath.NewInt(1_000),
	}
}

// AbsoluteMaxMessagesPerTx / PerBlock bound how high governance can raise the
// message caps. They are defense-in-depth on top of gas metering: even though
// every message already costs gas, a hard ceiling keeps a misconfiguration
// from turning one transaction or block into an oversized fan-out.
const (
	AbsoluteMaxMessagesPerTx        = 1024
	AbsoluteMaxMessagesPerBlock     = 65536
	AbsoluteMaxEmittedPerExecution  = 1024
	AbsoluteMaxContractDeploysPerTx = 1024
)

func (p Params) Validate() error {
	if p.MaxMessagesPerTx == 0 {
		return errors.New("max messages per tx must be positive")
	}
	if p.MaxMessagesPerTx > AbsoluteMaxMessagesPerTx {
		return fmt.Errorf("max messages per tx %d exceeds absolute ceiling %d", p.MaxMessagesPerTx, AbsoluteMaxMessagesPerTx)
	}
	if p.MaxMessagesPerBlock == 0 {
		return errors.New("max messages per block must be positive")
	}
	if p.MaxMessagesPerBlock > AbsoluteMaxMessagesPerBlock {
		return fmt.Errorf("max messages per block %d exceeds absolute ceiling %d", p.MaxMessagesPerBlock, AbsoluteMaxMessagesPerBlock)
	}
	if p.MaxQueuedMessagesPerContract == 0 {
		return errors.New("max queued messages per contract must be positive")
	}
	if p.MaxProcessingAttempts == 0 {
		return errors.New("max processing attempts must be positive")
	}
	if p.MaxRecursionDepth == 0 {
		return errors.New("max recursion depth must be positive")
	}
	if p.MaxBodySize == 0 {
		return errors.New("max body size must be positive")
	}
	if p.MaxStateSize == 0 {
		return errors.New("max state size must be positive")
	}
	if p.MaxContractDeploysPerTx == 0 {
		return errors.New("max contract deploys per tx must be positive")
	}
	if p.MaxContractDeploysPerTx > AbsoluteMaxContractDeploysPerTx {
		return fmt.Errorf("max contract deploys per tx %d exceeds absolute ceiling %d", p.MaxContractDeploysPerTx, AbsoluteMaxContractDeploysPerTx)
	}
	if p.MaxContractDeploysPerBlock == 0 {
		return errors.New("max contract deploys per block must be positive")
	}
	if p.MaxEmittedMessagesPerExec == 0 {
		return errors.New("max emitted messages per execution must be positive")
	}
	if p.MaxEmittedMessagesPerExec > AbsoluteMaxEmittedPerExecution {
		return fmt.Errorf("max emitted messages per execution %d exceeds absolute ceiling %d", p.MaxEmittedMessagesPerExec, AbsoluteMaxEmittedPerExecution)
	}
	if p.MaxStorageWritesPerExec == 0 {
		return errors.New("max storage writes per execution must be positive")
	}
	if p.MaxActionsPerExecution == 0 {
		return errors.New("max actions per execution must be positive")
	}
	if p.MaxRetriesPerMessage == 0 {
		return errors.New("max retries per message must be positive")
	}
	if p.DefaultRetryDelayBlocks == 0 {
		return errors.New("default retry delay blocks must be positive")
	}
	if p.MaxRetryDelayBlocks == 0 {
		return errors.New("max retry delay blocks must be positive")
	}
	if p.DefaultRetryDelayBlocks > p.MaxRetryDelayBlocks {
		return errors.New("default retry delay blocks must not exceed max retry delay blocks")
	}
	if p.MaxDeadLetters == 0 {
		return errors.New("max dead letters must be positive")
	}
	if p.ExecutionGasPerMessage == 0 {
		return errors.New("execution gas per message must be positive")
	}
	for _, item := range []struct {
		name	string
		value	sdkmath.Int
	}{
		{name: "storage fee per byte", value: p.StorageFeePerByte},
		{name: "forwarding fee", value: p.ForwardingFee},
		{name: "contract deployment cost", value: p.ContractDeploymentCost},
	} {
		if item.value.IsNil() || item.value.IsNegative() {
			return fmt.Errorf("%s must be non-negative", item.name)
		}
	}
	return nil
}
