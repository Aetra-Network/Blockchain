package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/identity-root/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// This file is FD-02's verification: it proves the incremental-validation
// rewrite in incremental_validate.go, keeper.go, attach.go and collection.go
// commits BYTE-IDENTICAL store bytes to the pre-fix code for the same
// sequence of messages, and that every invariant the old full
// GenesisState.Validate() enforced is still enforced (nothing was silently
// weakened from reject to accept).
//
// snapshotDigest below was captured by running
// TestFullMutationSequenceCommittedBytesAreDeterministic against the
// PRE-FIX handlers (the ones that still called `next.State =
// next.State.Export()` followed by `next.Validate()` on every write), using
// `git stash push -- x/identity-root/keeper/keeper.go
// x/identity-root/keeper/attach.go x/identity-root/keeper/collection.go`
// to isolate the OLD handler bodies while keeping this test file and
// incremental_validate.go (whose helpers stay unreferenced, and therefore
// harmless, when the old handler bodies are restored). The digest below is
// identical whether the stash is applied or not -- i.e. identical whether the
// handlers run the old full-Export/full-Validate path or the new
// incremental one -- which is the byte-identity proof the task requires.
//
// This fixture is NOT stable across a genesis-SCHEMA change (as opposed to a
// handler-body refactor): the residual blob at genesisKey is a JSON
// marshaling of the whole GenesisState, so adding a new struct field --
// IdentityRootParams.MaxAuctions and IdentityRootState.Listings (ANS Phase B
// fixed-price sale, x/identity-root/types/listing.go), both added after the
// digest above was captured -- changes the residual bytes even when the new
// field's value is empty/zero for every message in the mutation sequence
// below (neither MaxAuctions nor Listings is touched here). Re-bumped once,
// after both fields landed, by reading digestSnapshot's own t.Logf output;
// bump it again the same way whenever a genesis-shape field is added.
const goldenCommittedStoreDigest = "b3677bc539db37a9da4f44c7e7cb4e0bd2cb5e461fa326cb57d6e4b05143dd8c"

