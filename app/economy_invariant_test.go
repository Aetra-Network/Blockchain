package app

import (
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	"github.com/sovereign-l1/l1/app/params"
	economicstypes "github.com/sovereign-l1/l1/x/aetra-economics/types"
	burntypes "github.com/sovereign-l1/l1/x/burn/types"
	emissionstypes "github.com/sovereign-l1/l1/x/emissions/types"
	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
	storagerenttypes "github.com/sovereign-l1/l1/x/storage-rent/types"
	treasurytypes "github.com/sovereign-l1/l1/x/treasury/types"
)

func TestEmissionCapInvariantFailsOnExcessMinting(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	emParams := emissionstypes.DefaultParams()
	require.NoError(t, app.EmissionsKeeper.SetParams(ctx, emParams))

	maxMintable := emParams.AnnualReferenceSupply.Amount.Mul(sdkmath.NewInt(int64(emParams.ConstitutionalMaxInflationBps))).Quo(sdkmath.NewInt(10_000))
	excess := maxMintable.Add(sdkmath.NewInt(1))
	excessCoin := sdk.NewCoin(emParams.BaseDenom, excess)
	fakeGenesis := emissionstypes.DefaultGenesisState()
	fakeGenesis.TotalMintedAccounting = excessCoin
	fakeGenesis.EpochHistory = []emissionstypes.EmissionEpoch{
		{
			Epoch:			1,
			EmissionAmount:		excessCoin,
			ValidatorReward:	excessCoin,
			Treasury:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			ProtectionFund:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			Burn:			sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			Ecosystem:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			RoundingRemainder:	sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
		},
	}
	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.EmissionsKeeper.InitGenesis(importedCtx, *fakeGenesis))
	err := importedApp.assertEmissionCapInvariant(importedCtx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds constitutional max")
}

// TestEmissionCapInvariantAllowsSecondYearAccumulationWithoutFalsePositive
// covers security audit FINDING-003: totalMinted is a monotonic lifetime
// counter, so once the chain is into its second emission year (epoch >
// EpochsPerYear) a lifetime total that is legitimately a little over one
// year's worth of max emission must NOT trip the invariant. Before the fix,
// comparing the lifetime total directly against the flat one-year maxMintable
// would have permanently, falsely failed here.
func TestEmissionCapInvariantAllowsSecondYearAccumulationWithoutFalsePositive(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	emParams := emissionstypes.DefaultParams()
	require.NoError(t, app.EmissionsKeeper.SetParams(ctx, emParams))

	maxMintable := emParams.AnnualReferenceSupply.Amount.Mul(sdkmath.NewInt(int64(emParams.ConstitutionalMaxInflationBps))).Quo(sdkmath.NewInt(10_000))
	// One unit more than a single year's worth: legitimate once the chain has
	// rolled a little into emission year two.
	secondYearTotal := maxMintable.Add(sdkmath.NewInt(1))
	secondYearCoin := sdk.NewCoin(emParams.BaseDenom, secondYearTotal)

	fakeGenesis := emissionstypes.DefaultGenesisState()
	fakeGenesis.Params = emParams
	fakeGenesis.TotalMintedAccounting = secondYearCoin
	// Epoch 400 > EpochsPerYear (365): the chain is into its second emission
	// year, so the lifetime cap must scale to 2x maxMintable.
	fakeGenesis.EpochHistory = []emissionstypes.EmissionEpoch{
		{
			Epoch:			400,
			EmissionAmount:		secondYearCoin,
			ValidatorReward:	secondYearCoin,
			Treasury:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			ProtectionFund:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			Burn:			sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			Ecosystem:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			RoundingRemainder:	sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
		},
	}
	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.EmissionsKeeper.InitGenesis(importedCtx, *fakeGenesis))
	require.NoError(t, importedApp.assertEmissionCapInvariant(importedCtx),
		"lifetime total one unit over a single year's cap must not false-fire once the chain is into year two")
}

