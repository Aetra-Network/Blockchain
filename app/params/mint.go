package params

import (
	"time"

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
	// AnnualReferenceSupplyNaet is the genesis BOOTSTRAP anchor only: the supply
	// inflation is applied to before a live chain has any supply of its own
	// (app/native_economy.go emissionReferenceSupply falls back to it, and only
	// then). A running chain never uses it -- it anchors to real circulating
	// bank supply.
	//
	// 365e9 naet = 365 AET. That is a placeholder, not a supply figure, and it
	// must stay an int64-safe value: it is the one constant on this path typed
	// int64 rather than sdkmath.Int (it feeds sdk.NewInt64Coin in
	// x/emissions/types/genesis.go). int64 tops out at 9.223e18 naet =
	// 9,223,372,036 AET, so this constant can never be set to the 16e9 AET
	// target supply -- see the int64 note on MaxScheduledEpochEmissionNaetFor.
	AnnualReferenceSupplyNaet = int64(365_000_000_000)
	// EpochsPerYear is the emission cadence: 6-hour epochs => 4 * 365 = 1460.
	//
	// It MUST stay consistent with EmissionEpochDuration; the two are the same
	// fact expressed twice (31,536,000 s/yr / 21,600 s = 1460). Emission per
	// epoch is annual/EpochsPerYear, so a mismatch silently rescales the
	// realized inflation rate away from the configured one.
	//
	// 6h rather than 24h: at 24h no epoch lands inside a normal test or netsim
	// run, so the entire emission path goes live-untested (exactly what happened
	// on the 10-validator run that motivated this calibration -- the 14400-block
	// epoch was 20.6h at the real 5.14s block and never fired). 6h also keeps
	// rounding remainders small and costs nothing in EndBlock.
	EpochsPerYear             = int64(1_460)
	// SecondsPerYear is the 365-day year every annual rate in the economy is
	// expressed against: 365 * 24 * 3600 = 31,536,000. It is the k in
	// NET = i - (T*k*f*b)/S, and the denominator that turns the annual fee-burn
	// cap into a per-interval allowance.
	SecondsPerYear = int64(31_536_000)
	// EmissionEpochDuration is the wall-clock length of one emission epoch, as
	// measured by consensus block time.
	//
	// The epoch is TIME-triggered, not block-height-triggered, and this is a
	// correctness requirement rather than a preference. A height-triggered epoch
	// of N blocks realizes inflation i * (nominal_block_time / actual_block_time)
	// -- the old 14400-block epoch assumed 6s blocks, but the measured chain runs
	// 5.14s idle and 6.89s loaded, so realized inflation swung +17%/-13% with
	// network load, in the wrong direction (a busier chain minted LESS). That
	// drift alone is enough to push net supply growth below the owner's 3% floor
	// (4.00% * 0.87 - 1.00% = 2.48%). Triggering on elapsed consensus time makes
	// the drift factor identically 1 and the realized rate equal the configured
	// one at any block time.
	//
	// ctx.BlockTime() is consensus-agreed, so this stays deterministic across
	// nodes -- unlike deriving a cadence from a locally measured block time,
	// which would be a nondeterminism (and consensus-feedback) hazard.
	EmissionEpochDuration = 6 * time.Hour

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