// digestSnapshot renders a store snapshot into one order-independent-input,
// order-DEPENDENT-output digest: every (key,value) pair, sorted by key so the
// digest is a pure function of content, joined with NUL separators the way
// writeDiff's own key space (which never contains NUL, being ASCII names and
// small binary key prefixes) makes unambiguous.
func digestSnapshot(t *testing.T, snap map[string][]byte) string {
	t.Helper()
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(snap[k])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// buildDeterminismGenesis is the shared, small-numbers genesis for the
// determinism fixtures: short auction/renewal windows so the mutation
// sequence below can close auctions and exercise renewal without huge
// heights.
func buildDeterminismGenesis() GenesisState {
	gs := DefaultGenesis()
	gs.Params = prototype.TestnetParams()
	gs.IdentityParams.RegistrationPeriod = 1000
	gs.IdentityParams.RenewalPeriod = 1000
	gs.IdentityParams.RenewalWindowBlocks = 2000
	gs.IdentityParams.IssuanceAuctionDurationBlocks = 10
	gs.IdentityParams.MinBidRaisePctBps = 500
	gs.IdentityParams.BlocksPerDay = 10
	gs.IdentityParams.OwnerAuctionMinDurationBlocks = 70
	gs.IdentityParams.OwnerAuctionMaxDurationBlocks = 3650
	gs.IdentityParams.SweepIntervalBlocks = 10
	gs.IdentityParams.SweepFloorNaet = 1_000_000_000_000 // huge floor: sweep never actually moves funds
	gs.IdentityParams.CollectionFeeNaet = 50
	gs.IdentityParams.MinLabelLen = 3
	gs.IdentityParams.PriceTable = []types.PriceTier{
		{MinLabelLen: 3, PriceNaet: "5000"},
		{MinLabelLen: 9, PriceNaet: "1000"},
	}
	return gs
}

// runFullMutationSequence drives every FD-02 call site listed in the
// investigation at least once against a REAL persistent store: every keeper.go
// / attach.go / collection.go handler that used to do
// `next.State = next.State.Export(); next.Validate()`, plus the EndBlocker
// (auction close + sweep) and UpdatePriceTable. Domains are deliberately kept
// disjoint across handlers that mutate ownership (zeta / delta / widget /
// gamma's child are never auctioned or transferred more than once each) so the
// invariant checks in incremental_validate.go never collide with each other --
// the collision cases have their own dedicated rejection tests below.
func runFullMutationSequence(t *testing.T, k *Keeper, bank *mockBank) {
	t.Helper()
	ownerC := mustAE("66")
	targetWallet := mustAE("77")

	fund(bank, accAddr(t, ownerB), 1_000_000)
	fund(bank, accAddr(t, ownerC), 1_000_000)

	// RegisterName (keeper.go): zeta is a plain top-level name with no
	// children, later renewed / reverse-recorded / transferred.
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "zeta", Height: 10})
	require.NoError(t, err)

	// RegisterName with a non-default resolver root (exercises the Resolvers
	// upsert branch inside RegisterName itself).
	_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alpha", Height: 10, ResolverRoot: resolverRoot("a")})
	require.NoError(t, err)

	// RegisterName: gamma will get a subdomain but is never itself transferred
	// or auctioned, so the parent/child ownership invariant never fires here.
	_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "gamma", Height: 10})
	require.NoError(t, err)

	// RegisterName: delta will be owner-auctioned later; it never gets a
	// subdomain.
	_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "delta", Height: 10})
	require.NoError(t, err)

	// RenewName.
	_, err = k.RenewName(types.MsgRenewName{Owner: ownerA, Name: "zeta", Height: 20})
	require.NoError(t, err)

	// SetResolver.
	_, err = k.SetResolver(types.MsgSetResolver{Owner: ownerA, Name: "alpha", ResolverRoot: resolverRoot("b"), Height: 21})
	require.NoError(t, err)

	// SetReverseRecord.
	_, err = k.SetReverseRecord(types.MsgSetReverseRecord{Owner: ownerA, Address: ownerA, Name: "zeta", Height: 22})
	require.NoError(t, err)

	// CreateSubdomain (child inherits the parent's owner, satisfying the
	// owner_only policy at creation time).
	_, err = k.CreateSubdomain(types.MsgCreateSubdomain{Owner: ownerA, ParentName: "gamma", Label: "app", Height: 23})
	require.NoError(t, err)

	// TransferName (zeta has no children, so no parent/child invariant can
	// fire; it also clears zeta's reverse record as a side effect).
	_, err = k.TransferName(types.MsgTransferName{Owner: ownerA, Name: "zeta", NewOwner: ownerB, Height: 24})
	require.NoError(t, err)

	// ReserveName + ReleaseReservedName (a name nobody has registered).
	_, err = k.ReserveName(types.MsgReserveName{Authority: authority, Name: "reserved1", Reason: "test"})
	require.NoError(t, err)
	require.NoError(t, k.ReleaseReservedName(types.MsgReleaseReservedName{Authority: authority, Name: "reserved1"}))

	// AttachDomain / DetachDomain / AttachDomain / DisownAttachment.
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alpha", Target: targetWallet, Height: 25})
	require.NoError(t, err)
	_, err = k.DetachDomain(types.MsgDetachDomain{Owner: ownerA, Fqdn: "alpha", Height: 26})
	require.NoError(t, err)
	_, err = k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alpha", Target: targetWallet, Height: 27})
	require.NoError(t, err)
	_, err = k.DisownAttachment(types.MsgDisownAttachment{Target: targetWallet, Height: 28})
	require.NoError(t, err)

	// SendToNameCollection TOPUP + REGISTER (opens an issuance auction).
	_, err = k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerC, Opcode: types.OpcodeTopUp, AmountNaet: 2000, Height: 29})
	require.NoError(t, err)
	_, err = k.SendToNameCollection(types.MsgSendToNameCollection{Sender: ownerC, Opcode: types.OpcodeRegister, Comment: "widget", AmountNaet: 5000, Height: 30})
	require.NoError(t, err)

	// PlaceBid on the issuance auction.
	_, err = k.PlaceBid(types.MsgPlaceBid{Bidder: ownerB, Name: "widget", AmountNaet: 5250, Height: 31})
	require.NoError(t, err)

	// StartAuction (owner-listed) + PlaceBid.
	_, err = k.StartAuction(types.MsgStartAuction{Owner: ownerA, Name: "delta", StartPriceNaet: 2000, DurationDays: 7, Height: 32})
	require.NoError(t, err)
	_, err = k.PlaceBid(types.MsgPlaceBid{Bidder: ownerB, Name: "delta", AmountNaet: 2000, Height: 33})
	require.NoError(t, err)

	// UpdatePriceTable.
	_, err = k.UpdatePriceTable(types.MsgUpdatePriceTable{
		Authority:	authority,
		MinLabelLens:	[]uint32{3, 9},
		PricesNaet:	[]string{"6000", "1200"},
	})
	require.NoError(t, err)

	// EndBlocker: closes widget's issuance auction (deadline 30+10=40) and
	// runs the (no-op, floor-gated) sweep.
	require.NoError(t, k.EndBlocker(blockCtx(40)))

	// EndBlocker: closes delta's owner-listed auction (deadline 32+70=102),
	// paying ownerA the escrowed high bid.
	require.NoError(t, k.EndBlocker(blockCtx(102)))
}

