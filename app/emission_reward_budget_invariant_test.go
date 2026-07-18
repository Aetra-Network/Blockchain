package app

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	emissionstypes "github.com/sovereign-l1/l1/x/emissions/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
)

func epochRewardBudget(anchor sdkmath.Int, params emissionstypes.Params) sdkmath.Int {
	return anchor.
		Mul(sdkmath.NewInt(int64(params.MaxAnnualInflationBps))).
		Quo(sdkmath.NewInt(10_000)).
		Quo(sdkmath.NewIntFromUint64(params.EpochsPerYear))
}

// The reward budget must be sized from the live circulating-supply anchor that
// emission itself uses, not from the AnnualReferenceSupply param, which is
// still the 365 AET bootstrap placeholder on a real chain.
func TestNativeInvariantInputRewardBudgetUsesLiveAnchorNotStaleParam(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)

	emParams, err := app.EmissionsKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Positive(t, emParams.MaxAnnualInflationBps)
	require.Positive(t, emParams.EpochsPerYear)

	anchor, err := app.emissionReferenceSupply(ctx, emParams.BaseDenom)
	require.NoError(t, err)
	require.NotEqual(t, emParams.AnnualReferenceSupply.Amount.String(), anchor.String(),
		"test premise: the live anchor must differ from the param for this to prove anything")

	in, err := app.nativeInvariantInput(ctx)
	require.NoError(t, err)
	require.Equal(t, epochRewardBudget(anchor, emParams).Uint64(), in.RewardBudget)

	staleBudget := epochRewardBudget(emParams.AnnualReferenceSupply.Amount, emParams)
	require.Greater(t, in.RewardBudget, staleBudget.Uint64(),
		"budget still sized from the stale AnnualReferenceSupply param")
}

// RewardsAccrued must carry the real per-epoch staking reward. It defaulted to
// zero for the lifetime of this invariant, which made the check vacuous: it
// could never fire regardless of what the chain actually paid out.
func TestNativeInvariantInputPopulatesRewardsAccruedFromFinalizedEpoch(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(43)

	in, err := app.nativeInvariantInput(ctx)
	require.NoError(t, err)
	require.Zero(t, in.RewardsAccrued, "no epoch finalized yet")

	record, err := app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.NoError(t, err)
	require.True(t, record.ValidatorReward.Amount.IsPositive(),
		"fixture must emit a real validator reward for this test to mean anything")

	in, err = app.nativeInvariantInput(ctx)
	require.NoError(t, err)
	require.Equal(t, record.ValidatorReward.Amount.Uint64(), in.RewardsAccrued)

	// Honest rewards pass against an honestly sized budget...
	require.NoError(t, nativeaccounttypes.RunNativeAccountInvariant(
		nativeaccounttypes.InvariantRewardsCannotExceedAllocation, in))

	// ...and the check is live: one naet over budget fails it.
	overrun := in
	overrun.RewardsAccrued = in.RewardBudget + 1
	require.ErrorContains(t, nativeaccounttypes.RunNativeAccountInvariant(
		nativeaccounttypes.InvariantRewardsCannotExceedAllocation, overrun), "exceed budget")
}

// The two defects masked each other: real rewards measured against the stale
// param budget breach it outright. Populating RewardsAccrued without also
// re-anchoring the budget would have failed the check on an honest chain.
func TestNativeInvariantStaleBudgetWouldRejectHonestRewards(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(43)

	emParams, err := app.EmissionsKeeper.GetParams(ctx)
	require.NoError(t, err)

	_, err = app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.NoError(t, err)

	in, err := app.nativeInvariantInput(ctx)
	require.NoError(t, err)

	staleBudget := epochRewardBudget(emParams.AnnualReferenceSupply.Amount, emParams)
	require.Greater(t, in.RewardsAccrued, staleBudget.Uint64(),
		"premise: honest rewards exceed the stale-param budget")

	stale := in
	stale.RewardBudget = staleBudget.Uint64()
	require.ErrorContains(t, nativeaccounttypes.RunNativeAccountInvariant(
		nativeaccounttypes.InvariantRewardsCannotExceedAllocation, stale), "exceed budget")
}

// The budget math must stay in sdkmath.Int. In uint64 the pre-divide product
// anchor*MaxAnnualInflationBps wraps above ~23.1M AET at 800bps -- well inside
// this network's real supply -- yielding a plausible-looking budget many times
// too small, which would fire the check against honest rewards.
func TestNativeInvariantInputRewardBudgetDoesNotWrapAtRealSupply(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)

	emParams, err := app.EmissionsKeeper.GetParams(ctx)
	require.NoError(t, err)

	naetPerAET := sdkmath.NewInt(1_000_000_000)
	extra := sdk.NewCoins(sdk.NewCoin(emParams.BaseDenom, sdkmath.NewInt(80_000_000).Mul(naetPerAET)))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, extra))

	anchor, err := app.emissionReferenceSupply(ctx, emParams.BaseDenom)
	require.NoError(t, err)

	// Confirm this fixture actually crosses the wrap threshold, otherwise the
	// test would pass without exercising the overflow at all.
	require.True(t, anchor.Uint64() > ^uint64(0)/uint64(emParams.MaxAnnualInflationBps),
		"fixture must exceed the uint64 wrap threshold to prove anything")

	in, err := app.nativeInvariantInput(ctx)
	require.NoError(t, err)
	require.Equal(t, epochRewardBudget(anchor, emParams).Uint64(), in.RewardBudget)

	wrapped := (anchor.Uint64() * uint64(emParams.MaxAnnualInflationBps) / 10_000) / emParams.EpochsPerYear
	require.Greater(t, in.RewardBudget, wrapped, "budget computed in wrapping uint64 math")
}
