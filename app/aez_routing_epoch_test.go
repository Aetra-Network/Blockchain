package app

import (
	"encoding/json"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	sims "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	aezkeeper "github.com/sovereign-l1/l1/x/aez/keeper"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

// TestAEZRoutingTableSwapsInARealBlockLifecycle is the AEZ Phase 2 live proof.
//
// Every other Phase 2 test calls the keeper or the Msg handler directly. None of
// them proves the thing the promotion to systemModules actually bought: that the
// MODULE MANAGER calls x/aez's BeginBlocker on a real block. A module can
// implement appmodule.HasBeginBlocker perfectly and still never run if it is
// missing from OrderBeginBlockers or wired as the wrong family -- and that
// failure is invisible to keeper tests, which call BeginBlocker themselves.
//
// So this drives real FinalizeBlock calls and asserts the table swaps at exactly
// its activation height, from nothing but the passage of blocks.
//
// The default routing epoch is 10,000 blocks, which is not runnable here, so
// genesis is built with a short epoch. That is a change to a genesis PARAM, not
// to the rule under test: the boundary arithmetic
// (types.IsRoutingEpochBoundary) is identical at 5 and at 10,000.
func TestAEZRoutingTableSwapsInARealBlockLifecycle(t *testing.T) {
	const shortEpoch = uint64(5)

	app, _ := setup(true, 5)
	genesis := GenesisStateWithSingleValidator(t, app)

	aezGenesis := aeztypes.DefaultGenesis()
	aezGenesis.Params.RoutingEpochLength = shortEpoch
	require.NoError(t, aezGenesis.Validate())
	rawAEZ, err := json.Marshal(aezGenesis)
	require.NoError(t, err)
	genesis[aeztypes.ModuleName] = rawAEZ

	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)
	_, err = app.InitChain(&abci.RequestInitChain{
		Validators:		[]abci.ValidatorUpdate{},
		ConsensusParams:	sims.DefaultConsensusParams,
		AppStateBytes:		stateBytes,
	})
	require.NoError(t, err)

	// InitChain stages genesis but does not commit it: run block 1 first so
	// the routing table is readable from committed state.
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	// Stage a table through the real Msg handler, with the real authority.
	// The bucket map is identical to genesis (all core): the core zone is a
	// one-way trap, so this is the only table governance can stage today.
	stageCtx := app.NewUncachedContext(false, cmtproto.Header{Height: 2})
	current, err := app.AEZKeeper.GetRoutingTable(stageCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), current.Version)

	_, err = aezkeeper.NewMsgServerImpl(&app.AEZKeeper).UpdateRoutingTable(stageCtx, &aeztypes.MsgUpdateRoutingTable{
		Authority:		aeztypes.GovAuthority(),
		Version:		2,
		Epoch:			1,
		ActivationHeight:	int64(shortEpoch),
		Buckets:		aeztypes.BucketsFromTable(current),
	})
	require.NoError(t, err)

	// Blocks 2..shortEpoch-1: the BeginBlocker runs every block and must not
	// swap.
	for height := int64(2); height < int64(shortEpoch); height++ {
		_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Hash: app.LastCommitID().Hash})
		require.NoError(t, err)
		_, err = app.Commit()
		require.NoError(t, err)

		ctx := app.NewUncachedContext(false, cmtproto.Header{Height: height})
		table, err := app.AEZKeeper.GetRoutingTable(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), table.Version, "table swapped early, at height %d", height)
	}

	// The activation height itself.
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: int64(shortEpoch), Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: int64(shortEpoch)})
	table, err := app.AEZKeeper.GetRoutingTable(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), table.Version, "the module manager did not run x/aez's BeginBlocker at the boundary")

	// Pending is cleared, so the swap cannot repeat.
	_, found, err := app.AEZKeeper.GetPendingVersion(ctx)
	require.NoError(t, err)
	require.False(t, found)

	// The whole point of Phase 2's core-zone trap: after a real governance
	// swap on a real chain, every entity still resolves to the core zone.
	// Nothing routes anywhere.
	for _, ns := range aeztypes.AllNamespaces() {
		for i := 0; i < 16; i++ {
			zone, err := app.AEZKeeper.ZoneOf(ctx, ns, []byte{byte(i), 0x5a})
			require.NoError(t, err)
			require.Equal(t, aeztypes.ZoneIDCore, zone)
		}
	}
}

