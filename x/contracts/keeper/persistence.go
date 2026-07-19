package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	corestore "cosmossdk.io/core/store"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

// This file is the module's storage layout. It exists to keep a contract
// transaction's gas cost independent of how much unrelated state the module
// already holds.
//
// # The problem it solves
//
// Every mutation used to serialize the ENTIRE module state -- every code
// record with its full bytecode, every contract, every receipt -- into a single
// KV value at genesisKey, and read it all back at the top of every handler.
// Measured against a real IAVL store with production KVGasConfig, the cost of
// ONE StoreCode was exactly:
//
//	gas = 3,033 + 3*blobBefore + 30*blobAfter    (~33 gas per byte of TOTAL state)
//
//	  N=0 unrelated codes:    28,710 gas
//	  N=100 unrelated codes: 797,601 gas
//
// That is 100% of the module's SDK gas: a stage breakdown attributed 90.9% to
// the blob write and 9.1% to the blob read, and 0.0% to AVM execution -- the
// AVM meters itself internally (x/aetravm/avm/gas.go) and never charges the SDK
// meter, so contract gas WAS blob I/O and nothing else.
//
// Two consequences, both fatal:
//
//   - Contract count: with one shared code, deploy crossed MaxTxGas =
//     1,000,000 at the 12th instance. Above the cap the ante rejects, below it
//     the tx dies in FinalizeBlock -- unexecutable at any legal gas limit.
//   - Lifetime operation count, which is worse: every operation appends a
//     ContractReceipt to the same blob, so a SINGLE contract alone in state
//     bricked itself on its ~90th execution (30,112 blob bytes = 1,005,039
//     gas). MaxRetainedReceipts = 8192 (types/api.go) is real but sits ~91x
//     beyond the gas wall, so the bound never engaged before the module stopped
//     working -- and it took every other contract on the chain with it, since
//     they all shared the one key.
//
// # The layout
//
// The three collections that grow with usage -- State.Codes (56.0% of blob
// bytes), State.Contracts (32.9%) and State.Receipts (10.9%), 99.8% together --
// are stored as one KV record each under types.CodeKey / types.ContractKey /
// types.ReceiptKey. The residual GenesisState (Params, StateRoot, and the five
// collections measured at ~0%: InternalMessages, AssetOwnership,
// StakingCapabilities, NativeStakingInjects, SecurityAttestations) still lives
// at genesisKey as one small, fixed-size value.
//
// # Why the state root is still byte-identical
//
// ComputeContractsStateRoot hashes the whole normalized State as one JSON
// string, so it needs the entire state in memory by construction. That is NOT a
// reason to change the formula, because computing it costs CPU and ZERO GAS --
// gas is charged only for store Get/Set. Reads therefore still reassemble the
// full State (via three prefix scans) and the root is computed from exactly the
// same bytes as before, so it is unchanged for the same logical state and no
// node forks on it. Normalize, Validate and the GLOBAL pruneReceipts also keep
// running over the whole state, unweakened: pruneReceipts is a module-wide
// truncation whose result depends on what every contract did, and keeping the
// full state in RAM at write time is what lets it stay global. A per-contract
// receipt ring would have silently changed which receipts exist.
//
// What actually changes is the WRITE set: only records whose bytes differ get
// Set, and records that disappeared get Deleted. The 90.9% write term collapses
// from O(total module state) to O(records this message touched).
//
// # What this does NOT fix, and what would
//
// The read term stays O(module state) at 3 gas/byte -- 10x cheaper per byte than
// the writes it replaces, but still a slope. Measured: storing a code costs
// ~930 gas per unrelated code that already exists (was ~7,689), and executing a
// contract costs ~1,053 gas per receipt it has already emitted (was ~9,135).
// So the self-bricking contract now crosses MaxTxGas at ~execution #866 instead
// of #90. The wall moved ~10x; it did not go away, and MaxRetainedReceipts =
// 8192 (types/api.go) STILL sits ~9.5x beyond it -- 8192 receipts would be ~8.6M
// gas -- so that bound still never engages before the module stops working.
//
// Two ways out, both bigger than this change and both consensus breaks:
//
//   - Lower MaxRetainedReceipts so the existing bound actually engages inside
//     the gas wall (~256-512 given the slope above). Cheap, but it changes which
//     receipts exist, and pruneReceipts is a GLOBAL truncation -- whether
//     contract A's receipt survives depends on what contract B did -- so it is a
//     semantics change that needs a deliberate decision, not a silent tweak.
//   - Load only the entities a message touches. That IS deterministic (the
//     touched set is a function of the message), but it is blocked by
//     ComputeContractsStateRoot needing the whole state to hash: the root would
//     have to be redefined as a fold over per-record hashes in key byte order,
//     computed lazily on export/query rather than on the write path. Nothing on
//     the write path consumes it today (RootContribution and ValidateInvariants
//     have no live callers, and GenesisState.Validate's root check is a
//     tautology on the load path -- RefreshStateRoot overwrites the field and
//     then Validate recomputes it), so this is tractable. It is the right next
//     step and it is what AEZ per-zone roots need anyway.
//
// # Why a write set can be computed from memory, and why that is deterministic
//
// writeDiff decides what changed against k.written -- an in-memory map of
// exactly what this keeper last read from, or wrote to, the store, keyed by
// store key and holding the exact committed bytes.
//
// That is safe because k.written is re-established from the committed store by
// loadForBlock at the top of EVERY consensus entry point (every Msg handler via
// grpc_server.go, plus EndBlocker) before anything mutates. So the write set is
// a pure function of (committed state, message): every node computes the same
// set and is charged the same gas, no matter how long it has been running or
// what it has cached. A failed tx that rolls the store back leaves k.written
// ahead of the store, and the next entry point's load resets it before any write
// can observe the difference.
//
// That determinism requirement is also why the READ side is NOT cached. It is
// tempting to skip the load when a version marker says the in-memory copy is
// current, which would make reads O(1) -- but then a freshly restarted node
// would charge full read gas for a tx while a warm node charged none, the two
// would disagree on gasUsed, and the chain would halt on an apphash mismatch.
// Reads must be unconditional. That is the same F-17 bug class documented at
// keeper.go loadForBlock, and it is why loadForBlock still reads every block.
//
// Map iteration order never reaches the store: every Set/Delete below is issued
// over a sorted key slice, so the call sequence is byte-ordered on every node.
// (The resulting KV state and total gas are order-independent anyway, but the
// determinism gate forbids relying on that.)

