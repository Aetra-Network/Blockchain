package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
	emissionstypes "github.com/sovereign-l1/l1/x/emissions/types"
	mintauthoritytypes "github.com/sovereign-l1/l1/x/mint-authority/types"
)

// TestFinalizeNativeEconomyEpochDefaultScheduleDoesNotHalt drives the emission
// finalization with the REAL default emission schedule and params, not the
// shrunk "golden" params used elsewhere. Before the mint-authority caps were
// sized from the schedule, this path returned an error that halted the chain
// from EndBlock at the first emission epoch. See SEC-CRIT: genesis emission vs
// mint-cap chain halt.
//
// Emission is anchored to the chain's real circulating supply (see #31:
// AnnualReferenceSupply is a genesis bootstrap constant, not the live anchor),
// so the expected amount is computed here from that same anchor rather than
// hardcoded -- a hardcoded figure would silently stop meaning anything the
// moment the test genesis supply changes.
func TestFinalizeNativeEconomyEpochDefaultScheduleDoesNotHalt(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	configureGoldenBurnParams(t, app, ctx)
	emParams := emissionstypes.DefaultParams()
	require.NoError(t, app.EmissionsKeeper.SetParams(ctx, emParams))

	anchor, err := app.emissionReferenceSupply(ctx, emParams.BaseDenom)
	require.NoError(t, err)
	wantEmission, err := emissionstypes.ComputeEpochEmissionWithSupply(
		emParams, 1, uint64(appparams.DefaultTargetStakeBps), ctx.BlockHeight(), anchor)
	require.NoError(t, err)
	require.True(t, wantEmission.EmissionAmount.Amount.IsPositive(),
		"test anchor must be large enough to produce a non-zero emission")

	record, err := app.FinalizeNativeEconomyEpoch(ctx, 1, uint32(appparams.DefaultTargetStakeBps))
	require.NoError(t, err)
	require.Equal(t, wantEmission.EmissionAmount.Amount, record.EmissionAmount.Amount)

	// The mint actually went through under the schedule-derived caps.
	state, err := app.MintAuthorityKeeper.GetState(ctx)
	require.NoError(t, err)
	require.Len(t, state.MintedLifetime, 1)
	require.True(t, state.MintedLifetime[0].Amount.Equal(record.EmissionAmount.Amount),
		"lifetime minted = %s, want %s", state.MintedLifetime[0].Amount, record.EmissionAmount.Amount)
}

// TestFinalizeNativeEconomyEpochSkipsOnCapExceededWithoutHalting proves the
// fail-soft behaviour: if minting would exceed the mint-authority's safety
// ceiling, the epoch is skipped with an event rather than returning an error
// that would halt the chain, and nothing is committed so no partial state
// leaks.
//
// The per-epoch cap can no longer be tripped by a mismatched reference-supply
// param (#31's fix: emission and its cap are now sized from the same live
// anchor, at the same rate/cadence, with headroom -- so per-epoch emission is
// structurally bounded below its own cap). The mechanism that remains
// reachable, and the one this now exercises, is the LIFETIME cap: a chain
// that has already minted up to its lifetime ceiling (e.g. after
// governance shrinks ConstitutionalMaxInflationBps following a long period at
// a higher historic rate) must still skip further emission without halting.
func TestFinalizeNativeEconomyEpochSkipsOnCapExceededWithoutHalting(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	configureGoldenBurnParams(t, app, ctx)

	emParams := emissionstypes.DefaultParams()
	require.NoError(t, app.EmissionsKeeper.SetParams(ctx, emParams))

	anchor, err := app.emissionReferenceSupply(ctx, emParams.BaseDenom)
	require.NoError(t, err)
	epochCap := appparams.MintAuthorityEpochCapNaetFor(
		anchor, int64(emParams.ConstitutionalMaxInflationBps), int64(emParams.EpochsPerYear))
	lifetimeCap := appparams.MintAuthorityLifetimeCapNaetFor(
		anchor, int64(emParams.ConstitutionalMaxInflationBps), int64(emParams.EpochsPerYear))
	require.True(t, lifetimeCap.IsPositive())

	// Seed the mint-authority state as if the chain had already minted its
	// entire lifetime allowance, at the cap FinalizeNativeEconomyEpoch will
	// itself recompute from the same anchor next call (refreshMintCapsForSupply
	// runs before the mint attempt, so this must match or the state fails
	// Validate before the cap check is ever reached).
	// NormalizeMintAuthorityState fills in the content hash for the lifetime
	// counter automatically.
	mintState, err := app.MintAuthorityKeeper.GetState(ctx)
	require.NoError(t, err)
	mintState.MintedLifetime = []mintauthoritytypes.MintedLifetime{
		{Denom: emParams.BaseDenom, Amount: lifetimeCap},
	}
	mintState.Caps = []mintauthoritytypes.MintCap{
		{Denom: emParams.BaseDenom, EpochCap: epochCap, LifetimeCap: lifetimeCap},
	}
	require.NoError(t, app.MintAuthorityKeeper.SetState(ctx, mintState))

	record, err := app.FinalizeNativeEconomyEpoch(ctx, 3, emParams.TargetStakingRatioBps)
	require.NoError(t, err, "cap breach must be skipped, not returned as a chain-halting error")
	require.Equal(t, uint64(0), record.Epoch, "skipped epoch returns the zero record")

	// Nothing committed: the epoch was not recorded, so accounting stays clean.
	_, found, err := app.EmissionsKeeper.GetEmissionEpoch(ctx, 3)
	require.NoError(t, err)
	require.False(t, found, "cap-skipped epoch must not be recorded")

	// The lifetime counter itself must be unchanged -- the skip must not
	// partially commit.
	mintStateAfter, err := app.MintAuthorityKeeper.GetState(ctx)
	require.NoError(t, err)
	require.True(t, mintStateAfter.MintedLifetime[0].Amount.Equal(lifetimeCap))
}
