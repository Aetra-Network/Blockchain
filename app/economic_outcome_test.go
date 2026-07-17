package app

import (
	"fmt"
	"math"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
	emissionstypes "github.com/sovereign-l1/l1/x/emissions/types"
	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
)

// This file is THE CONTRACT of the calibrated economy.
//
// The individual constants (DefaultTargetInflationBps, EmissionFeeBurnAnnualCapBps,
// DistributionWeights, the fee split, ...) are an IMPLEMENTATION DETAIL. What the
// chain's owner actually specified is an OUTCOME:
//
//   1. NET annual supply growth lands in 3-5%.
//   2. Validators earn a real return.
//   3. Fees are not made absurdly cheap.
//
// These tests assert the outcome directly, from (supply, TPS, fee), driving the
// real emission code path and the real default params. If a future change moves a
// constant and the economy still lands in band, these stay green -- that is the
// point. If a change is "just a parameter tweak" but pushes real supply growth out
// of band, these fail.
//
// The model behind them, with d = 1 (the epoch is time-triggered, so realized
// inflation equals configured inflation at any block time) and beta = 0 (emission
// BurnBps = 0):
//
//	NET(T) = i*d*(1-beta) - min(phi*b, gamma)  =  i - min(phi*b, gamma)
//	phi    = T*k*f/S        (annual fee revenue as a fraction of supply)
//
// with i = 400 bps, gamma = 100 bps, b = 5000 bps, k = 31,536,000 s/yr. The burn
// cap gamma is what turns "NET in [i-gamma, i] = [3%, 4%]" into an ALGEBRAIC
// IDENTITY at every throughput, rather than a tuning that holds at one operating
// point. Target throughput for the calibration is T = 10 TPS.

const (
	naetPerAET = int64(1_000_000_000)

	// targetMilliTPS is the throughput the calibration is sized for: 10 TPS,
	// ~864k tx/day. The chain's hard gas ceiling is ~30.6 TPS (20M block gas /
	// ~127k gas per transfer / 5.14s block), so this leaves ~3x headroom.
	targetMilliTPS = int64(10_000)

	// liveMeasuredFeeNaet is the fee actually charged for a transfer on the live
	// 10-validator net: 0.498 AET. It is 1.3x the ~0.378 AET spam floor (the fee
	// at which sustaining a 100%-capacity attack for a day costs $10k at
	// $0.01/AET) and TON-tier in USD terms (~$0.005), so it satisfies the
	// owner's "fees must not be absurdly cheap" constraint.
	liveMeasuredFeeNaet = int64(498_000_000)

	// congestedFeeNaet is the dynamic fee at 100% block utilization: 5 AET.
	// x/fees ramps quadratically from the 0.4 AET base anchor up to this.
	congestedFeeNaet = int64(5_000_000_000)

	// liveGenesisSupplyAET is the measured genesis supply of the 10-validator
	// net that motivated this calibration.
	liveGenesisSupplyAET = int64(80_622_281)

	// targetSupplyAET is the supply the calibration is sized for: 16B AET.
	// Derived from the sizing law S_min = T*k*f/phi_max with f = 0.5 AET and the
	// peer-defensible ceiling phi_max = 1%/yr:
	//   S >= 10 * 31,536,000 * 0.5 / 0.01 = 1.577e10 -> 16e9 AET.
	targetSupplyAET = int64(16_000_000_000)
)

// aetToNaet returns amount AET in naet as an sdkmath.Int. It deliberately does
// NOT go through int64: 16B AET = 1.6e19 naet overflows int64 (max 9.223e18,
// i.e. 9,223,372,036 AET). The live emission path is sdkmath.Int throughout, so
// it is safe, but any test helper that reaches for int64 here would silently
// wrap.
func aetToNaet(amountAET int64) sdkmath.Int {
	return sdkmath.NewInt(amountAET).Mul(sdkmath.NewInt(naetPerAET))
}

