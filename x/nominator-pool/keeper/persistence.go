package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	corestore "cosmossdk.io/core/store"

	"github.com/sovereign-l1/l1/x/internal/prefixgenesis"
	"github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// This file is the module's storage layout. It exists to keep a deposit's gas
// cost independent of how much money the module already holds.
//
// # The problem it solves (FINDING-009 write amplification)
//
// Every mutation used to serialize the ENTIRE module state into a single KV
// value (prefixgenesis.Save of GenesisState.State), and then additionally
// re-serialize every pool and every share into their per-entity mirror keys --
// three times per deposit, because the deposit path saves three times. Gas is
// charged per byte written (WriteCostPerByte = 30, ten times the read cost),
// so gas was O(module state) and a measured deposit cost:
//
//	  1 depositor:   444,883 gas
//	 10 depositors: 1,321,556 gas   <-- already over MaxTxGas = 1,000,000
//	 50 depositors: 5,191,115 gas
//
// i.e. gas ~= 93,000 + 223 * (state bytes). Past ~7 depositors NO legal gas
// limit could execute a deposit: under the cap the tx died in FinalizeBlock,
// over it the ante rejected it. Deposits and unbonds both became impossible,
// which traps every depositor's principal by gas exhaustion alone. Real money
// (bank send + x/staking Delegate + distribution + auth) was only ~95k of that
// -- under 10% even on a fresh chain. The pool's own bookkeeping was the cost.
//
// # The layout
//
// The two collections that grow with users -- State.Pools and State.PoolShares
// -- are stored as one KV record per entity, under the SAME types.PoolKey /
// types.PoolShareKey keys that were previously written as write-only mirrors.
// They are no longer mirrors: they are the authoritative storage, so the bytes
// are stored once rather than twice, and the off-chain explorer indexer that
// reads those keys over raw ABCI keeps seeing exactly the JSON record it reads
// today (that KV contract is asserted by phase3_pool_accounting_kv_test.go and
// was the reason commit bddc9946 reverted an earlier attempt to delete these
// writes -- this change keeps every key and every value shape, and only stops
// rewriting records that did not change).
//
// Everything else in GenesisState still goes through prefixgenesis, which
// already skips a field whose bytes are unchanged. With Pools and PoolShares
// removed from it, the State field a deposit writes is small and usually
// identical to what is committed, so that write is skipped entirely.
//
// # Why a write set can be computed from memory, and why that is deterministic
//
// writeDiff writes only the records whose serialized bytes actually changed,
// deciding that against k.written -- an in-memory snapshot of exactly what this
// keeper last read from, or wrote to, the store.
//
// That is safe because k.written is re-established from the committed store by
// loadGenesisState at the top of EVERY consensus entry point (every Msg handler
// via msg_server.go, plus EndBlocker) before anything mutates. So the write set
// is a pure function of (committed state, message): every node computes the
// same set and is charged the same gas, no matter how long it has been running
// or what it has cached. A failed tx that rolls the store back leaves k.written
// ahead of the store, and the next entry point's load resets it before any
// write can observe the difference.
//
// That determinism requirement is also why the READ side is NOT cached. It is
// tempting to skip loadGenesisState when a version marker says the in-memory
// copy is current, which would make reads O(1) -- but then a freshly restarted
// node would charge full read gas for a tx while a warm node charged none, the
// two would disagree on gasUsed, and the chain would halt on an apphash
// mismatch. Reads must be unconditional. Making them cheap requires loading
// only the entities a message touches (which IS deterministic -- the touched
// set is a function of the message); that is a larger change and is not done
// here. See the note on remaining growth at readGenesisState.

// hotCollections are the collections stored one KV record per entity instead of
// inside the prefixgenesis State blob. Splitting them out is what makes a write
// O(entities changed) rather than O(entities that exist).
type hotCollections struct {
	pools  []types.NominatorPool
	shares []types.PoolShare
}

