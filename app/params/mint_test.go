package params

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/stretchr/testify/require"
)

func TestAetraMintPolicyMatchesEconomicsSpec(t *testing.T) {
	params := AetraMintParams()

	require.Equal(t, BaseDenom, params.MintDenom)
	require.Equal(t, BpsToLegacyDec(DefaultResponsivenessBps), params.InflationRateChange)
	// The stock x/mint minter is deliberately neutered to zero inflation:
	// protocol inflation is produced solely by the native emissions pipeline.
	// Re-enabling non-zero inflation here reintroduces the double-mint
	// (SEC-CRIT: double inflation).
	require.True(t, params.InflationMin.IsZero(), "x/mint min inflation must be zero")
	require.True(t, params.InflationMax.IsZero(), "x/mint max inflation must be zero")
	require.Equal(t, BpsToLegacyDec(DefaultTargetStakeBps), params.GoalBonded)
	require.Equal(t, int64(150), MinInflationBps)
	require.Equal(t, int64(800), MaxInflationBps)
	require.Equal(t, int64(6_500), DefaultTargetStakeBps)
	require.True(t, params.MaxSupply.IsZero(), "zero max supply means uncapped issuance")
	require.NoError(t, params.Validate())

	minter := AetraInitialMinter()
	require.True(t, minter.Inflation.IsZero(), "x/mint genesis minter must start at zero inflation")
	require.NoError(t, minttypes.ValidateGenesis(*AetraMintGenesisState()))
}

// TestMintAuthorityCapsCoverEmissionSchedule locks the invariant that the
// mint-authority safety caps are always at least as large as the emission the
// native schedule can legitimately produce. If this regresses, the EndBlock
// emission would trip the cap and halt the chain (SEC-CRIT: genesis emission
// vs mint-cap chain halt).
func TestMintAuthorityCapsCoverEmissionSchedule(t *testing.T) {
	maxEpoch := MaxScheduledEpochEmissionNaet()
	epochCap := MintAuthorityEpochCapNaet()
	lifetimeCap := MintAuthorityLifetimeCapNaet()

	wantMaxEpoch := sdkmath.NewInt(AnnualReferenceSupplyNaet).
		MulRaw(MaxInflationBps).
		QuoRaw(BasisPoints).
		QuoRaw(EpochsPerYear)
	require.True(t, maxEpoch.Equal(wantMaxEpoch), "max scheduled epoch emission = %s, want %s", maxEpoch, wantMaxEpoch)

	require.True(t, epochCap.GTE(maxEpoch), "epoch cap %s must cover max scheduled emission %s", epochCap, maxEpoch)
	require.True(t, lifetimeCap.GTE(epochCap), "lifetime cap %s must be >= epoch cap %s", lifetimeCap, epochCap)

	// The default (300 bps) per-epoch emission must also comfortably fit.
	defaultEpoch := sdkmath.NewInt(AnnualReferenceSupplyNaet).
		MulRaw(DefaultTargetInflationBps).
		QuoRaw(BasisPoints).
		QuoRaw(EpochsPerYear)
	require.True(t, epochCap.GT(defaultEpoch), "epoch cap %s must exceed default per-epoch emission %s", epochCap, defaultEpoch)
}