// --- real x/gov-driven routing table tests ----------------------------------
//
// Everything above stages its table by calling
// aezkeeper.NewMsgServerImpl(...).UpdateRoutingTable DIRECTLY, with the
// authority field set by the test itself. That proves the module manager runs
// x/aez's BeginBlocker for real, but it never proves anything about
// AUTHORIZATION: nothing stops a test (or a bug) from setting Authority to the
// gov module address and calling the handler straight through, and nothing
// there exercises x/gov's own submit/vote/tally/exec machinery at all.
//
// x/aez/keeper/msg_server.go's own doc comment records what the real
// authorization model actually is: Params.Prototype.Authorize is an EXACT
// STRING match against Params.Prototype.Authority, which DefaultParams points
// at types.GovAuthority() -- the x/gov module account address, the same
// address app/keepers.go wires as every other gov-gated module's authority
// (distribution, slashing, upgrade, mint, ...). x/gov itself is fully wired
// in this app: it is a real module in InitGenesisOrder/EndBlockerOrder
// (app/wiring/aetracore/order.go), and its keeper is constructed with
// app.MsgServiceRouter() (app/keepers.go) -- the EXACT router x/aez's
// AppModule.RegisterServices registers MsgUpdateRoutingTable's handler
// against. So the real, fully-wired authorization model for this message
// today IS x/gov -- not a fast-path authority key, not something else -- and
// x/aez/client/cli/tx.go's own Long help text says as much ("the usual way to
// use this command is with --generate-only, embedding the resulting message
// JSON as one entry in a 'tx gov submit-proposal' messages array"). The two
// tests below are what drives that path for real instead of asserting it from
// a doc comment.
func newAEZGovernanceHarness(t *testing.T, routingEpochLength uint64, votingPeriod time.Duration) (app *L1App, proposerVoter string, genesisTime time.Time) {
	t.Helper()

	app, _ = setup(true, 5)
	genesis := GenesisStateWithSingleValidator(t, app)

	aezGenesis := aeztypes.DefaultGenesis()
	aezGenesis.Params.RoutingEpochLength = routingEpochLength
	require.NoError(t, aezGenesis.Validate())
	rawAEZ, err := json.Marshal(aezGenesis)
	require.NoError(t, err)
	genesis[aeztypes.ModuleName] = rawAEZ

	// Shorten gov's voting period as a genesis PARAM change, exactly the same
	// kind of change TestAEZRoutingTableSwapsInARealBlockLifecycle already
	// makes to aez's routing epoch length: the SDK default (48h) is not
	// runnable here, but the tally/exec rule at that boundary
	// (x/gov's own EndBlocker, unmodified) is identical at 5s and at 48h.
	cdc := app.AppCodec()
	var govGenesis govv1.GenesisState
	cdc.MustUnmarshalJSON(genesis[govtypes.ModuleName], &govGenesis)
	govGenesis.Params.VotingPeriod = &votingPeriod
	genesis[govtypes.ModuleName] = cdc.MustMarshalJSON(&govGenesis)

	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)
	_, err = app.InitChain(&abci.RequestInitChain{
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: sims.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	// Gov's voting-period expiry is TIMESTAMP-driven (VotingEndTime), not
	// height-driven, unlike aez's routing epoch -- so every FinalizeBlock call
	// from here on needs an explicit, controlled Time.
	genesisTime = time.Unix(1_700_000_000, 0).UTC()
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: genesisTime, Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	// GenesisStateWithSingleValidator's one funded account is also the one
	// delegator simtestutil.GenesisStateWithValSet self-bonds the validator
	// with, so it holds 100% of the chain's bonded tokens -- the only account
	// that can ever meet gov's quorum here. Read it back from committed
	// staking state rather than reaching into that helper's internals (it
	// hands back neither the address nor the key), so this harness makes no
	// assumption about that helper beyond what it already documents.
	readCtx := app.NewUncachedContext(false, cmtproto.Header{Height: 1, Time: genesisTime})
	validator := GetBondedTestValidator(t, app, readCtx)
	valAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
	require.NoError(t, err)
	delegations, err := app.StakingKeeper.GetValidatorDelegations(readCtx, sdk.ValAddress(valAddr))
	require.NoError(t, err)
	require.Len(t, delegations, 1, "the harness expects exactly one delegator so it unambiguously carries 100% of bonded voting power")

	return app, delegations[0].DelegatorAddress, genesisTime
}

// aezDistinctBucketEntities returns two 20-byte native-account entity ids
// whose AEZ buckets differ, the same technique x/aez/keeper/bus_test.go's
// unexported twoDistinctBucketAddrs uses to build multizone_test.go's
// non-trivial fixture -- reimplemented here because that helper lives in
// x/aez/keeper and is not reachable from the app package.
func aezDistinctBucketEntities(t *testing.T) (entityA, entityB []byte) {
	t.Helper()
	entityA = make([]byte, 20)
	for i := range entityA {
		entityA[i] = 0x11
	}
	bucketA := aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, entityA)
	for seed := 0x12; seed <= 0xff; seed++ {
		candidate := make([]byte, 20)
		for i := range candidate {
			candidate[i] = byte(seed)
		}
		if aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, candidate) != bucketA {
			return entityA, candidate
		}
	}
	t.Fatal("could not find two native-account entities with distinct AEZ buckets")
	return nil, nil
}