// hotCollections are the collections stored one KV record per entity instead of
// inside the residual blob. Splitting them out is what makes a write
// O(records changed) rather than O(records that exist).
type hotCollections struct {
	codes     []types.CodeRecord
	contracts []types.Contract
	receipts  []types.ContractReceipt
}

// hotRecords maps a store key to the exact bytes believed committed under it.
// Holding raw bytes rather than structs means the diff can never be fooled by a
// slice aliased into the in-memory genesis and mutated in place.
type hotRecords map[string][]byte

// storeBaseline is what the committed store holds, as read: the per-record
// bytes and the residual blob's bytes. It is what the next write diffs against.
type storeBaseline struct {
	records  hotRecords
	residual []byte
}

// splitHotCollections removes the per-record collections from gs and returns
// them alongside the residual GenesisState that stays at genesisKey.
func splitHotCollections(gs types.GenesisState) (types.GenesisState, hotCollections) {
	hot := hotCollections{
		codes:     gs.State.Codes,
		contracts: gs.State.Contracts,
		receipts:  gs.State.Receipts,
	}
	gs.State.Codes = nil
	gs.State.Contracts = nil
	gs.State.Receipts = nil
	return gs, hot
}

// marshalHotRecords renders the per-record write set: store key -> record bytes.
// Marshaling costs no gas; only the Set calls that survive the diff do.
func marshalHotRecords(hot hotCollections) (hotRecords, error) {
	out := make(hotRecords, len(hot.codes)+len(hot.contracts)+len(hot.receipts))
	for _, code := range hot.codes {
		bz, err := json.Marshal(code)
		if err != nil {
			return nil, err
		}
		out[string(types.CodeKey(code.CodeID))] = bz
	}
	for _, contract := range hot.contracts {
		bz, err := json.Marshal(contract)
		if err != nil {
			return nil, err
		}
		out[string(types.ContractKey(contract.AddressUser))] = bz
	}
	for _, receipt := range hot.receipts {
		bz, err := json.Marshal(receipt)
		if err != nil {
			return nil, err
		}
		out[string(types.ReceiptKey(receipt.ReceiptID))] = bz
	}
	return out, nil
}

