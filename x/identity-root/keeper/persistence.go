package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	corestore "cosmossdk.io/core/store"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// This file is the module's storage layout. It exists to keep a collection
// message's gas cost independent of how many domains the module already holds.
//
// # The problem it solves
//
// The prototype stored the ENTIRE module state -- every NameRecord, resolver,
// reverse record and (as of Phase A) auction -- as a single JSON value at
// genesisKey, read back and rewritten on every mutation. Gas is charged per
// byte written, so a registration's cost grew with the number of domains that
// already exist: O(module state) per message. Graduating identity-root to a
// live system module makes that a real gas slope, exactly the one
// x/contracts/keeper/persistence.go (the template for this file) and
// x/nominator-pool/keeper/persistence.go were written to remove.
//
// # The layout
//
// The four collections that grow with usage -- Records, Resolvers,
// ReverseRecords and Auctions -- are stored as one KV record each, keyed by
// types.NameKey / types.ResolverKey / types.ReverseKey / types.AuctionKey. The
// residual GenesisState (Version, Params, IdentityParams, and the small
// collections NFTBindings / RootAuthorities / ReservedNames plus SweepState)
// stays at genesisKey as one small value.
//
// What changes is the WRITE set: only records whose bytes differ get Set, and
// records that disappeared get Deleted, so a mutation is O(records it touched)
// rather than O(records that exist). The read side still reassembles the full
// state via prefix scans (O(state) read gas, ~10x cheaper per byte than the
// writes it replaces) so Validate keeps running over the whole state and the
// exported genesis is byte-identical to the pre-layout one for the same logical
// state.
//
// # Determinism
//
// writeDiff decides what changed against k.written -- an in-memory map of the
// exact committed bytes of every per-record key, re-established from the store
// by loadForBlock at the top of every consensus entry point. So the write set
// (and the gas charged) is a pure function of (committed state, message), the
// same on every node regardless of uptime or cache state. Every Set/Delete is
// issued over a sorted key slice, so the call sequence is byte-ordered on every
// node instead of following Go's randomized map iteration. Reads are
// unconditional (never cached) for the same reason -- a warm node and a
// restarted node must charge identical gas or the chain halts on an apphash
// mismatch (the FINDING-006 / F-17 bug class).

// hotCollections are the collections stored one KV record per entity instead of
// inside the residual blob.
type hotCollections struct {
	records		[]types.NameRecord
	resolvers	[]types.ResolverRecord
	reverses	[]types.ReverseRecord
	auctions	[]types.Auction
}

// hotRecords maps a store key to the exact bytes believed committed under it.
type hotRecords map[string][]byte

// storeBaseline is what the committed store holds, as read: the per-record
// bytes and the residual blob's bytes. It is what the next write diffs against.
type storeBaseline struct {
	records		hotRecords
	residual	[]byte
}

// splitHotCollections removes the per-record collections from gs and returns
// them alongside the residual GenesisState that stays at genesisKey.
func splitHotCollections(gs GenesisState) (GenesisState, hotCollections) {
	hot := hotCollections{
		records:	gs.State.Records,
		resolvers:	gs.State.Resolvers,
		reverses:	gs.State.ReverseRecords,
		auctions:	gs.State.Auctions,
	}
	gs.State.Records = nil
	gs.State.Resolvers = nil
	gs.State.ReverseRecords = nil
	gs.State.Auctions = nil
	return gs, hot
}