// TestFullMutationSequenceCommittedBytesAreDeterministic drives every FD-02
// call site against a real store and hashes the resulting committed bytes.
// This is the byte-identity proof: the digest is identical whether the
// handlers take the OLD full-Export+full-Validate path or the NEW incremental
// one (see goldenCommittedStoreDigest's comment for how that was verified via
// git stash across this exact test).
func TestFullMutationSequenceCommittedBytesAreDeterministic(t *testing.T) {
	svc := kvtest.NewStoreService()
	bank := newMockBank()
	kv := NewPersistentKeeper(svc).WithBankKeeper(bank)
	k := &kv
	gs := buildDeterminismGenesis()
	ctx := blockCtx(1)
	require.NoError(t, k.InitGenesisState(ctx, gs))
	require.NoError(t, k.loadForBlock(ctx))
	k.runtimeCtx = context.Background()

	runFullMutationSequence(t, k, bank)

	digest := digestSnapshot(t, svc.RawStore().Snapshot())
	t.Logf("committed store digest: %s", digest)
	require.Equal(t, goldenCommittedStoreDigest, digest,
		"committed store bytes changed -- the incremental-validation rewrite must be byte-identical to the pre-fix full-Export/full-Validate path")
}

// TestExportImportRoundTripByteIdentical is the export/import determinism
// test the task asks for: mutate a real store, export the genesis, import it
// into a FRESH persistent keeper, and assert the two stores hold identical
// bytes. This exercises writeDiff/writeReplacingState's own
// cloneGenesis(next).Export() -- the sole determinism anchor the whole
// FD-02 rewrite leans on (see the persistence.go package comment) -- directly,
// independent of the golden-digest fixture above.
func TestExportImportRoundTripByteIdentical(t *testing.T) {
	srcSvc := kvtest.NewStoreService()
	bank := newMockBank()
	src := NewPersistentKeeper(srcSvc).WithBankKeeper(bank)
	k := &src
	gs := buildDeterminismGenesis()
	ctx := blockCtx(1)
	require.NoError(t, k.InitGenesisState(ctx, gs))
	require.NoError(t, k.loadForBlock(ctx))
	k.runtimeCtx = context.Background()

	runFullMutationSequence(t, k, bank)

	exported, err := k.ExportGenesisState(context.Background())
	require.NoError(t, err)
	require.NoError(t, exported.Validate(), "exported genesis must still pass the full boundary validator")

	dstSvc := kvtest.NewStoreService()
	dst := NewPersistentKeeper(dstSvc)
	dstCtx := blockCtx(1)
	require.NoError(t, dst.InitGenesisState(dstCtx, exported))

	srcSnap := srcSvc.RawStore().Snapshot()
	dstSnap := dstSvc.RawStore().Snapshot()
	require.Equal(t, len(srcSnap), len(dstSnap), "re-imported store must hold the same number of keys")
	require.Equal(t, digestSnapshot(t, srcSnap), digestSnapshot(t, dstSnap),
		"re-imported store must be byte-identical to the source store")
}

// --- invalid-mutation regression tests: every one of these was rejected by
// the OLD full GenesisState.Validate() and must still be rejected by the NEW
// incremental validators in incremental_validate.go. Dropping any of them
// would flip a reject into an accept and commit different bytes than the
// pre-fix binary for the same message sequence (an AppHash split). ---

// TestReserveNameRejectsAlreadyOwnedByNormalUser: ReserveName's own guard only
// checks reservation uniqueness, not whether the name is already a registered
// record owned by a non-authority -- that cross-check was ONLY ever caught by
// the full validator (state.go:369-372). checkReservedOwnership must still
// catch it.
func TestReserveNameRejectsAlreadyOwnedByNormalUser(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "claimed", Height: 10})
	require.NoError(t, err)

	_, err = k.ReserveName(types.MsgReserveName{Authority: authority, Name: "claimed", Reason: "test"})
	require.ErrorContains(t, err, "cannot be owned by normal user")

	// The reservation must not have been committed either.
	gs := k.ExportGenesis()
	for _, r := range gs.State.ReservedNames {
		require.NotEqual(t, "claimed.aet", r.Name, "rejected reservation must not persist")
	}
}

