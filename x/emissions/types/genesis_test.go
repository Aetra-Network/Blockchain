package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
)

// steadyStateInflationBps iterates the emission integrator at a FIXED bonded
// ratio, feeding each epoch's output back as the next epoch's CurrentInflationBps
// exactly as the keeper does, until it reaches a fixed point (a band rail or the
// target deadband).
func steadyStateInflationBps(params Params, bondedBps uint32) uint32 {
	cur := params.CurrentInflationBps
	for i := 0; i < 10_000; i++ {
		p := params
		p.CurrentInflationBps = cur
		next := ComputeInflationBps(p, bondedBps)
		if next == cur {
			return cur
		}
		cur = next
	}
	return cur
}

// TestComputeInflationBpsSteersByStakingRatio is the behavioural contract of the
// adaptive model: fed the REAL bonded ratio, ComputeInflationBps drives inflation
// toward 8% when stake is below the 65% target and toward 1.5% when it is above,
// always inside the [150, 800] band. A pinned controller (Min == Max) cannot --
// see TestOldPinnedControllerFailsTheSteeringContract.
func TestComputeInflationBpsSteersByStakingRatio(t *testing.T) {
	params := DefaultParams()
	require.Equal(t, uint32(appparams.DefaultTargetInflationBps), params.CurrentInflationBps, "starts at the 4% seed")
	require.Equal(t, uint32(appparams.MinInflationBps), params.MinAnnualInflationBps, "band floor un-pinned to 1.5%")
	require.Equal(t, uint32(appparams.MaxInflationBps), params.MaxAnnualInflationBps, "band ceiling un-pinned to 8%")
	require.Equal(t, uint32(appparams.DefaultTargetStakeBps), params.TargetStakingRatioBps, "target is 65%")

	// floorBps/ceilBps are the 1.5%/8% band rails; startBps is the 4% seed.
	floorBps := uint32(appparams.MinInflationBps)
	ceilBps := uint32(appparams.MaxInflationBps)
	startBps := uint32(appparams.DefaultTargetInflationBps)

	// One-epoch steer from the 4% start: a strict, monotonic gradient in the
	// bonded ratio. Every output is inside the band.
	oneStep := []struct {
		bondedBps uint32
		want      uint32
	}{
		{4_000, 600}, // 40% bonded, far below target -> up
		{5_500, 480}, // 55% -> up (above the 4% start)
		{6_500, 400}, // 65% == target -> unchanged
		{7_500, 320}, // 75% -> down (below the 4% start)
		{9_000, 200}, // 90% -> down
	}
	prev := uint32(10_000)
	for _, s := range oneStep {
		got := ComputeInflationBps(params, s.bondedBps)
		require.Equalf(t, s.want, got, "one epoch at %d bps bonded", s.bondedBps)
		require.GreaterOrEqual(t, got, floorBps)
		require.LessOrEqual(t, got, ceilBps)
		require.Less(t, got, prev, "inflation must fall monotonically as the bonded ratio rises")
		prev = got
	}

	// Steady state (the integrator run to a fixed point) reaches the rails the
	// owner specified: {40% -> at 8%, 55% -> above 4%, 65% -> at 4%,
	// 75% -> below 4%, 90% -> at 1.5%}.
	require.Equal(t, ceilBps, steadyStateInflationBps(params, 4_000), "40% bonded welds at the 8% ceiling")
	require.Greater(t, steadyStateInflationBps(params, 5_500), startBps, "55% bonded settles above the 4% start")
	require.Equal(t, startBps, steadyStateInflationBps(params, 6_500), "65% bonded (== target) holds at 4%")
	require.Less(t, steadyStateInflationBps(params, 7_500), startBps, "75% bonded settles below the 4% start")
	require.Equal(t, floorBps, steadyStateInflationBps(params, 9_000), "90% bonded welds at the 1.5% floor")

	// Every output across the whole ratio range stays clamped to [150, 800].
	for bonded := uint32(0); bonded <= 10_000; bonded += 250 {
		got := ComputeInflationBps(params, bonded)
		require.GreaterOrEqualf(t, got, floorBps, "bonded %d underflowed the floor", bonded)
		require.LessOrEqualf(t, got, ceilBps, "bonded %d overflowed the ceiling", bonded)
	}
}

// TestOldPinnedControllerFailsTheSteeringContract proves the retired pin
// (MinAnnualInflationBps == MaxAnnualInflationBps == CurrentInflationBps == 400)
// welds ComputeInflationBps to 400 at EVERY bonded ratio -- exactly the behaviour
// TestComputeInflationBpsSteersByStakingRatio now forbids.
func TestOldPinnedControllerFailsTheSteeringContract(t *testing.T) {
	pin := uint32(appparams.DefaultTargetInflationBps) // 400

	pinned := DefaultParams()
	pinned.MinAnnualInflationBps = pin
	pinned.MaxAnnualInflationBps = pin
	pinned.CurrentInflationBps = pin

	for _, bondedBps := range []uint32{0, 4_000, 5_500, 6_500, 7_500, 9_000, 10_000} {
		require.Equalf(t, pin, ComputeInflationBps(pinned, bondedBps),
			"pinned controller must return 400 at %d bps bonded", bondedBps)
	}

	// Concretely: at 40% bonded the un-pinned controller steers UP (>400) and at
	// 90% it steers DOWN (<400), while the pin stays flat at both. The steering
	// test asserts exactly these, so the pin cannot satisfy it.
	adaptive := DefaultParams()
	require.Greater(t, ComputeInflationBps(adaptive, 4_000), pin)
	require.Less(t, ComputeInflationBps(adaptive, 9_000), pin)
	require.Equal(t, pin, ComputeInflationBps(pinned, 4_000))
	require.Equal(t, pin, ComputeInflationBps(pinned, 9_000))
}