// annualFeeBurnNaet returns the fee-burn side of the model:
// min(T*k*f*b, gamma*S), using the REAL default fee split and the REAL cap
// constant.
func annualFeeBurnNaet(milliTPS int64, feeNaet int64, supplyNaet sdkmath.Int) sdkmath.Int {
	feeParams := feecollectortypes.DefaultParams()

	// txPerYear = T * k, computed in milli-TPS to stay exact in integers.
	txPerYear := sdkmath.NewInt(milliTPS).
		Mul(sdkmath.NewInt(appparams.SecondsPerYear)).
		Quo(sdkmath.NewInt(1_000))

	grossFees := txPerYear.Mul(sdkmath.NewInt(feeNaet))
	uncapped := grossFees.
		Mul(sdkmath.NewInt(int64(feeParams.BurnBps))).
		Quo(sdkmath.NewInt(appparams.BasisPoints))

	// The cap: gamma of supply per year. This is the mechanism that bounds an
	// otherwise UNBOUNDED quantity -- fee burn is linear in throughput while
	// emission is a fixed fraction of supply, so without this there is always a
	// T at which burn overwhelms mint.
	cap := supplyNaet.
		Mul(sdkmath.NewInt(appparams.EmissionFeeBurnAnnualCapBps)).
		Quo(sdkmath.NewInt(appparams.BasisPoints))

	if uncapped.GT(cap) {
		return cap
	}
	return uncapped
}

// annualEmissionNaet drives the REAL emission code path for one year: it asks
// x/emissions for a single epoch's record against the supply anchor and scales by
// EpochsPerYear. This pins i, beta (the emission burn weight) and EpochsPerYear
// through production code rather than restating them here.
func annualEmissionNaet(t *testing.T, supplyNaet sdkmath.Int, stakingRatioBps uint64) (mint sdkmath.Int, emissionBurn sdkmath.Int, inflationBps uint32) {
	t.Helper()
	params := emissionstypes.DefaultParams()
	record, err := emissionstypes.ComputeEpochEmissionWithSupply(params, 1, stakingRatioBps, 1, supplyNaet)
	require.NoError(t, err)

	epochs := sdkmath.NewInt(int64(params.EpochsPerYear))
	return record.EmissionAmount.Amount.Mul(epochs), record.Burn.Amount.Mul(epochs), record.InflationBps
}

// netAnnualGrowthBps is the outcome the owner specified, in bps of supply.
func netAnnualGrowthBps(t *testing.T, supplyNaet sdkmath.Int, stakingRatioBps uint64, milliTPS, feeNaet int64) int64 {
	t.Helper()
	mint, emissionBurn, _ := annualEmissionNaet(t, supplyNaet, stakingRatioBps)
	feeBurn := annualFeeBurnNaet(milliTPS, feeNaet, supplyNaet)

	net := mint.Sub(emissionBurn).Sub(feeBurn)
	return net.Mul(sdkmath.NewInt(appparams.BasisPoints)).Quo(supplyNaet).Int64()
}

// TestNetAnnualSupplyGrowthStaysInOwnerBand is the headline contract: for every
// physically reachable throughput, at both the live and the target supply, and at
// both the normal and the congested fee, net annual supply growth must land in
// 3-5%.
//
// The grid deliberately includes throughputs far past the chain's ~30.6 TPS gas
// ceiling (and a 1000 TPS absurdity) to prove the band is an identity rather than
// a tuning: it is the burn cap, not a coincidence of the operating point, that
// holds the floor.
func TestNetAnnualSupplyGrowthStaysInOwnerBand(t *testing.T) {
	const (
		minNetBps = int64(300)
		maxNetBps = int64(500)
	)

	supplies := []struct {
		name string
		aet  int64
	}{
		{name: "live-genesis-80.6M", aet: liveGenesisSupplyAET},
		{name: "target-16B", aet: targetSupplyAET},
	}
	fees := []struct {
		name string
		naet int64
	}{
		{name: "normal-0.498AET", naet: liveMeasuredFeeNaet},
		{name: "congested-5AET", naet: congestedFeeNaet},
	}
	// milliTPS: 0.1, 1, 5, 10 (target), 30.6 (gas ceiling), 91.3, 1000 (absurd).
	throughputs := []int64{100, 1_000, 5_000, targetMilliTPS, 30_600, 91_300, 1_000_000}

	for _, supply := range supplies {
		for _, fee := range fees {
			for _, milliTPS := range throughputs {
				name := fmt.Sprintf("%s/%s/%dmTPS", supply.name, fee.name, milliTPS)
				t.Run(name, func(t *testing.T) {
					supplyNaet := aetToNaet(supply.aet)
					// 84.26% bonded: the ratio measured live, far above the
					// 60% emission target. Under the old controller this alone
					// drove inflation to the floor.
					netBps := netAnnualGrowthBps(t, supplyNaet, 8_426, milliTPS, fee.naet)

					require.GreaterOrEqual(t, netBps, minNetBps,
						"net annual supply growth %d bps fell BELOW the owner's 3%% floor -- the fee burn is outrunning emission", netBps)
					require.LessOrEqual(t, netBps, maxNetBps,
						"net annual supply growth %d bps rose ABOVE the owner's 5%% ceiling", netBps)
				})
			}
		}
	}
}

