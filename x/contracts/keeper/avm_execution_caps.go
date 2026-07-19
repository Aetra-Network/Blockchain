package keeper

import (
	"bytes"
	"fmt"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// enforceAVMExecutionCaps checks the two per-execution hard caps that can be
// computed purely from an avm.Execution result plus the pre-execution
// storage snapshot, WITHOUT touching x/aetravm/avm/avm.go's interpreter loop
// (see the Phase H doc comment on the constants in x/contracts/types/api.go
// for why these live here instead). Call this immediately after a successful
// (ResultCode == async.ResultOK) avm.Runner.Run, before the returned
// exec.State/exec.Outgoing are used for anything else -- an error here must
// abort the whole delivery/execution exactly like the existing
// MaxContractStorageBytes / MaxInternalMessageQueueDepth checks it mirrors.
func enforceAVMExecutionCaps(preStorage avm.Storage, exec avm.Execution) error {
	if len(exec.Outgoing) > types.MaxEventsPerExecution {
		return fmt.Errorf("%s: execution emitted %d outgoing messages, exceeds per-execution limit of %d",
			types.ErrExecutionFailed, len(exec.Outgoing), types.MaxEventsPerExecution)
	}
	changed := changedStorageKeyCount(preStorage, exec.State)
	if changed > types.MaxChangedStorageKeysPerExecution {
		return fmt.Errorf("%s: execution changed %d distinct storage keys, exceeds per-execution limit of %d",
			types.ErrExecutionFailed, changed, types.MaxChangedStorageKeysPerExecution)
	}
	return nil
}

// changedStorageKeyCount returns the number of distinct keys that differ
// between before and after: added, value-changed, or deleted. This is a
// partial approximation of "touched storage keys" -- see
// MaxChangedStorageKeysPerExecution's doc comment for what it does and does
// not catch.
func changedStorageKeyCount(before, after avm.Storage) int {
	count := 0
	presentAfter := make(map[string]struct{}, len(after))
	for key, value := range after {
		presentAfter[key] = struct{}{}
		prior, existed := before[key]
		if !existed || !bytes.Equal(prior, value) {
			count++
		}
	}
	for key := range before {
		if _, stillPresent := presentAfter[key]; stillPresent {
			continue
		}
		// Present before, absent after: deleted.
		count++
	}
	return count
}

// requireStateGrowthWithinCap bounds the NET bytes a single execution may
// add to a contract's storage. See MaxStateGrowthBytesPerExecution's doc
// comment (x/contracts/types/api.go) for why this is distinct from the
// existing absolute MaxContractStorageBytes ceiling.
func requireStateGrowthWithinCap(beforeBytes, afterBytes uint64) error {
	if afterBytes <= beforeBytes {
		return nil
	}
	growth := afterBytes - beforeBytes
	if growth > types.MaxStateGrowthBytesPerExecution {
		return fmt.Errorf("%s: execution grew contract storage by %d bytes, exceeds per-execution limit of %d",
			types.ErrExecutionFailed, growth, types.MaxStateGrowthBytesPerExecution)
	}
	return nil
}
