package app

import (
	"bytes"
	"encoding/json"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	burntypes "github.com/sovereign-l1/l1/x/burn/types"
	emissionstypes "github.com/sovereign-l1/l1/x/emissions/types"
	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
	mintauthoritykeeper "github.com/sovereign-l1/l1/x/mint-authority/keeper"
	mintauthoritypb "github.com/sovereign-l1/l1/x/mint-authority/types/mintauthoritypb"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
	treasurytypes "github.com/sovereign-l1/l1/x/treasury/types"
)

func TestEndBlockerDistributesNativeTransactionFees(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	fees := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 20_000_000))

	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, fees))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, fees))
	supplyBefore := app.BankKeeper.GetSupply(ctx, appparams.BaseDenom)
	treasuryBefore := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.TreasuryModuleName), appparams.BaseDenom)
	protectionBefore := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.ProtectionModuleName), appparams.BaseDenom)

	require.NoError(t, app.FeesKeeper.RecordCollectedFees(ctx, fees))
	validatorsBeforeDistribution := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName), appparams.BaseDenom)
	pending, err := app.FeeCollectorKeeper.GetPendingDistribution(ctx)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 10_000_000)), pending.Burn)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 3_000_000)), pending.Treasury)
	require.True(t, pending.Protection.Empty())
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 7_000_000)), pending.Validators)

	_, err = app.EndBlocker(ctx)
	require.NoError(t, err)

	pending, err = app.FeeCollectorKeeper.GetPendingDistribution(ctx)
	require.NoError(t, err)
	require.True(t, pending.Total().Empty())
	require.Equal(t, sdk.NewCoins(), app.BankKeeper.GetAllBalances(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.CollectorModuleName)))
	require.Equal(t, treasuryBefore.Add(sdk.NewInt64Coin(appparams.BaseDenom, 3_000_000)), app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.TreasuryModuleName), appparams.BaseDenom))
	require.Equal(t, protectionBefore, app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.ProtectionModuleName), appparams.BaseDenom))
	require.Equal(t, validatorsBeforeDistribution.Add(sdk.NewInt64Coin(appparams.BaseDenom, 7_000_000)), app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName), appparams.BaseDenom))
	require.Equal(t, supplyBefore.Amount.Sub(sdkmath.NewInt(10_000_000)), app.BankKeeper.GetSupply(ctx, appparams.BaseDenom).Amount)

	history, found, err := app.FeeCollectorKeeper.GetFeeHistory(ctx, 42)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 20_000_000)), history.Collected)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 10_000_000)), history.Burn)
	require.NoError(t, app.FeeCollectorKeeper.AssertModuleAccountingInvariant(ctx))
}