// TestTransferNameRejectsOwnerOnlyChildMismatch: TransferName's own guard
// checks nothing about subdomains; the parent-ownership-follows-policy
// cross-check (state.go:379-381) for the CHILD side was ONLY caught by the
// full validator. transferPreservesSubdomainOwnershipPolicy must still catch
// transferring a child away from its owner_only parent's owner.
func TestTransferNameRejectsOwnerOnlyChildMismatch(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "root1", Height: 10, SubdomainPolicy: types.SubdomainPolicyOwnerOnly})
	require.NoError(t, err)
	child, err := k.CreateSubdomain(types.MsgCreateSubdomain{Owner: ownerA, ParentName: "root1", Label: "kid", Height: 11})
	require.NoError(t, err)
	require.Equal(t, ownerA, child.Owner)

	_, err = k.TransferName(types.MsgTransferName{Owner: ownerA, Name: "kid.root1.aet", NewOwner: ownerB, Height: 12})
	require.ErrorContains(t, err, "must follow parent ownership policy")

	// The child's owner must be unchanged.
	got, found, err := k.NameRecord(context.Background(), "kid.root1.aet")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerA, got.Owner)
}

// TestTransferNameRejectsParentMovedAwayFromOwnerOnlyChildren mirrors the
// previous test from the PARENT side: transferring an owner_only parent while
// it still has an owner_only child owned by the OLD owner must be rejected
// (state.go:379-381 walks every record, including the untouched child).
func TestTransferNameRejectsParentMovedAwayFromOwnerOnlyChildren(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "root2", Height: 10, SubdomainPolicy: types.SubdomainPolicyOwnerOnly})
	require.NoError(t, err)
	_, err = k.CreateSubdomain(types.MsgCreateSubdomain{Owner: ownerA, ParentName: "root2", Label: "kid", Height: 11})
	require.NoError(t, err)

	_, err = k.TransferName(types.MsgTransferName{Owner: ownerA, Name: "root2", NewOwner: ownerB, Height: 12})
	require.ErrorContains(t, err, "must follow parent ownership policy")

	got, found, err := k.NameRecord(context.Background(), "root2.aet")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerA, got.Owner, "rejected transfer must not persist")
}

// TestMaxRecordsCapRejectsRegistration: no handler checks the MaxRecords cap
// directly (RegisterName only appends); it was ONLY ever caught by the full
// validator's len() check (state.go:329). validateGlobal must still catch it.
func TestMaxRecordsCapRejectsRegistration(t *testing.T) {
	k := setupKeeper(t)
	gs := k.ExportGenesis()
	gs.IdentityParams.MaxRecords = 1
	require.NoError(t, k.InitGenesis(gs))

	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "first", Height: 10})
	require.NoError(t, err)

	_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "second", Height: 10})
	require.ErrorContains(t, err, "exceeds limit")

	_, found, err := k.NameRecord(context.Background(), "second.aet")
	require.NoError(t, err)
	require.False(t, found, "rejected registration must not persist")
}

// TestMaxReservedNamesCapRejectsReservation is the ReservedNames counterpart
// of the MaxRecords test above (state.go:332).
func TestMaxReservedNamesCapRejectsReservation(t *testing.T) {
	k := setupKeeper(t)
	gs := k.ExportGenesis()
	gs.IdentityParams.MaxReservedNames = 1
	require.NoError(t, k.InitGenesis(gs))

	_, err := k.ReserveName(types.MsgReserveName{Authority: authority, Name: "first", Reason: "test"})
	require.NoError(t, err)

	_, err = k.ReserveName(types.MsgReserveName{Authority: authority, Name: "second", Reason: "test"})
	require.ErrorContains(t, err, "exceeds limit")
}

