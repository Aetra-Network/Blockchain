package app

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
	emissionstypes "github.com/sovereign-l1/l1/x/emissions/types"
)

// TestFinalizeNativeEconomyEpochDefaultScheduleDoesNotHalt drives the emission
// finalization with the REAL default emission schedule (30,000,000 naet/epoch),
// not the shrunk "golden" params used elsewhere. Before the mint-authority caps
// were sized from the schedule, this path returned an error that halted the
// chain from EndBlock at the first emission epoch. See SEC-CRIT: genesis
// emission vs mint-cap chain halt.
func TestFinalizeNativeEconomyEpochDefaultScheduleDoesNotHalt(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	configureGoldenBurnParams(t, app, ctx)
	require.NoError(t, app.EmissionsKeeper.SetParams(ctx, emissionstypes.DefaultParams()))

	record, err := app.FinalizeNativeEconomyEpoch(ctx, 1, uint32(appparams.DefaultTargetStakeBps))
	require.NoError(t, err)
	// 365e9 * 300 / 10000 / 365 = 30,000,000.
	require.Equal(t, sdkmath.NewInt(30_000_000), record.EmissionAmount.Amount)

	// The mint actually went through under the schedule-derived caps.
	state, err := app.MintAuthorityKeeper.GetState(ctx)
	require.NoError(t, err)
	require.Len(t, state.MintedLifetime, 1)
	require.True(t, state.MintedLifetime[0].Amount.Equal(sdkmath.NewInt(30_000_000)),
		"lifetime minted = %s, want 30000000", state.MintedLifetime[0].Amount)
}

// TestFinalizeNativeEconomyEpochSkipsOnCapExceededWithoutHalting proves the
// fail-soft behaviour: if a (mis-configured) emission would exceed the mint
// cap, the epoch is skipped with an event rather than returning an error that
// would halt the chain, and nothing is committed so no partial state leaks.
func TestFinalizeNativeEconomyEpochSkipsOnCapExceededWithoutHalting(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	configureGoldenBurnParams(t, app, ctx)

	params := emissionstypes.DefaultParams()
	params.CurrentInflationBps = 500
	params.MinAnnualInflationBps = 500
	params.MaxAnnualInflationBps = 500
	params.ConstitutionalMaxInflationBps = 500
	params.EpochsPerYear = 1
	// emission = 100e9 * 500 / 10000 / 1 = 5,000,000,000, far above the cap.
	params.AnnualReferenceSupply = sdk.NewInt64Coin(appparams.BaseDenom, 100_000_000_000)
	require.NoError(t, app.EmissionsKeeper.SetParams(ctx, params))

	record, err := app.FinalizeNativeEconomyEpoch(ctx, 3, params.TargetStakingRatioBps)
	require.NoError(t, err, "cap breach must be skipped, not returned as a chain-halting error")
	require.Equal(t, uint64(0), record.Epoch, "skipped epoch returns the zero record")

	// Nothing committed: the epoch was not recorded, so accounting stays clean.
	_, found, err := app.EmissionsKeeper.GetEmissionEpoch(ctx, 3)
	require.NoError(t, err)
	require.False(t, found, "cap-skipped epoch must not be recorded")
}