// TestNetGrowthWithoutBurnCapLeavesBandProves the cap is load-bearing. Without
// it, the live operating point (7.63 TPS at 0.498 AET on an 80.6M supply) runs at
// roughly -73%/yr: the chain destroys ~3/4 of its money supply per year. This is
// the failure the calibration exists to fix, and it is asserted here so nobody can
// remove the cap and still see TestNetAnnualSupplyGrowthStaysInOwnerBand pass.
func TestNetGrowthWithoutBurnCapLeavesBand(t *testing.T) {
	supplyNaet := aetToNaet(liveGenesisSupplyAET)
	feeParams := feecollectortypes.DefaultParams()

	// 7.63 TPS, the sustained throughput measured on the live 10-validator net.
	txPerYear := sdkmath.NewInt(7_630).
		Mul(sdkmath.NewInt(appparams.SecondsPerYear)).
		Quo(sdkmath.NewInt(1_000))
	uncappedBurn := txPerYear.
		Mul(sdkmath.NewInt(liveMeasuredFeeNaet)).
		Mul(sdkmath.NewInt(int64(feeParams.BurnBps))).
		Quo(sdkmath.NewInt(appparams.BasisPoints))

	mint, emissionBurn, _ := annualEmissionNaet(t, supplyNaet, 8_426)
	uncappedNetBps := mint.Sub(emissionBurn).Sub(uncappedBurn).
		Mul(sdkmath.NewInt(appparams.BasisPoints)).Quo(supplyNaet).Int64()

	require.Less(t, uncappedNetBps, int64(-5_000),
		"sanity: without the burn cap the live operating point must be deeply deflationary (measured ~-73%%/yr); got %d bps", uncappedNetBps)

	// And with the cap, the same operating point is back in band.
	cappedNetBps := netAnnualGrowthBps(t, supplyNaet, 8_426, 7_630, liveMeasuredFeeNaet)
	require.GreaterOrEqual(t, cappedNetBps, int64(300))
	require.LessOrEqual(t, cappedNetBps, int64(500))
}

// TestEmissionRateIsIndependentOfStakingRatio pins the Tinbergen fix.
//
// One instrument cannot hit two targets. The old controller used inflation to
// steer the BONDED RATIO, which made net supply growth a function of how much
// stake happened to be bonded -- at the measured 84.26% it drove inflation to the
// 150 bps floor in a single epoch and realized 1.5% while the params advertised
// 3%. Worse, it is an INTEGRATOR whose output is fed back as its next input with
// no restoring term, so it welds to whichever rail it reaches.
//
// The rate is now pinned and the staking ratio is steered by the emission SPLIT
// instead (DefaultDistributionWeights' validator share), which is neutral to total
// supply growth. So: inflation must not move with the staking ratio, at all.
func TestEmissionRateIsIndependentOfStakingRatio(t *testing.T) {
	supplyNaet := aetToNaet(targetSupplyAET)

	for _, stakingRatioBps := range []uint64{0, 1_000, 5_000, 6_000, 8_426, 10_000} {
		t.Run(fmt.Sprintf("bonded-%dbps", stakingRatioBps), func(t *testing.T) {
			_, _, inflationBps := annualEmissionNaet(t, supplyNaet, stakingRatioBps)
			require.Equal(t, uint32(appparams.DefaultTargetInflationBps), inflationBps,
				"inflation must be pinned at %d bps regardless of the bonded ratio; the split, not the rate, is the staking instrument",
				appparams.DefaultTargetInflationBps)
		})
	}
}