func TestGoldenNativeEconomyLoopCoversFeesEmissionsPoolRewardsAndStorageRent(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	rentPayer := AddTestAddrsWithCoins(t, app, ctx, 1, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1_000)))[0]

	fees := sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 20_000_000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, fees))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, fees))
	supplyBefore := app.BankKeeper.GetSupply(ctx, appparams.BaseDenom)
	treasuryBefore := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.TreasuryModuleName), appparams.BaseDenom)
	protectionBefore := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.ProtectionModuleName), appparams.BaseDenom)
	ecosystemBefore := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.EcosystemGrantsModuleName), appparams.BaseDenom)
	storageReserveBefore := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.StorageRentReserveModuleName), appparams.BaseDenom)

	require.NoError(t, app.FeesKeeper.RecordCollectedFees(ctx, fees))

	validatorsBefore := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName), appparams.BaseDenom)
	_, err := app.EndBlocker(ctx)
	require.NoError(t, err)

	configureGoldenBurnParams(t, app, ctx)
	configureGoldenEmissionParams(t, app, ctx)
	ctx = ctx.WithBlockHeight(43)
	emission, err := app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt64Coin(appparams.BaseDenom, 1_000), emission.EmissionAmount)
	require.Equal(t, sdk.NewInt64Coin(appparams.BaseDenom, 700), emission.ValidatorReward)
	require.Equal(t, sdk.NewInt64Coin(appparams.BaseDenom, 100), emission.Treasury)
	require.Equal(t, sdk.NewInt64Coin(appparams.BaseDenom, 100), emission.ProtectionFund)
	require.Equal(t, sdk.NewInt64Coin(appparams.BaseDenom, 50), emission.Burn)
	require.Equal(t, sdk.NewInt64Coin(appparams.BaseDenom, 50), emission.Ecosystem)
	require.Equal(t, appparams.BaseDenom, emission.RoundingRemainder.Denom)
	require.True(t, emission.RoundingRemainder.Amount.IsZero())

	nextPool, rewardSummary, err := nominatorpooltypes.SyncPoolRewards(nominatorpooltypes.DefaultParams(), nominatorpooltypes.NominatorPool{
		PoolID:            "golden-pool",
		TotalBondedStake:  1_000,
		TotalShares:       1_000,
		PoolCommissionBps: 100,
	}, nominatorpooltypes.MsgSyncPoolRewards{
		Authority:          nominatorpooltypes.DefaultParams().Authority,
		PoolID:             "golden-pool",
		Epoch:              7,
		RewardRateBps:      1_000,
		EmissionsAllocated: uint64(emission.ValidatorReward.Amount.Uint64()),
		FeesAllocated:      7_000_000,
		Height:             43,
		Allocations: []nominatorpooltypes.ValidatorRewardAllocation{{
			Validator:          testAEAddress(0x51),
			PoolAllocatedStake: 1_000,
			ValidatorSelfStake: 500,
			PerformanceBps:     10_000,
			CommissionBps:      500,
		}},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(100), rewardSummary.GrossPoolRewards)
	require.Equal(t, uint64(5), rewardSummary.ValidatorCommission)
	require.Equal(t, uint64(0), rewardSummary.PoolProtocolFee)
	require.Equal(t, uint64(95), rewardSummary.PoolUserRewards)
	require.Equal(t, uint64(50), rewardSummary.ValidatorSelfStakeRewards)
	require.LessOrEqual(t, rewardSummary.PoolUserRewards, rewardSummary.EmissionsAllocated+rewardSummary.FeesAllocated)
	require.Equal(t, uint64(95_000_000), nextPool.RewardIndex)
	require.Equal(t, uint64(5), nextPool.ValidatorCommissionAccrued)
	// Rewards are credited solely to RewardIndex (above); TotalBondedStake stays
	// at principal (1000) and is not double-credited with the 95 user rewards.
	require.Equal(t, uint64(1_000), nextPool.TotalBondedStake)

	allocations, remainder, err := app.FeeCollectorKeeper.CollectAndDistributeProtocolIncomeFromAccount(ctx, rentPayer, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 100)))
	require.NoError(t, err)
	require.True(t, remainder.Empty())
	byBucket := map[string]sdk.Coins{}
	for _, allocation := range allocations {
		byBucket[allocation.Bucket] = allocation.Amount
	}
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 5)), byBucket[feecollectortypes.BucketStorageRentReserve])
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 2)), byBucket[feecollectortypes.BucketBurn])
	require.Equal(t, sdk.NewCoins(), app.BankKeeper.GetAllBalances(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.CollectorModuleName)))

	require.Equal(t, validatorsBefore.Add(sdk.NewInt64Coin(appparams.BaseDenom, 7_000_738)), app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName), appparams.BaseDenom))
	require.Equal(t, treasuryBefore.Add(sdk.NewInt64Coin(appparams.BaseDenom, 3_000_125)), app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.TreasuryModuleName), appparams.BaseDenom))
	require.Equal(t, protectionBefore.Add(sdk.NewInt64Coin(appparams.BaseDenom, 110)), app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.ProtectionModuleName), appparams.BaseDenom))
	require.Equal(t, ecosystemBefore.Add(sdk.NewInt64Coin(appparams.BaseDenom, 62)), app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.EcosystemGrantsModuleName), appparams.BaseDenom))
	require.Equal(t, storageReserveBefore.Add(sdk.NewInt64Coin(appparams.BaseDenom, 5)), app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(feecollectortypes.StorageRentReserveModuleName), appparams.BaseDenom))
	require.Equal(t, supplyBefore.Amount.Sub(sdkmath.NewInt(10_000_000)).Add(sdkmath.NewInt(950)).Sub(sdkmath.NewInt(2)), app.BankKeeper.GetSupply(ctx, appparams.BaseDenom).Amount)

	burned, found, err := app.BurnKeeper.GetBurnedDenomEntry(ctx, burntypes.BaseDenom)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 50)), burned.Amount)
	emissionsGenesis, err := app.EmissionsKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt64Coin(appparams.BaseDenom, 1_000), emissionsGenesis.TotalMintedAccounting)
	require.NoError(t, emissionsGenesis.Validate())
	feeCollectorGenesis, err := app.FeeCollectorKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, feeCollectorGenesis.Validate())
	burnGenesis, err := app.BurnKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, burnGenesis.Validate())
	mintAuthorityGenesis, err := app.MintAuthorityKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, mintAuthorityGenesis.Validate())
	require.NoError(t, app.TreasuryKeeper.SyncIncomingFunds(ctx))
	treasuryGenesis, err := app.TreasuryKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, treasuryGenesis.Validate())
	require.NoError(t, app.FeeCollectorKeeper.AssertModuleAccountingInvariant(ctx))
	require.NoError(t, app.TreasuryKeeper.AssertTreasuryAccountingInvariant(ctx))

	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	treasuryAccounting := treasuryGenesis.Allocations.AccountingBalance()
	if !treasuryAccounting.Empty() {
		require.NoError(t, importedApp.BankKeeper.MintCoins(importedCtx, minttypes.ModuleName, treasuryAccounting))
		require.NoError(t, importedApp.BankKeeper.SendCoinsFromModuleToModule(importedCtx, minttypes.ModuleName, treasurytypes.TreasuryModuleName, treasuryAccounting))
	}
	require.NoError(t, importedApp.FeeCollectorKeeper.InitGenesis(importedCtx, *feeCollectorGenesis))
	require.NoError(t, importedApp.BurnKeeper.InitGenesis(importedCtx, *burnGenesis))
	require.NoError(t, importedApp.EmissionsKeeper.InitGenesis(importedCtx, *emissionsGenesis))
	require.NoError(t, importedApp.MintAuthorityKeeper.InitGenesis(importedCtx, *mintAuthorityGenesis))
	require.NoError(t, importedApp.TreasuryKeeper.InitGenesis(importedCtx, *treasuryGenesis))
	reexportedFees, err := importedApp.FeeCollectorKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, feeCollectorGenesis, reexportedFees)
	reexportedBurn, err := importedApp.BurnKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, burnGenesis, reexportedBurn)
	reexportedEmissions, err := importedApp.EmissionsKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, emissionsGenesis, reexportedEmissions)
	reexportedMintAuthority, err := importedApp.MintAuthorityKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, mintAuthorityGenesis, reexportedMintAuthority)
	reexportedTreasury, err := importedApp.TreasuryKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, treasuryGenesis, reexportedTreasury)
	require.NoError(t, importedApp.FeeCollectorKeeper.AssertModuleAccountingInvariant(importedCtx))
	require.NoError(t, importedApp.TreasuryKeeper.AssertTreasuryAccountingInvariant(importedCtx))
}

