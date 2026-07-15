package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
	"github.com/sovereign-l1/l1/x/internal/prototype"
	"github.com/sovereign-l1/l1/x/validator-election/types"
	validatorregistrytypes "github.com/sovereign-l1/l1/x/validator-registry/types"
)

func TestDefaultGenesisValidates(t *testing.T) {
	require.NoError(t, DefaultGenesis().Validate())
}

func TestValidatorSetTransitionAcrossEpochs(t *testing.T) {
	k := NewKeeper()
	applyCandidate(t, &k, 0x11, 100, 100, 2)
	applyCandidate(t, &k, 0x22, 50, 50, 3)

	result, err := k.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)
	require.Len(t, result.NextSet, 2)

	transition, err := k.FinalizeElection(types.MsgFinalizeElection{Authority: prototype.DefaultAuthority, Height: 101})
	require.NoError(t, err)
	require.Equal(t, uint64(1), transition.Epoch)
	require.Equal(t, result.NextSet, transition.CurrentSet)
	require.Equal(t, transition.CurrentSet, k.CurrentValidatorSet())
	require.Empty(t, k.NextValidatorSet())
	require.Equal(t, uint64(2), k.Election().ElectionEpoch)

	stored, found := k.ValidatorSetTransition(1)
	require.True(t, found)
	require.Equal(t, transition, stored)
}

// TestFinalizeElectionDoesNotHaltOnCommittedEmptySet covers SA2-S03: when the
// sole pending candidate is also a pending exit, computeNextSet returns nothing
// and CommitElection commits an empty NextValidatorSet. FinalizeElection must
// still finalize that committed-but-empty election instead of erroring every
// block ("must be committed before finalization"), which would halt the chain.
func TestFinalizeElectionDoesNotHaltOnCommittedEmptySet(t *testing.T) {
	k := NewKeeper()
	applyCandidate(t, &k, 0x11, 100, 100, 2)
	_, err := k.RequestValidatorExit(types.MsgRequestValidatorExit{
		Authority:       prototype.DefaultAuthority,
		OperatorAddress: testAddress(0x11),
		Height:          3,
	})
	require.NoError(t, err)

	result, err := k.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)
	require.Empty(t, result.NextSet) // the exiting sole candidate yields an empty commit

	_, err = k.FinalizeElection(types.MsgFinalizeElection{Authority: prototype.DefaultAuthority, Height: 101})
	require.NoError(t, err) // previously errored and halted every block
	require.Empty(t, k.CurrentValidatorSet())
	require.Equal(t, uint64(2), k.Election().ElectionEpoch)
}

// TestApplyForValidatorSetRejectsDuplicateConsensusKey covers the SA2-S05
// regression fix: a duplicate consensus key must be rejected at submission so
// the auto-commit EndBlocker never builds a set that trips validateValidatorSet
// and halts the chain.
func TestApplyForValidatorSetRejectsDuplicateConsensusKey(t *testing.T) {
	k := NewKeeper()
	applyCandidate(t, &k, 0x11, 100, 100, 2)

	_, err := k.ApplyForValidatorSet(types.MsgApplyForValidatorSet{
		Authority: prototype.DefaultAuthority,
		Application: types.CandidateApplication{
			OperatorAddress:    testAddress(0x22),
			ConsensusPublicKey: testConsensusKey(0x11), // same key as candidate 0x11
			RequestedPower:     50,
			SelfBond:           50,
			ValidatorStatus:    validatorregistrytypes.StatusCandidate,
		},
		Height: 3,
	})
	require.ErrorContains(t, err, "consensus key already used")
}

func TestExportImportDuringActiveElection(t *testing.T) {
	source := NewKeeper()
	applyCandidate(t, &source, 0x11, 100, 100, 2)
	applyCandidate(t, &source, 0x22, 50, 50, 3)
	_, err := source.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)

	exported := source.ExportGenesis()
	require.NoError(t, exported.Validate())
	target := NewKeeper()
	require.NoError(t, target.InitGenesis(exported))
	require.Equal(t, exported, target.ExportGenesis())
	require.NoError(t, NewMigrator(&target).Migrate1to2())
	require.Len(t, target.NextValidatorSet(), 2)
	require.Len(t, target.ElectionCandidates(), 2)
}

func TestPersistentRuntimeMutationSurvivesRestartAndImport(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	source := NewPersistentKeeper(service)
	require.NoError(t, source.InitGenesisState(ctx, DefaultGenesis()))

	applyCandidate(t, &source, 0x61, 100, 100, 2)
	result, err := source.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)
	require.Len(t, result.NextSet, 1)

	restarted := NewPersistentKeeper(service)
	exported, err := restarted.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Len(t, exported.State.NextValidatorSet, 1)

	imported := NewPersistentKeeper(kvtest.NewStoreService())
	require.NoError(t, imported.InitGenesisState(ctx, exported))
	require.Len(t, imported.NextValidatorSet(), 1)
	require.Len(t, imported.ElectionCandidates(), 1)
}

// TestRestartedKeeperLoadsCommittedStateForBlock is the regression guard for
// SEC-HIGH: election EndBlocker drives consensus off in-memory genesis that is
// never restored on restart/state-sync. A fresh keeper (a restarted or
// state-synced node, where InitChain is not re-run) starts with the empty
// default in memory, and must hydrate the committed election state on the block
// context before making consensus decisions.
func TestRestartedKeeperLoadsCommittedStateForBlock(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()

	source := NewPersistentKeeper(service)
	require.NoError(t, source.InitGenesisState(ctx, DefaultGenesis()))
	applyCandidate(t, &source, 0x61, 100, 100, 2)
	_, err := source.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)

	restarted := NewPersistentKeeper(service)
	require.Empty(t, restarted.NextValidatorSet(), "fresh keeper starts at the empty default in memory")

	require.NoError(t, restarted.loadForBlock(ctx))
	require.Len(t, restarted.NextValidatorSet(), 1, "restarted node must load the committed election from the store")
	require.Len(t, restarted.ElectionCandidates(), 1)
}