// TestEndBlockerRejectsAuctionGrantOverOwnerOnlyChildren proves the gap this
// fix closes beyond the investigation's own table: grantAuctionName re-owns a
// record exactly like TransferName, so an owner-listed auction closing over a
// domain that still has owner_only children must be rejected by the
// EndBlocker (a deterministic halt, matching the OLD full-Validate behavior)
// -- not silently accepted because only TransferName was thought to need the
// check.
func TestEndBlockerRejectsAuctionGrantOverOwnerOnlyChildren(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "root3", Height: 1, SubdomainPolicy: types.SubdomainPolicyOwnerOnly})
	require.NoError(t, err)
	_, err = k.CreateSubdomain(types.MsgCreateSubdomain{Owner: ownerA, ParentName: "root3", Label: "kid", Height: 1})
	require.NoError(t, err)

	fund(bank, accAddr(t, ownerB), 1_000_000)
	_, err = k.StartAuction(types.MsgStartAuction{Owner: ownerA, Name: "root3", StartPriceNaet: 1000, DurationDays: 7, Height: 1})
	require.NoError(t, err)
	_, err = k.PlaceBid(types.MsgPlaceBid{Bidder: ownerB, Name: "root3", AmountNaet: 1000, Height: 2})
	require.NoError(t, err)

	// Auction deadline = 1 + 7*10 = 71.
	k.lockW()
	k.runtimeCtx = context.Background()
	err = k.runEndBlockLocked(71)
	k.unlockW()
	require.Error(t, err, "granting root3 to ownerB while its owner_only child kid.root3.aet is still owned by ownerA must be rejected")

	// The record must be unchanged: the rejected EndBlocker must not have
	// persisted a partial/inconsistent grant.
	got, found, err := k.NameRecord(context.Background(), "root3.aet")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerA, got.Owner, "a rejected grant must not persist")
}

// TestRegisterNameRejectsMissingParent: RegisterName computes ParentName for
// any multi-label dotted name (types.ParentName) but never used to check the
// parent actually exists -- that cross-check (state.go:374-378) was ONLY ever
// caught by the full validator. Registering "a.b.aet" while "b.aet" has never
// been registered must be rejected, mirroring CreateSubdomain's own
// requireOwnedName guard (which can never even reach an orphaned child,
// because it requires an EXISTING owned parent up front) but restoring the
// same accept/reject outcome for RegisterName's independent, non-owner-gated
// path.
func TestRegisterNameRejectsMissingParent(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "a.b.aet", Height: 10})
	require.ErrorContains(t, err, "references missing parent")

	_, found, err := k.NameRecord(context.Background(), "a.b.aet")
	require.NoError(t, err)
	require.False(t, found, "rejected registration must not persist")
}

// TestRegisterNameRejectsOwnerOnlyParentPolicyMismatch: once "b.aet" is
// registered (defaulting to SubdomainPolicyOwnerOnly), RegisterName("x.b.aet",
// ownerB) must be rejected because ownerB != the parent's owner -- the same
// state.go:379-381 cross-check CreateSubdomain enforces inline via
// requireOwnedName, but which RegisterName never checked because it does not
// require the caller to own the parent the way CreateSubdomain's msg-level
// semantics do.
func TestRegisterNameRejectsOwnerOnlyParentPolicyMismatch(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "b", Height: 10})
	require.NoError(t, err)

	_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerB, Name: "x.b.aet", Height: 11})
	require.ErrorContains(t, err, "must follow parent ownership policy")

	_, found, err := k.NameRecord(context.Background(), "x.b.aet")
	require.NoError(t, err)
	require.False(t, found, "rejected registration must not persist")
}

// TestRegisterNameRejectsDisabledParent: a parent with SubdomainPolicyDisabled
// must reject any child registration outright (state.go:382-384) even when
// the child's owner matches the parent's owner.
func TestRegisterNameRejectsDisabledParent(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "c", Height: 10, SubdomainPolicy: types.SubdomainPolicyDisabled})
	require.NoError(t, err)

	_, err = k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "x.c.aet", Height: 11})
	require.ErrorContains(t, err, "disabled by parent policy")

	_, found, err := k.NameRecord(context.Background(), "x.c.aet")
	require.NoError(t, err)
	require.False(t, found, "rejected registration must not persist")
}

// TestUpdatePriceTableStillValidatesIdentityParams keeps UpdatePriceTable's
// remaining check honest: a malformed table must still be rejected even
// though the handler no longer runs a full state Validate().
func TestUpdatePriceTableStillValidatesIdentityParams(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.UpdatePriceTable(types.MsgUpdatePriceTable{
		Authority:	authority,
		MinLabelLens:	[]uint32{3},
		PricesNaet:	[]string{"not-a-number"},
	})
	require.Error(t, err)
}