// TestEmissionCapInvariantStillFailsWhenExceedingMultiYearLifetimeCap proves
// the FINDING-003 fix is not merely "always pass": even with the lifetime
// scope applied, a total that exceeds the correctly-scoped multi-year cap
// must still fail.
func TestEmissionCapInvariantStillFailsWhenExceedingMultiYearLifetimeCap(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	emParams := emissionstypes.DefaultParams()
	require.NoError(t, app.EmissionsKeeper.SetParams(ctx, emParams))

	maxMintable := emParams.AnnualReferenceSupply.Amount.Mul(sdkmath.NewInt(int64(emParams.ConstitutionalMaxInflationBps))).Quo(sdkmath.NewInt(10_000))
	// Epoch 400 allows up to 2x maxMintable (year two, rounded up); one unit
	// over that must still fail.
	excess := maxMintable.MulRaw(2).Add(sdkmath.NewInt(1))
	excessCoin := sdk.NewCoin(emParams.BaseDenom, excess)

	fakeGenesis := emissionstypes.DefaultGenesisState()
	fakeGenesis.Params = emParams
	fakeGenesis.TotalMintedAccounting = excessCoin
	fakeGenesis.EpochHistory = []emissionstypes.EmissionEpoch{
		{
			Epoch:			400,
			EmissionAmount:		excessCoin,
			ValidatorReward:	excessCoin,
			Treasury:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			ProtectionFund:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			Burn:			sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			Ecosystem:		sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
			RoundingRemainder:	sdk.NewCoin(emParams.BaseDenom, sdkmath.ZeroInt()),
		},
	}
	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.EmissionsKeeper.InitGenesis(importedCtx, *fakeGenesis))
	err := importedApp.assertEmissionCapInvariant(importedCtx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds constitutional max")
}

// TestEconomicsInvariantsReadLiveStateNotStaleGenesisSnapshot covers security
// audit FINDING-005: for the persistent (real-app) aetra-economics keeper,
// ExportGenesis() (no ctx) returns the in-memory snapshot frozen at
// genesis-import time, while live writes (ApplyEpoch, SetParams) only ever
// reach the ctx-scoped KV store. Before the fix, the economics invariants
// read via ExportGenesis() and so always validated an empty RewardHistory
// no-op instead of live state.
func TestEconomicsInvariantsReadLiveStateNotStaleGenesisSnapshot(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	liveBefore, err := app.AetraEconomicsKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Empty(t, liveBefore.State.RewardHistory, "genesis-default live state starts with no reward history")

	input := economicstypes.EpochEconomicsInput{
		Epoch:		1,
		TotalSupply:	1_000_000_000,
		BondedTokens:	600_000_000,
		FeesCollected:	10_000_000,
	}
	_, err = app.AetraEconomicsKeeper.ApplyEpoch(ctx, input)
	require.NoError(t, err)

	// The persistent keeper's in-memory k.state is frozen at genesis-import
	// time (see the SEC-MED note in x/aetra-economics/keeper/keeper.go): the
	// context-free ExportGenesis() must NOT observe the live ApplyEpoch
	// write. This is the exact staleness FINDING-005 describes.
	staleAfter, err := app.AetraEconomicsKeeper.ExportGenesis()
	require.NoError(t, err)
	require.Empty(t, staleAfter.State.RewardHistory, "ExportGenesis() must stay frozen at the genesis snapshot for the persistent keeper")

	// The live, ctx-based read must observe it.
	liveAfter, err := app.AetraEconomicsKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.Len(t, liveAfter.State.RewardHistory, 1)
	require.Equal(t, uint64(1), liveAfter.State.RewardHistory[0].Epoch)

	// Both invariants must validate the live epoch history (post-fix), not
	// the permanently-empty stale genesis snapshot: this proves they are
	// wired to ExportGenesisState(ctx), not ExportGenesis().
	require.NoError(t, app.assertEconomicsFeeSplitInvariant(ctx))
	require.NoError(t, app.assertEconomicsAccountingInvariant(ctx))
}