func configureGoldenBurnParams(t *testing.T, app *L1App, ctx sdk.Context) {
	t.Helper()
	// DefaultParams() already authorizes authtypes.FeeCollectorName to burn
	// (fee_collector's own EndBlock burn permission is granted by default,
	// see x/burn/types/genesis.go) -- appending it again here would produce a
	// duplicate ProtocolBurnPermissions entry and fail SetParams' validation.
	require.NoError(t, app.BurnKeeper.SetParams(ctx, burntypes.DefaultParams()))
}

func configureGoldenEmissionParams(t *testing.T, app *L1App, ctx sdk.Context) {
	t.Helper()
	params := emissionstypes.DefaultParams()
	params.CurrentInflationBps = 1_000
	params.MinAnnualInflationBps = 1_000
	params.MaxAnnualInflationBps = 1_000
	params.ConstitutionalMaxInflationBps = 1_000
	params.TargetStakingRatioBps = 5_000
	params.ResponsivenessBps = 1
	params.AnnualReferenceSupply = sdk.NewInt64Coin(appparams.BaseDenom, 10_000)
	params.EpochsPerYear = 1
	params.DistributionWeights = emissionstypes.DefaultDistributionWeights()
	require.NoError(t, app.EmissionsKeeper.SetParams(ctx, params))
}