// TestAEZGovernanceSubmitVoteTallyExecutesRealRoutingTableSwap closes the gap
// TestAEZRoutingTableSwapsInARealBlockLifecycle leaves open: that test's own
// comment says it stages a table "through the real Msg handler, with the real
// authority" -- but it gets there with one direct Go call, never through a
// real x/gov MsgSubmitProposal -> vote -> tally -> exec flow. This test drives
// that flow for real:
//
//   - MsgSubmitProposal, carrying MsgUpdateRoutingTable as its one proposal
//     message, submitted through the real x/gov Msg server (not a keeper
//     setter) with a real deposit covering Params.MinDeposit;
//   - MsgVote, cast by the chain's sole bonded delegator (100% of voting
//     power on this harness), through the real x/gov Msg server;
//   - a real tally and a real message-execution dispatch through
//     baseapp.MsgServiceRouter -- the exact router x/aez's RegisterServices
//     registered its handler against -- run from x/gov's own EndBlocker as
//     the module manager invokes it on a real FinalizeBlock call, never a
//     direct Go call to any gov or aez keeper/EndBlocker function;
//   - real BeginBlocker activation at the epoch boundary, exactly as
//     TestAEZRoutingTableSwapsInARealBlockLifecycle already proves for the
//     direct-call path.
//
// The staged table is bucket-identical to genesis (only Version/Epoch/
// ActivationHeight move). See
// TestAEZGovernanceRejectsGenuineMultiZoneRoutingTableEvenWithFullQuorum below
// for why that is not a simplification this test chose, but the only table
// ANY authority can legally stage from genesis -- proven through this exact
// same real path, not merely asserted.
func TestAEZGovernanceSubmitVoteTallyExecutesRealRoutingTableSwap(t *testing.T) {
	const routingEpochLength = uint64(10)
	const votingPeriod = 5 * time.Second

	app, proposerVoter, genesisTime := newAEZGovernanceHarness(t, routingEpochLength, votingPeriod)

	stageCtx := app.NewUncachedContext(false, cmtproto.Header{Height: 2, Time: genesisTime})
	current, err := app.AEZKeeper.GetRoutingTable(stageCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), current.Version)

	updateMsg := &aeztypes.MsgUpdateRoutingTable{
		Authority:        aeztypes.GovAuthority(),
		Version:          2,
		Epoch:            1,
		ActivationHeight: int64(routingEpochLength),
		Buckets:          aeztypes.BucketsFromTable(current),
	}

	govParams, err := app.GovKeeper.Params.Get(stageCtx)
	require.NoError(t, err)

	submitMsg, err := govv1.NewMsgSubmitProposal(
		[]sdk.Msg{updateMsg},
		govParams.MinDeposit,
		proposerVoter,
		"",
		"AEZ routing table version bump",
		"Advance the AEZ routing table to version 2 at the next routing-epoch boundary. The bucket map is unchanged from genesis.",
		false,
	)
	require.NoError(t, err)

	govMsgServer := govkeeper.NewMsgServerImpl(&app.GovKeeper)
	submitResp, err := govMsgServer.SubmitProposal(stageCtx, submitMsg)
	require.NoError(t, err)

	_, err = govMsgServer.Vote(stageCtx, &govv1.MsgVote{
		ProposalId: submitResp.ProposalId,
		Voter:      proposerVoter,
		Option:     govv1.VoteOption_VOTE_OPTION_YES,
	})
	require.NoError(t, err)

	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: genesisTime, Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	// Height 3's block time crosses VotingEndTime (genesisTime + votingPeriod),
	// so x/gov's real EndBlocker -- reached only because the module manager
	// wires it into EndBlockerOrder, not by any direct call here -- tallies
	// and executes the proposal in this exact block.
	pastVoting := genesisTime.Add(votingPeriod + time.Second)
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 3, Time: pastVoting, Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	tallyCtx := app.NewUncachedContext(false, cmtproto.Header{Height: 3, Time: pastVoting})
	proposal, err := app.GovKeeper.Proposals.Get(tallyCtx, submitResp.ProposalId)
	require.NoError(t, err)
	require.Equal(t, govv1.StatusPassed, proposal.Status, "proposal should pass with 100%% Yes turnout; failure reason: %s", proposal.FailedReason)

	pendingVersion, found, err := app.AEZKeeper.GetPendingVersion(tallyCtx)
	require.NoError(t, err)
	require.True(t, found, "x/gov's real EndBlocker must have called the real aez Msg handler and staged the table")
	require.Equal(t, uint64(2), pendingVersion)

	// Blocks up to (not including) the activation height: BeginBlocker runs
	// every block and must not swap early.
	for height := int64(4); height < int64(routingEpochLength); height++ {
		_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Time: pastVoting.Add(time.Duration(height) * time.Second), Hash: app.LastCommitID().Hash})
		require.NoError(t, err)
		_, err = app.Commit()
		require.NoError(t, err)

		ctx := app.NewUncachedContext(false, cmtproto.Header{Height: height})
		table, err := app.AEZKeeper.GetRoutingTable(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), table.Version, "table swapped early, at height %d", height)
	}

	// The activation height itself.
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: int64(routingEpochLength), Time: pastVoting.Add(time.Duration(routingEpochLength) * time.Second), Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	finalCtx := app.NewUncachedContext(false, cmtproto.Header{Height: int64(routingEpochLength)})
	table, err := app.AEZKeeper.GetRoutingTable(finalCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), table.Version, "the real x/gov -> EndBlocker -> MsgServiceRouter -> aez Msg handler path did not swap the table")

	_, found, err = app.AEZKeeper.GetPendingVersion(finalCtx)
	require.NoError(t, err)
	require.False(t, found)
}