// TestEndBlockerPeriodicInvariantSweepEventsOnFailureWithoutHalting covers
// security audit FINDING-002: the critical invariant registry must actually
// execute from a real consensus entry point (EndBlock), and a violation must
// be surfaced (logged + evented) without halting the chain.
func TestEndBlockerPeriodicInvariantSweepEventsOnFailureWithoutHalting(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(int64(criticalInvariantCheckInterval))
	require.Zero(t, uint64(ctx.BlockHeight())%criticalInvariantCheckInterval, "sanity: height must land on the sweep interval")

	// Seed a deliberately broken invariant the same way
	// TestRentReserveBalanceInvariantFailsOnAlertState does, so the sweep has
	// something real to catch.
	gs, err := app.StorageRentKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	gs.State.SystemReserve = storagerenttypes.SystemRentReserve{
		AvailableFunds:				0,
		ProjectedRentPerBlock:			100,
		WarningRunwayBlocks:			100,
		CriticalRunwayBlocks:			10,
		RequiredTopUp:				1_000,
		FeeCollectorBalance:			0,
		TreasuryBalance:			0,
		GovernanceConfiguredPayerBalance:	0,
		ProtocolCriticalExecutable:		false,
	}
	require.NoError(t, app.StorageRentKeeper.InitGenesisState(ctx, gs))
	require.Error(t, app.assertRentReserveBalanceInvariant(ctx), "sanity: the seeded state must actually violate the invariant")

	_, err = app.EndBlocker(ctx)
	require.NoError(t, err, "a broken invariant must never be returned as an EndBlock error")

	var found bool
	for _, event := range ctx.EventManager().Events() {
		if event.Type != EventTypeCriticalInvariantViolated {
			continue
		}
		for _, attr := range event.Attributes {
			if attr.Key == "invariant" && strings.Contains(attr.Value, AppInvariantRentReserveBalance) {
				found = true
			}
		}
	}
	require.True(t, found, "expected a critical_invariant_violated event for the seeded storage-rent alert")
}

// TestEndBlockerPeriodicInvariantSweepSkipsOffInterval proves the throttling
// half of FINDING-002's remediation: the sweep must not run on every block,
// only at the configured interval, so it does not add unconditional
// per-block overhead.
func TestEndBlockerPeriodicInvariantSweepSkipsOffInterval(t *testing.T) {
	app := Setup(t, false)
	offInterval := int64(criticalInvariantCheckInterval) + 1
	ctx := app.NewContext(false).WithBlockHeight(offInterval)
	require.NotZero(t, uint64(ctx.BlockHeight())%criticalInvariantCheckInterval, "sanity: height must NOT land on the sweep interval")
	require.NotZero(t, uint64(ctx.BlockHeight())%uint64(nominatorpooltypes.DefaultRewardEpochDurationBlocks), "sanity: height must also not land on the emission-epoch interval")

	gs, err := app.StorageRentKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	gs.State.SystemReserve = storagerenttypes.SystemRentReserve{
		AvailableFunds:				0,
		ProjectedRentPerBlock:			100,
		WarningRunwayBlocks:			100,
		CriticalRunwayBlocks:			10,
		RequiredTopUp:				1_000,
		FeeCollectorBalance:			0,
		TreasuryBalance:			0,
		GovernanceConfiguredPayerBalance:	0,
		ProtocolCriticalExecutable:		false,
	}
	require.NoError(t, app.StorageRentKeeper.InitGenesisState(ctx, gs))

	_, err = app.EndBlocker(ctx)
	require.NoError(t, err)

	for _, event := range ctx.EventManager().Events() {
		require.NotEqual(t, EventTypeCriticalInvariantViolated, event.Type, "sweep must not run off the interval boundary")
	}
}