// TestEmissionRateIsPinnedAcrossRepeatedEpochs guards the integrator directly:
// x/emissions writes each epoch's computed rate back as the next epoch's input
// (CurrentInflationBps), so a controller with a live band ratchets. With the band
// pinned (Min == Max), iterating must be a fixed point.
func TestEmissionRateIsPinnedAcrossRepeatedEpochs(t *testing.T) {
	params := emissionstypes.DefaultParams()
	supplyNaet := aetToNaet(targetSupplyAET)

	for epoch := uint64(1); epoch <= 24; epoch++ {
		record, err := emissionstypes.ComputeEpochEmissionWithSupply(params, epoch, 8_426, int64(epoch), supplyNaet)
		require.NoError(t, err)
		require.Equal(t, uint32(appparams.DefaultTargetInflationBps), record.InflationBps,
			"epoch %d drifted off the pin", epoch)
		// Feed the output back in, exactly as the keeper does.
		params.CurrentInflationBps = record.InflationBps
	}
}

// TestValidatorsEarnARealReturn pins the owner's hard constraint that validators
// must actually be paid.
//
// APR = (v*i + validatorFeeShare*phi) / sigma, ignoring the 2% community tax
// (which only makes the true figure marginally lower). At the target supply and
// throughput with sigma = 65% bonded this is ~6.4%, comfortably above Ethereum
// (3-4%) and TON (4%), around Solana (7%), and below Cosmos Hub (15-20%, which
// pays 10-15% inflation for it).
func TestValidatorsEarnARealReturn(t *testing.T) {
	const bondedRatioBps = int64(6_500)

	supplyNaet := aetToNaet(targetSupplyAET)
	emissionParams := emissionstypes.DefaultParams()
	feeParams := feecollectortypes.DefaultParams()

	// Emission accruing to validators over a year.
	mint, _, _ := annualEmissionNaet(t, supplyNaet, uint64(bondedRatioBps))
	validatorEmission := mint.
		Mul(sdkmath.NewInt(int64(emissionParams.DistributionWeights.ValidatorRewardBps))).
		Quo(sdkmath.NewInt(appparams.BasisPoints))

	// Fees accruing to validators over a year at the target throughput.
	txPerYear := sdkmath.NewInt(targetMilliTPS).
		Mul(sdkmath.NewInt(appparams.SecondsPerYear)).
		Quo(sdkmath.NewInt(1_000))
	validatorFees := txPerYear.
		Mul(sdkmath.NewInt(liveMeasuredFeeNaet)).
		Mul(sdkmath.NewInt(int64(feeParams.ValidatorsBps))).
		Quo(sdkmath.NewInt(appparams.BasisPoints))

	bondedNaet := supplyNaet.Mul(sdkmath.NewInt(bondedRatioBps)).Quo(sdkmath.NewInt(appparams.BasisPoints))
	aprBps := validatorEmission.Add(validatorFees).
		Mul(sdkmath.NewInt(appparams.BasisPoints)).Quo(bondedNaet).Int64()

	require.Greater(t, aprBps, int64(400),
		"validator APR %d bps is not a real return -- the owner's hard constraint is that validators must earn", aprBps)
	require.Less(t, aprBps, int64(1_200),
		"validator APR %d bps is implausibly high for %d bps of inflation; check the split", aprBps, appparams.DefaultTargetInflationBps)

	// The validator share must dominate the emission split: reserves without a
	// spend path are phantom inflation.
	require.GreaterOrEqual(t, emissionParams.DistributionWeights.ValidatorRewardBps, uint32(8_000))
}

