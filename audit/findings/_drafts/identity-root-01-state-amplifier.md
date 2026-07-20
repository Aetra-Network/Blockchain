# [Medium] Identity-root full-state export and validation amplifier

Status: partially fixed (commit e0d7cc15, 2026-07-18) -- see "Fix status" below. The
per-write full `Export()` + `Validate()` replay described in this finding is gone;
a distinct, larger O(N) cost (full deep-clone of every record on each handler
entry) remains and is tracked as the residual open item.

Scope:
- [x/identity-root/types/state.go](/C:/Users/Ryzen/Desktop/L1/x/identity-root/types/state.go#L301)
- [x/identity-root/types/state.go](/C:/Users/Ryzen/Desktop/L1/x/identity-root/types/state.go#L324)
- [x/identity-root/keeper/keeper.go](/C:/Users/Ryzen/Desktop/L1/x/identity-root/keeper/keeper.go#L202)
- [x/identity-root/keeper/persistence.go](/C:/Users/Ryzen/Desktop/L1/x/identity-root/keeper/persistence.go#L152)
- [x/identity-root/keeper/grpc_server.go](/C:/Users/Ryzen/Desktop/L1/x/identity-root/keeper/grpc_server.go#L196)

Summary:
The ANS/identity-root module keeps re-materializing and re-validating its full state on both write and read paths. `loadForBlock` rehydrates the committed store by scanning every hot collection, `IdentityRootState.Export()` clones and sorts every collection, and `IdentityRootState.Validate()` replays validation over the entire exported state. Public queries such as `CollectionBalance` and `Auctions` also traverse the whole collection rather than a bounded index.

Evidence:
- `readGenesisState` scans every hot collection with prefix iteration, including records, resolvers, reverse records, auctions, and attachments.
- `loadForBlock` calls `readGenesisState` and then validates the reconstructed state before every consensus entry point.
- `IdentityRootState.Export()` clones and sorts records, resolvers, reverse records, bindings, authorities, reserved names, auctions, and attachments every time it is called.
- `IdentityRootState.Validate()` re-runs validation over the whole exported state.
- `CollectionBalance` uses `openEscrowTotal(k.genesis.State.Auctions)`, and `Auctions` / `Subdomains` expose full-state scans on the query surface.

Validation receipt:
- Synthetic benchmark on a valid state with 100,000 records:
  - `Export = 22.8553 ms`
  - `Validate = 146.1764 ms`

Impact:
The cost of ANS mutation and several public queries scales with total module state, not just the touched record. Once the module accumulates large state, a normal write or query becomes a usable CPU/latency DoS lever.

Recommendation:
- Move hot-path validation toward incremental checks instead of full-state replay.
- Avoid whole-state `Export()` in mutation handlers.
- Add explicit caps for any collection that is still unbounded.
- Prefer indexed or paginated query surfaces over full-state scans.

## Fix status (2026-07-20)

Commit `e0d7cc15` ("ANS: incremental per-write validation, replacing
full-state Export+Validate") fixes the specific mechanism this finding
described -- `loadForBlock` / handler entry calling `next.State =
next.State.Export()` followed by a full `next.Validate()` (the O(N) sort +
O(N) validation replay) on every single mutation -- and replaces it with
`validateGlobal` + a handful of O(touched) cross-record checks
(`checkReservedOwnership`, `transferPreservesSubdomainOwnershipPolicy`,
`requireParentPolicySatisfied`, `validateGrantedName`) in
`x/identity-root/keeper/incremental_validate.go`. `loadForBlock`'s duplicate
per-message full `Validate()` is downgraded to a params-only check.

Proof of no behavior change: `TestFullMutationSequenceCommittedBytesAreDeterministic`
(`x/identity-root/keeper/incremental_validate_determinism_test.go:199`) drives
every FD-02 call site against a real persistent store and hashes every
committed (key,value) pair (`goldenCommittedStoreDigest =
b3677bc539db37a9da4f44c7e7cb4e0bd2cb5e461fa326cb57d6e4b05143dd8c` as of the
current tree). Per the test file's own header comment, this constant is
intentionally re-bumped (by reading `digestSnapshot`'s `t.Logf` output)
whenever the `GenesisState` shape gains a field, since the residual blob at
`genesisKey` is a full JSON marshaling of it; it has moved at least once since
this draft was written, for that reason, and should be expected to keep
moving -- the hex value itself is not load-bearing for this finding, only that
the test keeps passing. Per the original commit message, the byte-identity
proof (old full-Validate path vs. the new incremental one) was independently
verified via a git-stash A/B against the pre-fix handler bodies -- i.e. the
incremental checks accept/reject exactly what the old full-Validate path did,
for the exercised mutation sequence.

The commit message also discloses (and the fix does NOT claim to have found)
two separate pre-existing bugs it repairs along the way: a dropped
parent-existence/owner_only check in `RegisterName` (restored via
`requireParentPolicySatisfied`; not live-exploitable today because
`RegisterName`/`RenewName`/`TransferName`/`SetResolver`/`SetReverseRecord`/
`ReserveName`/`ReleaseReservedName` are not wired to any Msg-service path
yet), and duplicate full-Validate calls collapsed from ~3 to 1 occurrence per
write before this fix's further work.

### Residual open item: O(N) deep-clone-per-write is NOT fixed

The fix above removed the O(N log N) `sort.SliceStable` cost that ran at
every handler entry (benchmarked ~342ms -> ~39ms at 100k records for the
isolated handler-entry clone, via the new `IdentityRootState.ExportUnsortedHot`
/ `cloneGenesisUnsorted` in `x/identity-root/types/state.go:367` and
`x/identity-root/keeper/keeper.go:1024`, which skip sorting the five HOT
collections -- records, resolvers, reverse records, auctions, attachments --
since their order is not AppHash-load-bearing, while still sorting the three
wire-significant residual collections: NFTBindings, RootAuthorities,
ReservedNames).

It did NOT remove the underlying O(N) full deep-clone of every record,
resolver, reverse record, auction, and attachment that still runs on every
single handler entry (`cloneGenesis` / `cloneGenesisUnsorted` in
`x/identity-root/keeper/keeper.go`), nor the independent O(N) re-sort/re-clone
`writeDiff` performs at persist time from its own `cloneGenesis(next)` call --
a second full-state clone per write that this commit explicitly did not touch.
So a write against a 100k-record state still pays an O(N) clone cost (of
copying-not-sorting order, i.e. cheaper than before but still linear in total
module state, not in the touched record count) at least twice: once at
handler entry, once at persist time.

The commit's own message states this precisely and marks it unfixed: "the
O(N) deep-clone of every record/resolver/reverse/auction/attachment remains
(both here and in writeDiff's own independent re-sort at persist time, which
this change does not touch) -- a true O(changed-records-only) mutation path
needs a map-indexed in-memory representation instead of sorted slices +
linear recordIndex/auctionIndex scans. That is a separate, larger initiative,
tracked as a followup, not claimed as solved here."

This residual item -- replace the sorted-slice-based `IdentityRootState`
representation (and its linear `recordIndex`/`auctionIndex` lookups) with a
map-indexed representation so a single-record mutation touches O(1) records
instead of O(N) -- remains open and is not scheduled in this pass. It should
be filed/tracked as its own finding or backlog item rather than folded back
into FD-02, since it is a larger and architecturally distinct change (data
structure choice, not validation strategy) from what e0d7cc15 fixed.

The public full-state query surface named in this finding's Evidence
(`CollectionBalance`'s `openEscrowTotal`, `Auctions`, `Subdomains`) was out of
scope for e0d7cc15 (which only touched write-path handler validation) and is
unverified as of this update -- re-check those query handlers before closing
this finding fully.