func TestBurnAccountingInvariantFailsOnMismatch(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	configureGoldenBurnParams(t, app, ctx)

	require.NoError(t, app.assertBurnAccountingInvariant(ctx))

	gs, err := app.BurnKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	gs.BurnedByDenom = []burntypes.BurnedByDenomEntry{
		{Denom: params.BaseDenom, Amount: sdk.NewCoins(sdk.NewInt64Coin(params.BaseDenom, 999_999))},
	}
	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.BurnKeeper.InitGenesis(importedCtx, *gs))
	err = importedApp.assertBurnAccountingInvariant(importedCtx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "burn accounting mismatch")
}

func TestTreasuryAccountingInvariantFailsOnMismatch(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	require.NoError(t, app.assertTreasuryAccountingInvariant(ctx))

	gs, err := app.TreasuryKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	gs.Allocations = treasurytypes.TreasuryAllocations{
		ReserveBalance: sdk.NewCoins(sdk.NewInt64Coin(params.BaseDenom, 1)),
	}
	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.TreasuryKeeper.InitGenesis(importedCtx, *gs))
	err = importedApp.assertTreasuryAccountingInvariant(importedCtx)
	require.Error(t, err)
}

func TestFeeCollectorAccountingInvariantFailsOnMismatch(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	require.NoError(t, app.assertFeeCollectorAccountingInvariant(ctx))

	gs, err := app.FeeCollectorKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	gs.Balances = feecollectortypes.FeeBalances{
		GasFees:	sdk.NewCoins(sdk.NewInt64Coin(params.BaseDenom, 999)),
		TotalCollected:	sdk.NewCoins(sdk.NewInt64Coin(params.BaseDenom, 999)),
	}
	gs.PendingDistribution = feecollectortypes.PendingDistribution{
		Treasury: sdk.NewCoins(sdk.NewInt64Coin(params.BaseDenom, 999)),
	}
	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.FeeCollectorKeeper.InitGenesis(importedCtx, *gs))
	err = importedApp.assertFeeCollectorAccountingInvariant(importedCtx)
	require.Error(t, err)
}

func TestRentReserveBalanceInvariantFailsOnAlertState(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	require.NoError(t, app.assertRentReserveBalanceInvariant(ctx))

	gs, err := app.StorageRentKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	gs.State.SystemReserve = storagerenttypes.SystemRentReserve{
		AvailableFunds:				0,
		ProjectedRentPerBlock:			100,
		WarningRunwayBlocks:			100,
		CriticalRunwayBlocks:			10,
		RequiredTopUp:				1_000,
		FeeCollectorBalance:			0,
		TreasuryBalance:			0,
		GovernanceConfiguredPayerBalance:	0,
		ProtocolCriticalExecutable:		false,
	}
	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.StorageRentKeeper.InitGenesisState(importedCtx, gs))
	err = importedApp.assertRentReserveBalanceInvariant(importedCtx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "storage rent system reserve is in invariant alert state")
}

func TestFeesModuleGenesisRoundTrip(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	original, err := app.FeesKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, original.Validate())

	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.FeesKeeper.InitGenesis(importedCtx, *original))

	reexported, err := importedApp.FeesKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, original, reexported)
}

func TestFeeCollectorModuleGenesisRoundTrip(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	original, err := app.FeeCollectorKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, original.Validate())

	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.FeeCollectorKeeper.InitGenesis(importedCtx, *original))

	reexported, err := importedApp.FeeCollectorKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, original, reexported)
}

func TestEmissionsModuleGenesisRoundTrip(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	original, err := app.EmissionsKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, original.Validate())

	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.EmissionsKeeper.InitGenesis(importedCtx, *original))

	reexported, err := importedApp.EmissionsKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, original, reexported)
}

func TestBurnModuleGenesisRoundTrip(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	original, err := app.BurnKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, original.Validate())

	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.BurnKeeper.InitGenesis(importedCtx, *original))

	reexported, err := importedApp.BurnKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, original, reexported)
}

