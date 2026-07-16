package params

import (
	sdkmath "cosmossdk.io/math"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
)

// Emission schedule reference values. These are the single source of truth
// shared by x/emissions (which computes the per-epoch protocol emission) and
// x/mint-authority (which sizes its mint safety caps). Keeping both derived
// from the same constants prevents the two subsystems from drifting into an
// inconsistent state that would halt the chain in EndBlock. See security audit
// finding SEC-CRIT: genesis emission vs mint-cap chain halt.
const (
	AnnualReferenceSupplyNaet = int64(365_000_000_000)
	EpochsPerYear             = int64(365)

	// mintAuthorityEpochCapHeadroom multiplies the maximum scheduled per-epoch
	// emission to give the mint-authority epoch cap headroom above legitimate
	// emission, so normal operation never trips the cap.
	mintAuthorityEpochCapHeadroom = int64(4)
	// mintAuthorityLifetimeCapYears sizes the lifetime mint safety ceiling for
	// many years of maximum emission — far above any realistic testnet horizon
	// while still bounding a runaway-minting bug.
	mintAuthorityLifetimeCapYears = int64(1_000)
)

func BpsToLegacyDec(bps int64) sdkmath.LegacyDec {
	return sdkmath.LegacyNewDec(bps).Quo(sdkmath.LegacyNewDec(BasisPoints))
}

// AetraInitialMinter returns the genesis minter for the stock x/mint module.
//
// Protocol inflation on Aetra is produced exclusively by the custom native
// emissions pipeline (x/emissions -> x/mint-authority -> fee collector). The
// stock x/mint BeginBlocker is deliberately neutered (zero inflation) so it
// does not mint a second, uncapped and unaccounted inflation stream on top of
// native emissions. See security audit finding SEC-CRIT: double inflation.
func AetraInitialMinter() minttypes.Minter {
	return minttypes.InitialMinter(sdkmath.LegacyZeroDec())
}

func AetraMintParams() minttypes.Params {
	params := minttypes.DefaultParams()
	params.MintDenom = BaseDenom
	params.InflationRateChange = BpsToLegacyDec(DefaultResponsivenessBps)
	// Zero min == max pins the stock minter to zero inflation; native emissions
	// is the sole protocol inflation source.
	params.InflationMin = sdkmath.LegacyZeroDec()
	params.InflationMax = sdkmath.LegacyZeroDec()
	params.GoalBonded = BpsToLegacyDec(DefaultTargetStakeBps)
	params.MaxSupply = sdkmath.ZeroInt()
	return params
}

func AetraMintGenesisState() *minttypes.GenesisState {
	return minttypes.NewGenesisState(AetraInitialMinter(), AetraMintParams())
}

// MaxScheduledEpochEmissionNaet is the largest per-epoch protocol emission the
// native emission schedule can produce, i.e. at the constitutional maximum
// inflation. It is the reference figure the mint-authority safety caps are
// sized against.
//
// This is the BOOTSTRAP figure, anchored to AnnualReferenceSupplyNaet and the
// package-level defaults. A live chain sizes the same quantity against its
// real circulating supply AND its actually-configured rate/cadence via
// MaxScheduledEpochEmissionNaetFor -- using the package defaults there instead
// would silently drift the cap out of sync with whatever x/emissions is really
// configured to mint whenever a chain overrides MaxAnnualInflationBps or
// EpochsPerYear away from the bootstrap values, i.e. the exact class of bug
// SEC-CRIT (genesis emission vs mint-cap chain halt) was fixed to prevent.
func MaxScheduledEpochEmissionNaet() sdkmath.Int {
	return MaxScheduledEpochEmissionNaetFor(sdkmath.NewInt(AnnualReferenceSupplyNaet), MaxInflationBps, EpochsPerYear)
}

// MaxScheduledEpochEmissionNaetFor sizes the maximum per-epoch emission against
// a supply anchor, maximum inflation rate and epoch cadence supplied by the
// caller. On a live chain the anchor is the real circulating supply and the
// rate/cadence are x/emissions' own configured
// ConstitutionalMaxInflationBps/EpochsPerYear, so the cap tracks the thing it
// bounds instead of a set of genesis-time constants that can drift from it.
//
// Sizing the ceiling relative to supply is still a real bound: emission can
// only ever be a fixed multiple of the maximum legitimate rate, and since
// supply itself can only grow through emission, the cap cannot run away -- a
// total minting bug is bounded at mintAuthorityEpochCapHeadroom x the
// constitutional maximum, not at an unbounded amount.
func MaxScheduledEpochEmissionNaetFor(referenceSupply sdkmath.Int, maxInflationBps, epochsPerYear int64) sdkmath.Int {
	if referenceSupply.IsNil() || !referenceSupply.IsPositive() {
		referenceSupply = sdkmath.NewInt(AnnualReferenceSupplyNaet)
	}
	if maxInflationBps <= 0 {
		maxInflationBps = MaxInflationBps
	}
	if epochsPerYear <= 0 {
		epochsPerYear = EpochsPerYear
	}
	return referenceSupply.
		MulRaw(maxInflationBps).
		QuoRaw(BasisPoints).
		QuoRaw(epochsPerYear)
}

// MintAuthorityEpochCapNaet is the per-epoch mint-authority safety ceiling,
// sized with headroom above the maximum scheduled per-epoch emission.
func MintAuthorityEpochCapNaet() sdkmath.Int {
	return MintAuthorityEpochCapNaetFor(sdkmath.NewInt(AnnualReferenceSupplyNaet), MaxInflationBps, EpochsPerYear)
}

// MintAuthorityEpochCapNaetFor is MintAuthorityEpochCapNaet against a live
// supply anchor, rate and cadence.
func MintAuthorityEpochCapNaetFor(referenceSupply sdkmath.Int, maxInflationBps, epochsPerYear int64) sdkmath.Int {
	return MaxScheduledEpochEmissionNaetFor(referenceSupply, maxInflationBps, epochsPerYear).MulRaw(mintAuthorityEpochCapHeadroom)
}

// MintAuthorityLifetimeCapNaet is the lifetime mint-authority safety ceiling,
// sized for many years of maximum emission.
func MintAuthorityLifetimeCapNaet() sdkmath.Int {
	return MintAuthorityLifetimeCapNaetFor(sdkmath.NewInt(AnnualReferenceSupplyNaet), MaxInflationBps, EpochsPerYear)
}

// MintAuthorityLifetimeCapNaetFor is MintAuthorityLifetimeCapNaet against a
// live supply anchor, rate and cadence.
func MintAuthorityLifetimeCapNaetFor(referenceSupply sdkmath.Int, maxInflationBps, epochsPerYear int64) sdkmath.Int {
	if epochsPerYear <= 0 {
		epochsPerYear = EpochsPerYear
	}
	return MaxScheduledEpochEmissionNaetFor(referenceSupply, maxInflationBps, epochsPerYear).
		MulRaw(epochsPerYear).
		MulRaw(mintAuthorityLifetimeCapYears)
}