// TestAEZGovernanceRejectsGenuineMultiZoneRoutingTableEvenWithFullQuorum is
// the ground-truth proof for the question the routing-epoch test suite could
// not previously answer: can a genuinely multi-zone routing table (buckets
// actually split across Core and elastic zones, not just a version/epoch
// bump) ever be produced through the REAL, fully authorized governance path?
//
// The answer, proven live here rather than merely asserted in a doc comment,
// is no -- not from genesis, not even with a real MsgSubmitProposal, a real
// vote carrying 100% of bonded power, a real passing tally, and a real
// dispatch through x/gov's EndBlocker into the exact MsgServiceRouter x/aez
// registered its handler against. The proposal message reaches the real
// handler, which calls the real keeper.ValidateRoutingTableTransition, which
// enforces I-9 (the Core Zone never migrates): every bucket starts on Core at
// genesis, so ANY table that moves one off Core -- authorized or not -- is
// rejected by keeper/table.go's ValidateRoutingTableTransition, wholly
// independent of the authorization layer around it.
//
// This is exactly the scenario the routing-epoch test suite's own comments
// flag ("the core zone is a one-way trap, so [a bucket-identical table] is
// the only table governance can stage today") -- but proven through the real
// x/gov flow, not the direct-authority shortcut, and with the full negative
// consequences asserted: the proposal is marked FAILED (never re-attempted),
// no pending table is left behind, the routing table itself is byte-for-byte
// unchanged, buckets keep resolving to Core, and -- the property a
// keeper-only test structurally cannot show -- the chain does not halt or
// panic over the failed message execution; it keeps producing blocks.
//
// x/aez/keeper/multizone_test.go's own multi-zone fixtures remain valid and
// necessary: they prove the ROUTING MACHINERY (bus, quotas, pins) behaves
// correctly once a second zone is live. They reach that state via
// installTable, which calls SetPendingRoutingTable directly -- deliberately
// bypassing StageRoutingTable's policy layer, exactly as table.go's own doc
// comment says only the Msg handler may call it. This test is the other half:
// proof that the bypass in those fixtures is not a shortcut around a
// reachable real path, but the ONLY way a multi-zone table exists anywhere in
// this codebase today, in test or on a real chain.
func TestAEZGovernanceRejectsGenuineMultiZoneRoutingTableEvenWithFullQuorum(t *testing.T) {
	const routingEpochLength = uint64(10)
	const votingPeriod = 5 * time.Second

	app, proposerVoter, genesisTime := newAEZGovernanceHarness(t, routingEpochLength, votingPeriod)

	stageCtx := app.NewUncachedContext(false, cmtproto.Header{Height: 2, Time: genesisTime})
	current, err := app.AEZKeeper.GetRoutingTable(stageCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), current.Version)

	// A genuinely multi-zone table: one bucket moves to elastic zone 2, a
	// second, distinct bucket moves to elastic zone 3. Every other bucket,
	// including these two's neighbours, stays on Core -- mirroring
	// multizone_test.go's own subset-remap fixture, not a wholesale
	// relocation.
	entityA, entityB := aezDistinctBucketEntities(t)
	bucketA := aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, entityA)
	bucketB := aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, entityB)
	buckets := aeztypes.BucketsFromTable(current)
	buckets[bucketA] = 2
	buckets[bucketB] = 3

	multiZoneMsg := &aeztypes.MsgUpdateRoutingTable{
		Authority:        aeztypes.GovAuthority(),
		Version:          2,
		Epoch:            1,
		ActivationHeight: int64(routingEpochLength),
		Buckets:          buckets,
	}

	govParams, err := app.GovKeeper.Params.Get(stageCtx)
	require.NoError(t, err)

	submitMsg, err := govv1.NewMsgSubmitProposal(
		[]sdk.Msg{multiZoneMsg},
		govParams.MinDeposit,
		proposerVoter,
		"",
		"AEZ genuine multi-zone remap",
		"Move one bucket to elastic zone 2 and a second, distinct bucket to elastic zone 3, off the Core Zone.",
		false,
	)
	require.NoError(t, err)

	govMsgServer := govkeeper.NewMsgServerImpl(&app.GovKeeper)
	submitResp, err := govMsgServer.SubmitProposal(stageCtx, submitMsg)
	require.NoError(t, err)

	_, err = govMsgServer.Vote(stageCtx, &govv1.MsgVote{
		ProposalId: submitResp.ProposalId,
		Voter:      proposerVoter,
		Option:     govv1.VoteOption_VOTE_OPTION_YES,
	})
	require.NoError(t, err)

	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: genesisTime, Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)

	// Height 3 crosses VotingEndTime: the real EndBlocker tallies (100% Yes,
	// full quorum) and attempts the real dispatch. The block must complete
	// without error even though the message inside it fails -- x/gov catches
	// and records the failure; it never propagates as a block-level error.
	pastVoting := genesisTime.Add(votingPeriod + time.Second)
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 3, Time: pastVoting, Hash: app.LastCommitID().Hash})
	require.NoError(t, err, "a rejected proposal message must not halt block execution")
	_, err = app.Commit()
	require.NoError(t, err)

	afterCtx := app.NewUncachedContext(false, cmtproto.Header{Height: 3, Time: pastVoting})
	proposal, err := app.GovKeeper.Proposals.Get(afterCtx, submitResp.ProposalId)
	require.NoError(t, err)
	require.Equal(t, govv1.StatusFailed, proposal.Status, "a genuinely multi-zone table unexpectedly passed execution")
	require.Contains(t, proposal.FailedReason, "core zone never migrates", "must fail for the documented I-9 reason, not some unrelated bug")

	_, found, err := app.AEZKeeper.GetPendingVersion(afterCtx)
	require.NoError(t, err)
	require.False(t, found, "a failed proposal execution must not leave a pending table behind")

	table, err := app.AEZKeeper.GetRoutingTable(afterCtx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), table.Version, "the routing table must be untouched by a failed proposal execution")
	for i := 0; i < aeztypes.BucketCount; i++ {
		require.Equal(t, aeztypes.ZoneIDCore, table.Buckets[i], "bucket %d must still be Core: no partial write may survive a failed execution", i)
	}

	zoneA, err := app.AEZKeeper.ZoneOf(afterCtx, aeztypes.NamespaceNativeAccount, entityA)
	require.NoError(t, err)
	require.Equal(t, aeztypes.ZoneIDCore, zoneA, "the real governance path could not move this bucket off Core")

	zoneB, err := app.AEZKeeper.ZoneOf(afterCtx, aeztypes.NamespaceNativeAccount, entityB)
	require.NoError(t, err)
	require.Equal(t, aeztypes.ZoneIDCore, zoneB)

	// The chain keeps producing real blocks after the failed execution: no
	// halt, no panic, nothing left in a stuck state.
	_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 4, Time: pastVoting.Add(time.Second), Hash: app.LastCommitID().Hash})
	require.NoError(t, err)
	_, err = app.Commit()
	require.NoError(t, err)
}