// TestTargetSupplyIsRepresentableOnTheEmissionPath pins the one hard engineering
// consequence of sizing the supply at 16B: 16e9 AET = 1.6e19 naet does NOT fit in
// int64 (max 9.223e18 naet = 9,223,372,036 AET). Every constant and helper on the
// live emission path must therefore be sdkmath.Int, never int64.
//
// The emission path is clean today, and this test is what keeps it that way: a
// future int64 shortcut anywhere in ComputeEpochEmissionWithSupply or the
// mint-authority cap sizing would blow up here rather than on mainnet. Note that
// appparams.AnnualReferenceSupplyNaet is still typed int64 -- that is safe only
// because it is a 365 AET genesis bootstrap placeholder that the live chain never
// uses (it anchors to real bank supply), NOT because int64 is adequate for a
// supply figure. It must never be set to the target supply.
func TestTargetSupplyIsRepresentableOnTheEmissionPath(t *testing.T) {
	targetNaet := aetToNaet(targetSupplyAET)
	require.False(t, targetNaet.IsInt64(),
		"sanity: the 16B target supply must be the case that proves int64 is not an option")

	// The largest supply an int64 naet field could ever hold.
	maxInt64Supply := sdkmath.NewInt(math.MaxInt64).Quo(sdkmath.NewInt(naetPerAET))
	require.Equal(t, int64(9_223_372_036), maxInt64Supply.Int64())
	require.Less(t, maxInt64Supply.Int64(), targetSupplyAET)

	// The real emission path handles it: 16e9 * 400/10^4 = 640e6 AET/yr.
	mint, _, inflationBps := annualEmissionNaet(t, targetNaet, 8_426)
	require.Equal(t, uint32(appparams.DefaultTargetInflationBps), inflationBps)

	// ...but not to the naet. This is delta, the per-epoch truncation that forces
	// the burn cap to sit strictly below i - 300 (see
	// appparams.EmissionFeeBurnAnnualCapBps): each epoch mints
	// floor(S*i/10^4 / EpochsPerYear), so a year of them lands up to
	// EpochsPerYear-1 naet SHORT of the ideal -- never over. At the target supply
	// that is ~940 naet out of 6.4e17, i.e. ~1.5e-15 of the emission, but it is
	// strictly one-directional, which is why zero margin was never safe.
	ideal := aetToNaet(640_000_000)
	require.True(t, mint.LTE(ideal), "per-epoch truncation can only ever under-mint")
	shortfall := ideal.Sub(mint)
	require.True(t, shortfall.LT(sdkmath.NewInt(appparams.EpochsPerYear)),
		"annual mint shortfall %s naet must stay under one naet per epoch; got a real drift", shortfall)

	// So does the mint-authority epoch cap sizing.
	epochCap := appparams.MaxScheduledEpochEmissionNaetFor(
		targetNaet, appparams.EmissionConstitutionalMaxInflationBps, appparams.EpochsPerYear)
	require.True(t, epochCap.IsPositive())
}

// TestFeePressureStaysInPeerBandAtTargetSupply pins the SIZING LAW -- the reason
// the calibration calls for a 16B genesis supply rather than 80.62M.
//
// phi is annual fee revenue as a fraction of supply. Peers: Cosmos Hub ~0.08%,
// TON ~0.3%, Ethereum ~0.75%, Solana ~0.85%. The live Aetra net ran phi = 148.6%
// -- the fee bill was 1.5x the entire money supply per year, which users cannot
// physically pay because the coins do not exist. The burn cap keeps NET in band
// even then (see TestNetAnnualSupplyGrowthStaysInOwnerBand, which passes at the
// live supply too), but a capped burn only stops the supply from imploding; it
// does not make the fees payable. Only sizing the supply does.
//
//	S_min = T*k*f / phi_max
//
// This is the constraint the NET band alone does NOT catch, which is exactly why
// it is asserted separately.
func TestFeePressureStaysInPeerBandAtTargetSupply(t *testing.T) {
	const peerCeilingBps = int64(100) // 1%/yr

	feePressureBps := func(supplyAET int64) int64 {
		txPerYear := sdkmath.NewInt(targetMilliTPS).
			Mul(sdkmath.NewInt(appparams.SecondsPerYear)).
			Quo(sdkmath.NewInt(1_000))
		grossFees := txPerYear.Mul(sdkmath.NewInt(liveMeasuredFeeNaet))
		return grossFees.Mul(sdkmath.NewInt(appparams.BasisPoints)).Quo(aetToNaet(supplyAET)).Int64()
	}

	atTarget := feePressureBps(targetSupplyAET)
	require.LessOrEqual(t, atTarget, peerCeilingBps,
		"fee pressure %d bps/yr at the %d AET target supply exceeds the 1%%/yr peer ceiling", atTarget, targetSupplyAET)

	// And the counterfactual that justifies the supply change at all.
	atLive := feePressureBps(liveGenesisSupplyAET)
	require.Greater(t, atLive, int64(10_000),
		"sanity: at the 80.62M live supply, 10 TPS of 0.498 AET fees must exceed 100%% of supply per year; got %d bps", atLive)
}