func testAEAddress(fill byte) string {
	return aetraaddress.FormatAccAddress(sdk.AccAddress(bytes.Repeat([]byte{fill}, 20)))
}

func TestEmissionEpochDuplicateRejected(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	configureGoldenBurnParams(t, app, ctx)
	configureGoldenEmissionParams(t, app, ctx)

	_, err := app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.NoError(t, err)

	_, err = app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already finalized")
}

func TestEmissionCapInvariant(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	configureGoldenBurnParams(t, app, ctx)
	configureGoldenEmissionParams(t, app, ctx)

	emission, err := app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.NoError(t, err)

	totalDistributed := emission.ValidatorReward.Amount.
		Add(emission.Treasury.Amount).
		Add(emission.ProtectionFund.Amount).
		Add(emission.Burn.Amount).
		Add(emission.Ecosystem.Amount).
		Add(emission.RoundingRemainder.Amount)
	require.Equal(t, emission.EmissionAmount.Amount, totalDistributed,
		"total distributed rewards must equal total emission amount")
}

// TestEndBlockerFinalizesEmissionInflationAndBurnAtEpochBoundary proves the
// inflation/emission/burn processes actually execute through the real runtime
// path — the EndBlocker hook maybeFinalizeNativeEmissionEpoch — rather than only
// via a direct FinalizeNativeEconomyEpoch call (which the golden loop test
// covers). It confirms an epoch is finalized exactly at a reward-epoch boundary,
// that total supply inflates by the minted emission net of its burn, that the
// burn is accounted, that the treasury is credited, and that re-finalizing the
// same height does not double-mint.
func TestEndBlockerFinalizesEmissionInflationAndBurnAtEpochBoundary(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	configureGoldenBurnParams(t, app, ctx)
	configureGoldenEmissionParams(t, app, ctx)

	interval := int64(nominatorpooltypes.DefaultRewardEpochDurationBlocks)
	require.Greater(t, interval, int64(1), "reward epoch must span multiple blocks")

	burnedBase := func(c sdk.Context) sdkmath.Int {
		entry, found, err := app.BurnKeeper.GetBurnedDenomEntry(c, burntypes.BaseDenom)
		require.NoError(t, err)
		if !found {
			return sdkmath.ZeroInt()
		}
		return entry.Amount.AmountOf(appparams.BaseDenom)
	}
	treasuryBalance := func(c sdk.Context) sdkmath.Int {
		return app.BankKeeper.GetBalance(c, app.AccountKeeper.GetModuleAddress(feecollectortypes.TreasuryModuleName), appparams.BaseDenom).Amount
	}

	// Off a boundary (height 1, interval > 1), EndBlocker must not emit.
	offBoundary := ctx.WithBlockHeight(1)
	supplyOffBefore := app.BankKeeper.GetSupply(offBoundary, appparams.BaseDenom).Amount
	_, err := app.EndBlocker(offBoundary)
	require.NoError(t, err)
	require.Equal(t, supplyOffBefore, app.BankKeeper.GetSupply(offBoundary, appparams.BaseDenom).Amount,
		"no emission may occur off a reward-epoch boundary")
	_, found, err := app.EmissionsKeeper.GetEmissionEpoch(offBoundary, 1)
	require.NoError(t, err)
	require.False(t, found, "no epoch may be finalized off a boundary")

	// At the boundary, the EndBlocker hook finalizes emission epoch 1.
	boundary := ctx.WithBlockHeight(interval)
	supplyBefore := app.BankKeeper.GetSupply(boundary, appparams.BaseDenom).Amount
	burnBefore := burnedBase(boundary)
	treasuryBefore := treasuryBalance(boundary)

	_, err = app.EndBlocker(boundary)
	require.NoError(t, err)

	record, found, err := app.EmissionsKeeper.GetEmissionEpoch(boundary, 1)
	require.NoError(t, err)
	require.True(t, found, "EndBlocker must finalize emission epoch 1 at the boundary")
	require.True(t, record.EmissionAmount.IsPositive(), "epoch must mint a positive emission")

	// Inflation: supply grew by the minted emission net of its burn portion.
	supplyAfter := app.BankKeeper.GetSupply(boundary, appparams.BaseDenom).Amount
	require.Equal(t, supplyBefore.Add(record.EmissionAmount.Amount).Sub(record.Burn.Amount), supplyAfter,
		"supply must inflate by emission minus emission-burn")

	// Burn: the emission burn was accounted in the burn module.
	require.Equal(t, burnBefore.Add(record.Burn.Amount), burnedBase(boundary),
		"burn ledger must record the emission burn")

	// Economics: the treasury received at least its emission share.
	require.True(t, treasuryBalance(boundary).GTE(treasuryBefore.Add(record.Treasury.Amount)),
		"treasury must receive its emission allocation")

	// Idempotency: re-finalizing the same height must not double-mint.
	_, err = app.EndBlocker(boundary)
	require.NoError(t, err)
	require.Equal(t, supplyAfter, app.BankKeeper.GetSupply(boundary, appparams.BaseDenom).Amount,
		"re-running EndBlocker at the same boundary must not double-mint")
}