// prefixEnd returns the exclusive upper bound of a prefix scan.
func prefixEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xFF {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

// scanRecords reads every record under prefix, appending the decoded values and
// recording the raw bytes into seen. Prefix iteration is charged
// IterNextCostFlat per step plus the per-byte read cost, NOT ReadCostFlat per
// record, so reading N entities back this way costs about the same per byte as
// reading one big blob did.
func scanRecords[T any](store corestore.KVStore, prefix []byte, seen hotRecords) ([]T, error) {
	iter, err := store.Iterator(prefix, prefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []T
	for ; iter.Valid(); iter.Next() {
		var value T
		raw := iter.Value()
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("read %x record %q: %w", prefix, iter.Key(), err)
		}
		out = append(out, value)
		seen[string(iter.Key())] = append([]byte(nil), raw...)
	}
	// Deliberately not checking iter.Error(): cachekv's cacheMergeIterator
	// defines Error() as "not Valid()", so it reports a spurious "invalid
	// cacheMergeIterator" for every scan that ran to completion. Valid() is the
	// only usable termination signal, which is why the SDK's own modules
	// iterate this way too.
	return out, nil
}

// readGenesisState reassembles the full GenesisState from the committed store:
// the residual struct from genesisKey, plus the per-record code, contract and
// receipt records via three prefix scans. found reports whether the module has
// any committed state at all; records is exactly what the per-record keys hold,
// which is what the next write diffs against.
//
// The returned GenesisState is raw: NOT normalized, NOT pruned, NOT validated.
// Callers apply RefreshStateRoot/Validate, and must keep records as the write
// baseline rather than the post-refresh state -- pruneReceipts drops receipts
// from memory that are still committed, and the next writeDiff is what deletes
// their records.
func (k Keeper) readGenesisState(ctx context.Context) (types.GenesisState, storeBaseline, bool, error) {
	baseline := storeBaseline{records: hotRecords{}}
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(genesisKey)
	if err != nil {
		return types.GenesisState{}, storeBaseline{}, false, err
	}
	if len(bz) == 0 {
		return types.GenesisState{}, baseline, false, nil
	}
	baseline.residual = bz
	var gs types.GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return types.GenesisState{}, storeBaseline{}, false, err
	}

	codes, err := scanRecords[types.CodeRecord](store, types.CodeKeyPrefix, baseline.records)
	if err != nil {
		return types.GenesisState{}, storeBaseline{}, false, err
	}
	contracts, err := scanRecords[types.Contract](store, types.ContractKeyPrefix, baseline.records)
	if err != nil {
		return types.GenesisState{}, storeBaseline{}, false, err
	}
	receipts, err := scanRecords[types.ContractReceipt](store, types.ReceiptKeyPrefix, baseline.records)
	if err != nil {
		return types.GenesisState{}, storeBaseline{}, false, err
	}

	// A store written before this layout existed still holds Codes/Contracts/
	// Receipts INSIDE the residual blob and has no per-record keys at all.
	// Prefer the blob's copy in that case so the two can never be double
	// counted. records stays empty for those collections, which is the truth --
	// the per-record store holds nothing -- so the first write after the upgrade
	// fans every record out and rewrites the residual without them. Getting this
	// backwards would silently drop every pre-upgrade code and contract.
	if len(gs.State.Codes) == 0 {
		gs.State.Codes = codes
	}
	if len(gs.State.Contracts) == 0 {
		gs.State.Contracts = contracts
	}
	if len(gs.State.Receipts) == 0 {
		gs.State.Receipts = receipts
	}
	return gs, baseline, true, nil
}