func TestTreasuryModuleGenesisRoundTrip(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	require.NoError(t, app.TreasuryKeeper.SyncIncomingFunds(ctx))

	original, err := app.TreasuryKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, original.Validate())

	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	if err := initTreasuryBankBalance(importedCtx, importedApp, original); err != nil {
		require.NoError(t, err)
	}
	require.NoError(t, importedApp.TreasuryKeeper.InitGenesis(importedCtx, *original))

	reexported, err := importedApp.TreasuryKeeper.ExportGenesis(importedCtx)
	require.NoError(t, err)
	require.Equal(t, original, reexported)
}

func TestStorageRentModuleGenesisRoundTrip(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	original, err := app.StorageRentKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	require.NoError(t, original.Validate())

	importedApp := Setup(t, false)
	importedCtx := importedApp.NewContext(false)
	require.NoError(t, importedApp.StorageRentKeeper.InitGenesisState(importedCtx, original))

	reexported, err := importedApp.StorageRentKeeper.ExportGenesisState(importedCtx)
	require.NoError(t, err)
	require.Equal(t, original, reexported)
}

func TestGoldenEconomyFullCycleWithStepByStepInvariants(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(42)

	require.Empty(t, app.RunAppInvariants(ctx))

	fees := sdk.NewCoins(sdk.NewInt64Coin(params.BaseDenom, 20_000_000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, fees))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, fees))
	require.NoError(t, app.FeesKeeper.RecordCollectedFees(ctx, fees))
	require.Empty(t, app.RunAppInvariants(ctx))

	_, err := app.EndBlocker(ctx)
	require.NoError(t, err)
	require.Empty(t, app.RunAppInvariants(ctx))

	wrongDenomFee := sdk.NewCoins(sdk.NewInt64Coin("uosmo", 100))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, wrongDenomFee))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, wrongDenomFee))
	err = app.FeesKeeper.RecordCollectedFees(ctx, wrongDenomFee)
	require.Error(t, err)
	require.Empty(t, app.RunAppInvariants(ctx))

	zeroFee := sdk.NewCoins()
	require.NoError(t, app.FeesKeeper.RecordCollectedFees(ctx, zeroFee))
	require.Empty(t, app.RunAppInvariants(ctx))

	configureGoldenBurnParams(t, app, ctx)
	configureGoldenEmissionParams(t, app, ctx)
	ctx = ctx.WithBlockHeight(43)
	emission, err := app.FinalizeNativeEconomyEpoch(ctx, 7, 5_000)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt64Coin(params.BaseDenom, 1_000), emission.EmissionAmount)
	require.Empty(t, app.RunAppInvariants(ctx))

	rentPayer := AddTestAddrsWithCoins(t, app, ctx, 1, sdk.NewCoins(sdk.NewInt64Coin(params.BaseDenom, 1_000)))[0]
	allocations, remainder, err := app.FeeCollectorKeeper.CollectAndDistributeProtocolIncomeFromAccount(ctx, rentPayer, sdk.NewCoins(sdk.NewInt64Coin(params.BaseDenom, 100)))
	require.NoError(t, err)
	require.True(t, remainder.Empty())
	require.NotEmpty(t, allocations)
	require.Empty(t, app.RunAppInvariants(ctx))

	feeCollectorGenesis, err := app.FeeCollectorKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	burnGenesis, err := app.BurnKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	emissionsGenesis, err := app.EmissionsKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	treasuryGenesis, err := app.TreasuryKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	storageRentGenesis, err := app.StorageRentKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)

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
	require.NoError(t, importedApp.TreasuryKeeper.InitGenesis(importedCtx, *treasuryGenesis))
	require.NoError(t, importedApp.StorageRentKeeper.InitGenesisState(importedCtx, storageRentGenesis))
	require.Empty(t, importedApp.RunAppInvariants(importedCtx))
}

func initTreasuryBankBalance(ctx sdk.Context, app *L1App, gs *treasurytypes.GenesisState) error {
	accounting := gs.Allocations.AccountingBalance()
	if !accounting.Empty() {
		if err := app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, accounting); err != nil {
			return err
		}
		return app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, treasurytypes.TreasuryModuleName, accounting)
	}
	return nil
}