func TestJailedValidatorProducesZeroPoolRewards(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)
	configureGoldenBurnParams(t, app, ctx)
	configureGoldenEmissionParams(t, app, ctx)

	emission, err := app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.NoError(t, err)

	_, rewardSummary, err := nominatorpooltypes.SyncPoolRewards(nominatorpooltypes.DefaultParams(), nominatorpooltypes.NominatorPool{
		PoolID:            "jail-test-pool",
		TotalBondedStake:  1_000,
		TotalShares:       1_000,
		PoolCommissionBps: 100,
	}, nominatorpooltypes.MsgSyncPoolRewards{
		Authority:          nominatorpooltypes.DefaultParams().Authority,
		PoolID:             "jail-test-pool",
		Epoch:              7,
		RewardRateBps:      1_000,
		EmissionsAllocated: uint64(emission.ValidatorReward.Amount.Uint64()),
		FeesAllocated:      0,
		Height:             42,
		Allocations: []nominatorpooltypes.ValidatorRewardAllocation{{
			Validator:          testAEAddress(0x51),
			PoolAllocatedStake: 1_000,
			ValidatorSelfStake: 500,
			PerformanceBps:     10_000,
			CommissionBps:      500,
			Jailed:             true,
		}},
	})
	require.NoError(t, err)

	require.Equal(t, uint64(0), rewardSummary.GrossPoolRewards)
	require.Equal(t, uint64(0), rewardSummary.ValidatorCommission)
	require.Equal(t, uint64(0), rewardSummary.PoolProtocolFee)
	require.Equal(t, uint64(0), rewardSummary.PoolUserRewards)
	require.Equal(t, uint64(0), rewardSummary.ValidatorSelfStakeRewards)
	require.Equal(t, uint64(0), rewardSummary.ValidatorGrossIncome)
}