func TestCandidateWithdrawalBeforeDeadline(t *testing.T) {
	k := NewKeeper()
	app := applyCandidate(t, &k, 0x11, 100, 100, 2)

	withdrawn, err := k.WithdrawApplication(types.MsgWithdrawApplication{
		Authority:       prototype.DefaultAuthority,
		OperatorAddress: app.OperatorAddress,
		Height:          80,
	})
	require.NoError(t, err)
	require.Equal(t, types.ApplicationStatusWithdrawn, withdrawn.Status)

	result, err := k.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)
	require.Empty(t, result.NextSet)
}

func TestCandidateWithdrawalAfterDeadlineRejected(t *testing.T) {
	k := NewKeeper()
	app := applyCandidate(t, &k, 0x11, 100, 100, 2)

	_, err := k.WithdrawApplication(types.MsgWithdrawApplication{
		Authority:       prototype.DefaultAuthority,
		OperatorAddress: app.OperatorAddress,
		Height:          82,
	})
	require.ErrorContains(t, err, "deadline")
}

func TestFrozenStakeUnlockTiming(t *testing.T) {
	k := NewKeeper()
	app := applyCandidate(t, &k, 0x11, 100, 100, 2)
	stakes, err := k.FrozenStake(app.OperatorAddress)
	require.NoError(t, err)
	require.Len(t, stakes, 1)
	require.Equal(t, uint64(1002), stakes[0].UnlockHeight)

	_, err = k.ReleaseFrozenStake(app.OperatorAddress, 1001)
	require.ErrorContains(t, err, "still locked")
	released, err := k.ReleaseFrozenStake(app.OperatorAddress, 1002)
	require.NoError(t, err)
	require.True(t, released.Released)
}

func TestDeterministicTieBreakerByAddress(t *testing.T) {
	k := NewKeeper()
	lower := applyCandidate(t, &k, 0x11, 100, 100, 2)
	higher := applyCandidate(t, &k, 0x22, 100, 100, 3)

	result, err := k.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)
	require.Len(t, result.NextSet, 2)
	require.Equal(t, lower.OperatorAddress, result.NextSet[0].OperatorAddress)
	require.Equal(t, higher.OperatorAddress, result.NextSet[1].OperatorAddress)
}

func TestMaxVotingPowerCapEnforced(t *testing.T) {
	k := NewKeeper()
	app := applyCandidate(t, &k, 0x11, 1_000, 1_000, 2)
	gs := k.ExportGenesis()
	gs.State.ValidatorPowerCaps = []types.ValidatorPowerCap{{OperatorAddress: app.OperatorAddress, MaxVotingPower: 123}}
	require.NoError(t, k.InitGenesis(gs))

	result, err := k.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)
	require.Len(t, result.NextSet, 1)
	require.Equal(t, uint64(123), result.NextSet[0].VotingPower)
}

func TestInvalidNextSetRejectedAtGenesis(t *testing.T) {
	gs := DefaultGenesis()
	gs.State.NextValidatorSet = []types.ValidatorPower{{
		OperatorAddress:    testAddress(0x11),
		ConsensusPublicKey: testConsensusKey(0x40),
		VotingPower:        10,
		ValidatorStatus:    validatorregistrytypes.StatusJailed,
	}}
	require.ErrorContains(t, gs.Validate(), "next set")
}

func TestTotalVotingPowerLimitEnforced(t *testing.T) {
	k := NewKeeper()
	gs := k.ExportGenesis()
	gs.Params.MaxValidatorPower = 100
	gs.Params.MaxTotalVotingPower = 150
	require.NoError(t, k.InitGenesis(gs))
	applyCandidate(t, &k, 0x11, 100, 100, 2)
	applyCandidate(t, &k, 0x22, 100, 100, 3)

	result, err := k.CommitElection(types.MsgCommitElection{Authority: prototype.DefaultAuthority, Height: 90})
	require.NoError(t, err)
	require.Len(t, result.NextSet, 1)
}

func applyCandidate(t *testing.T, k *Keeper, fill byte, requestedPower, selfBond, height uint64) types.CandidateApplication {
	t.Helper()
	app, err := k.ApplyForValidatorSet(types.MsgApplyForValidatorSet{
		Authority: prototype.DefaultAuthority,
		Application: types.CandidateApplication{
			OperatorAddress:    testAddress(fill),
			ConsensusPublicKey: testConsensusKey(fill),
			RequestedPower:     requestedPower,
			SelfBond:           selfBond,
			ValidatorStatus:    validatorregistrytypes.StatusCandidate,
		},
		Height: height,
	})
	require.NoError(t, err)
	return app
}

func testAddress(fill byte) string {
	return addressing.FormatAccAddress(sdk.AccAddress(bytesOf(fill)))
}

func bytesOf(fill byte) []byte {
	out := make([]byte, 20)
	for i := range out {
		out[i] = fill
	}
	return out
}

func testConsensusKey(fill byte) string {
	sum := sha256.Sum256(bytesOf(fill))
	return "ed25519:" + hex.EncodeToString(sum[:])
}