// splitHotCollections removes the per-entity collections from gs and returns
// them alongside the residual GenesisState that prefixgenesis persists.
func splitHotCollections(gs GenesisState) (GenesisState, hotCollections) {
	hot := hotCollections{pools: gs.State.Pools, shares: gs.State.PoolShares}
	gs.State.Pools = nil
	gs.State.PoolShares = nil
	return gs, hot
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

// scanJSON reads every record under prefix and appends the decoded values.
// Prefix iteration is charged IterNextCostFlat (30) per step plus the per-byte
// read cost, NOT ReadCostFlat (1000) per record, so reading N entities back
// this way costs about the same per byte as reading one big blob did.
func scanJSON[T any](store corestore.KVStore, prefix []byte) ([]T, error) {
	iter, err := store.Iterator(prefix, prefixEnd(prefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []T
	for ; iter.Valid(); iter.Next() {
		var value T
		if err := json.Unmarshal(iter.Value(), &value); err != nil {
			return nil, fmt.Errorf("read %s record %q: %w", prefix, iter.Key(), err)
		}
		out = append(out, value)
	}
	// Deliberately not checking iter.Error(): cachekv's cacheMergeIterator
	// defines Error() as "not Valid()", so it reports a spurious "invalid
	// cacheMergeIterator" for every scan that ran to completion. Valid() is
	// the only usable termination signal, which is why the SDK's own modules
	// iterate this way too.
	return out, nil
}

// readGenesisState reassembles the full GenesisState from the committed store:
// the residual struct via prefixgenesis, plus the per-entity pool and share
// records via two prefix scans.
//
// This is O(module state) in READ gas (3 gas/byte), which is the growth that
// remains after this change: roughly 1,000 gas per already-existing depositor
// on every nominator-pool message. That is bounded and ~10x cheaper per byte
// than the writes it replaces, but it is not O(1) -- see the file header for
// why caching it would be a consensus hazard and what the real fix is.
func (k Keeper) readGenesisState(ctx context.Context) (GenesisState, error) {
	gs, _, err := prefixgenesis.Load(ctx, k.storeService, genesisKey, DefaultGenesis())
	if err != nil {
		return GenesisState{}, err
	}
	store := k.storeService.OpenKVStore(ctx)
	pools, err := scanJSON[types.NominatorPool](store, []byte(types.PoolKeyPrefix))
	if err != nil {
		return GenesisState{}, err
	}
	shares, err := scanJSON[types.PoolShare](store, []byte(types.PoolShareKeyPrefix))
	if err != nil {
		return GenesisState{}, err
	}
	// A store written before this layout existed still has Pools/PoolShares
	// inside the blob and mirror records that agree with them; prefer the
	// blob's copy in that case so the two can never be double-counted.
	if len(gs.State.Pools) == 0 {
		gs.State.Pools = pools
	}
	if len(gs.State.PoolShares) == 0 {
		gs.State.PoolShares = shares
	}
	return gs, nil
}

// writeReplacingState makes the store hold exactly gs, whatever it held before.
// InitGenesisState uses it, which means it must cope with a store that is not
// empty: importing a genesis over populated state has to REMOVE the pools and
// shares that genesis does not mention. Reading the committed baseline rather
// than trusting k.written is what makes that safe -- k.written describes
// whatever this keeper last did, which for a fresh keeper over an existing
// store is nothing at all.
//
// Records that survive an import unmentioned would be resurrected by the next
// read, since these records are authoritative rather than a mirror of some
// other copy.
func (k *Keeper) writeReplacingState(ctx context.Context, gs GenesisState) error {
	if k.storeService == nil {
		return nil
	}
	committed, err := k.readGenesisState(ctx)
	if err != nil {
		return err
	}
	k.written = cloneGenesis(committed)
	return k.writeDiff(ctx, gs)
}

// writeDiff persists next, touching only the records whose bytes differ from
// k.written. Records that disappeared are deleted -- previously they were left
// behind, so a fully unbonded PoolShare stayed readable at its key forever.
func (k *Keeper) writeDiff(ctx context.Context, next GenesisState) error {
	if k.storeService == nil {
		return nil
	}
	residual, hot := splitHotCollections(cloneGenesis(next))
	// prefixgenesis.Save compares each field against the committed bytes and
	// skips it when equal, so this is a no-op write for the common mutation
	// that only touches a pool or a share.
	if err := prefixgenesis.Save(ctx, k.storeService, genesisKey, residual); err != nil {
		return err
	}
	_, prevHot := splitHotCollections(k.written)
	store := k.storeService.OpenKVStore(ctx)

	if err := syncRecords(store, prevHot.pools, hot.pools,
		func(p types.NominatorPool) string { return p.PoolID },
		func(p types.NominatorPool) []byte { return types.PoolKey(p.PoolID) },
	); err != nil {
		return err
	}
	if err := syncRecords(store, prevHot.shares, hot.shares,
		func(s types.PoolShare) string { return s.PoolID + "/" + s.Owner },
		func(s types.PoolShare) []byte { return types.PoolShareKey(s.PoolID, s.Owner) },
	); err != nil {
		return err
	}
	k.written = cloneGenesis(next)
	return nil
}

// syncRecords brings the per-entity records for one collection in line with
// next: Set for entities that are new or whose bytes changed, Delete for
// entities that are gone. Marshaling and comparing cost no gas; only the Set
// and Delete calls that survive the comparison do.
func syncRecords[T any](
	store corestore.KVStore,
	prev, next []T,
	id func(T) string,
	key func(T) []byte,
) error {
	prevBytes := make(map[string][]byte, len(prev))
	for _, value := range prev {
		bz, err := json.Marshal(value)
		if err != nil {
			return err
		}
		prevBytes[id(value)] = bz
	}
	for _, value := range next {
		bz, err := json.Marshal(value)
		if err != nil {
			return err
		}
		name := id(value)
		existing, had := prevBytes[name]
		delete(prevBytes, name)
		if had && bytes.Equal(existing, bz) {
			continue
		}
		if err := store.Set(key(value), bz); err != nil {
			return err
		}
	}
	// Whatever is left in prevBytes no longer exists in next.
	for _, value := range prev {
		if _, stale := prevBytes[id(value)]; !stale {
			continue
		}
		if err := store.Delete(key(value)); err != nil {
			return err
		}
	}
	return nil
}