// marshalHotRecords renders the per-record write set: store key -> record bytes.
func marshalHotRecords(hot hotCollections) (hotRecords, error) {
	out := make(hotRecords, len(hot.records)+len(hot.resolvers)+len(hot.reverses)+len(hot.auctions))
	for _, record := range hot.records {
		bz, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		out[string(types.NameKey(record.Name))] = bz
	}
	for _, resolver := range hot.resolvers {
		bz, err := json.Marshal(resolver)
		if err != nil {
			return nil, err
		}
		out[string(types.ResolverKey(resolver.Name))] = bz
	}
	for _, reverse := range hot.reverses {
		bz, err := json.Marshal(reverse)
		if err != nil {
			return nil, err
		}
		out[string(types.ReverseKey(reverse.Address))] = bz
	}
	for _, auction := range hot.auctions {
		bz, err := json.Marshal(auction)
		if err != nil {
			return nil, err
		}
		out[string(types.AuctionKey(auction.Name))] = bz
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
// recording the raw bytes into seen.
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
	// reports a spurious error for every scan that ran to completion, so Valid()
	// is the only usable termination signal -- the SDK's own modules iterate
	// this way too.
	return out, nil
}

// readGenesisState reassembles the full GenesisState from the committed store:
// the residual struct from genesisKey plus the four per-record collections via
// prefix scans. found reports whether the module has any committed state at all.
//
// A store written before this layout existed still holds the collections INSIDE
// the residual blob and has no per-record keys; readGenesisState prefers the
// blob's copy in that case (the len==0 guards) so the two can never be double
// counted -- the first write after the upgrade fans every record out and
// rewrites the residual without them. Getting this backwards would silently
// drop every pre-upgrade domain.
func (k Keeper) readGenesisState(ctx context.Context) (GenesisState, storeBaseline, bool, error) {
	baseline := storeBaseline{records: hotRecords{}}
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(genesisKey)
	if err != nil {
		return GenesisState{}, storeBaseline{}, false, err
	}
	if len(bz) == 0 {
		return GenesisState{}, baseline, false, nil
	}
	baseline.residual = bz
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return GenesisState{}, storeBaseline{}, false, err
	}

	records, err := scanRecords[types.NameRecord](store, types.NameKeyPrefix, baseline.records)
	if err != nil {
		return GenesisState{}, storeBaseline{}, false, err
	}
	resolvers, err := scanRecords[types.ResolverRecord](store, types.ResolverKeyPrefix, baseline.records)
	if err != nil {
		return GenesisState{}, storeBaseline{}, false, err
	}
	reverses, err := scanRecords[types.ReverseRecord](store, types.ReverseKeyPrefix, baseline.records)
	if err != nil {
		return GenesisState{}, storeBaseline{}, false, err
	}
	auctions, err := scanRecords[types.Auction](store, types.AuctionKeyPrefix, baseline.records)
	if err != nil {
		return GenesisState{}, storeBaseline{}, false, err
	}

	if len(gs.State.Records) == 0 {
		gs.State.Records = records
	}
	if len(gs.State.Resolvers) == 0 {
		gs.State.Resolvers = resolvers
	}
	if len(gs.State.ReverseRecords) == 0 {
		gs.State.ReverseRecords = reverses
	}
	if len(gs.State.Auctions) == 0 {
		gs.State.Auctions = auctions
	}
	return gs, baseline, true, nil
}

// writeReplacingState makes the store hold exactly gs, whatever it held before.
// InitGenesisState uses it, so it must cope with a non-empty store: importing a
// genesis over populated state has to REMOVE the records that genesis does not
// mention. Reading the committed baseline rather than trusting k.written is what
// makes that safe -- for a fresh keeper over an existing store, k.written
// describes nothing.
func (k *Keeper) writeReplacingState(ctx context.Context, gs GenesisState) error {
	if k.storeService == nil {
		return nil
	}
	_, baseline, _, err := k.readGenesisState(ctx)
	if err != nil {
		return err
	}
	k.written = baseline.records
	k.writtenResidual = baseline.residual
	return k.writeDiff(ctx, gs)
}

// writeDiff persists next, touching only the records whose bytes differ from
// k.written, and deleting the records that no longer exist.
func (k *Keeper) writeDiff(ctx context.Context, next GenesisState) error {
	if k.storeService == nil {
		return nil
	}
	residual, hot := splitHotCollections(cloneGenesis(next))
	desired, err := marshalHotRecords(hot)
	if err != nil {
		return err
	}
	residualBytes, err := json.Marshal(residual)
	if err != nil {
		return err
	}
	store := k.storeService.OpenKVStore(ctx)

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