// writeReplacingState makes the store hold exactly gs, whatever it held before.
// InitGenesisState uses it, which means it must cope with a store that is not
// empty: importing a genesis over populated state has to REMOVE the codes,
// contracts and receipts that genesis does not mention. Reading the committed
// baseline rather than trusting k.written is what makes that safe -- k.written
// describes whatever this keeper last did, which for a fresh keeper over an
// existing store is nothing at all.
//
// Records that survived an import unmentioned would be resurrected by the next
// read, since these records are authoritative rather than a mirror of some other
// copy.
//
// design doc §8.3 (extended): its only caller, InitGenesisState, invokes this
// AFTER InitGenesis has already returned (and released txMu) -- never nested
// -- so acquiring txMu here is deadlock-free. Needed for the same reason
// writeGenesis (keeper.go) needs it: this method bare-reads/writes
// k.written/k.writtenResidual via readGenesisState/writeDiff, which must be
// serialized against every other txMu-locked critical section, not just
// against loadForBlock's own. k.genesis itself is read AFTER the lock is
// acquired (not by the caller beforehand) for the same reason: computing
// gs := types.RefreshStateRoot(k.genesis) outside the lock would reopen
// exactly the bare-read gap this fix closes elsewhere, even though
// InitGenesisState (a startup/genesis-import path, never a concurrent
// wire-level entrypoint) is not part of the demonstrated race.
func (k *Keeper) writeReplacingState(ctx context.Context) error {
	k.txMu.Lock()
	defer k.txMu.Unlock()
	if k.storeService == nil {
		return nil
	}
	gs := types.RefreshStateRoot(k.genesis)
	_, baseline, _, err := k.readGenesisState(ctx)
	if err != nil {
		return err
	}
	k.written = baseline.records
	k.writtenResidual = baseline.residual
	return k.writeDiff(ctx, gs)
}

// writeDiff persists next, touching only the records whose bytes differ from
// k.written, and deleting the records that no longer exist. Records that
// disappeared were previously left behind, so a pruned receipt would have stayed
// readable at its key forever.
func (k *Keeper) writeDiff(ctx context.Context, next types.GenesisState) error {
	if k.storeService == nil {
		return nil
	}
	residual, hot := splitHotCollections(next)
	desired, err := marshalHotRecords(hot)
	if err != nil {
		return err
	}
	residualBytes, err := json.Marshal(residual)
	if err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)

	// The residual carries StateRoot, so it changes on essentially every
	// mutation; the compare only skips the write for a handler that changed
	// nothing. It is O(1) in size either way.
	if !bytes.Equal(residualBytes, k.writtenResidual) {
		if err := store.Set(genesisKey, residualBytes); err != nil {
			return err
		}
		k.writtenResidual = residualBytes
	}

	for _, key := range sortedKeys(desired) {
		bz := desired[key]
		if prev, had := k.written[key]; had && bytes.Equal(prev, bz) {
			continue
		}
		if err := store.Set([]byte(key), bz); err != nil {
			return err
		}
	}
	for _, key := range sortedKeys(k.written) {
		if _, still := desired[key]; still {
			continue
		}
		if err := store.Delete([]byte(key)); err != nil {
			return err
		}
	}
	k.written = desired
	return nil
}

// sortedKeys orders a record map's keys so the Set/Delete call sequence is
// byte-ordered on every node instead of following Go's randomized map iteration.
func sortedKeys(records hotRecords) []string {
	out := make([]string, 0, len(records))
	for key := range records {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