func TestMaybeFinalizeNativeEmissionEpochAtEpochBoundary(t *testing.T) {
	app := Setup(t, false)

	epochBoundary := int64(nominatorpooltypes.DefaultRewardEpochDurationBlocks)
	ctx := app.NewContext(false).WithBlockHeight(epochBoundary)
	configureGoldenBurnParams(t, app, ctx)
	configureGoldenEmissionParams(t, app, ctx)

	err := app.maybeFinalizeNativeEmissionEpoch(ctx)
	require.NoError(t, err)

	epoch1, found, err := app.EmissionsKeeper.GetEmissionEpoch(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(1), epoch1.Epoch)

	err = app.maybeFinalizeNativeEmissionEpoch(ctx)
	require.NoError(t, err)
}

// TestEndBlockerBurnsFromFeeCollectorOnFreshChainDefaults reproduces, against
// the real ABCI EndBlocker entrypoint (not just FinalizeNativeEconomyEpoch
// directly), a chain-halt that a freshly launched chain running default
// genesis params would hit deterministically at its first emission epoch:
// native-emission distribution burns a portion of every epoch's emission
// from authtypes.FeeCollectorName (x/emissions' default distribution
// weights allocate a nonzero burn-bucket share), and an EndBlocker that
// returns a non-nil error halts the chain. Deliberately does NOT call
// configureGoldenBurnParams -- the whole point is that burn must work from
// x/burn's own DefaultParams() alone, with no test-only or operator-applied
// permission grant required.
func TestEndBlockerBurnsFromFeeCollectorOnFreshChainDefaults(t *testing.T) {
	app := Setup(t, false)

	epochBoundary := int64(nominatorpooltypes.DefaultRewardEpochDurationBlocks)
	ctx := app.NewContext(false).WithBlockHeight(epochBoundary)
	configureGoldenEmissionParams(t, app, ctx)

	_, err := app.EndBlocker(ctx)
	require.NoError(t, err)

	entry, found, err := app.BurnKeeper.GetBurnedEpochEntry(ctx, 1)
	require.NoError(t, err)
	require.True(t, found, "epoch 1's native-emission burn should have been recorded, not silently skipped")
	require.True(t, entry.Amount.IsAllPositive(), "burned amount should be positive given DefaultDistributionWeights' nonzero burn-bucket share")
}

func TestMintAuthorityRejectsNonCanonicalBaseDenomUpdateAndFinalizeStillWorks(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)

	state, err := app.MintAuthorityKeeper.GetState(ctx)
	require.NoError(t, err)

	badState := state
	badState.Params.BaseDenom = "uatom"
	bz, err := json.Marshal(badState)
	require.NoError(t, err)

	msgServer := mintauthoritykeeper.NewMsgServerImpl(app.MintAuthorityKeeper)
	_, err = msgServer.UpdateMintAuthorityParams(ctx, &mintauthoritypb.MsgUpdateMintAuthorityParams{
		Authority: state.Params.Authority,
		StateJson: string(bz),
	})
	require.ErrorContains(t, err, "canonical")

	configureGoldenBurnParams(t, app, ctx)
	configureGoldenEmissionParams(t, app, ctx)
	ctx = ctx.WithBlockHeight(43)

	emission, err := app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.NoError(t, err)
	require.Equal(t, appparams.BaseDenom, emission.EmissionAmount.Denom)
	require.Equal(t, appparams.BaseDenom, emission.ValidatorReward.Denom)

	currentState, err := app.MintAuthorityKeeper.GetState(ctx)
	require.NoError(t, err)
	require.Equal(t, appparams.BaseDenom, currentState.Params.BaseDenom)
}
