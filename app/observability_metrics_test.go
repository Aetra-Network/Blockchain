package app

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"

	"github.com/sovereign-l1/l1/observability"
)

// testValidatorsWithConsAddrs builds n staking validators with real (cached)
// consensus pubkeys so GetConsAddr works, returning them alongside the string
// map keys the observability sweep uses for signing-info lookups.
func testValidatorsWithConsAddrs(t *testing.T, n int) ([]stakingValidator, []string) {
	t.Helper()
	vals := make([]stakingValidator, 0, n)
	keys := make([]string, 0, n)
	for i := 0; i < n; i++ {
		pk := ed25519.GenPrivKeyFromSecret([]byte{byte(i + 1)}).PubKey()
		anyPk, err := codectypes.NewAnyWithValue(pk)
		require.NoError(t, err)
		vals = append(vals, stakingValidator{
			OperatorAddress:	fmt.Sprintf("op%d", i),
			ConsensusPubkey:	anyPk,
		})
		keys = append(keys, string(pk.Address().Bytes()))
	}
	return vals, keys
}

func TestTopNPowerSharesBps(t *testing.T) {
	_, _, _, ok := topNPowerSharesBps(nil)
	require.False(t, ok, "empty set has no concentration")

	// Single validator owns everything: every top-N share is 100%.
	t10, t20, t33, ok := topNPowerSharesBps([]math.Int{math.NewInt(1000)})
	require.True(t, ok)
	require.Equal(t, int64(10_000), t10)
	require.Equal(t, int64(10_000), t20)
	require.Equal(t, int64(10_000), t33)

	// 40 equal validators (100 each, total 4000): top-10 = 25%, top-20 = 50%,
	// top-33 = 82.5%.
	eq := make([]math.Int, 40)
	for i := range eq {
		eq[i] = math.NewInt(100)
	}
	t10, t20, t33, ok = topNPowerSharesBps(eq)
	require.True(t, ok)
	require.Equal(t, int64(2_500), t10)
	require.Equal(t, int64(5_000), t20)
	require.Equal(t, int64(8_250), t33)

	// Fewer than 10 validators: every top-N clamps to the whole set = 100%.
	few := []math.Int{math.NewInt(50), math.NewInt(30), math.NewInt(20)}
	t10, t20, t33, ok = topNPowerSharesBps(few)
	require.True(t, ok)
	require.Equal(t, int64(10_000), t10)
	require.Equal(t, int64(10_000), t20)
	require.Equal(t, int64(10_000), t33)

	// A dominant top validator: 900 of 1000 total across 4 validators.
	skewed := []math.Int{math.NewInt(900), math.NewInt(50), math.NewInt(30), math.NewInt(20)}
	t10, _, _, ok = topNPowerSharesBps(skewed)
	require.True(t, ok)
	require.Equal(t, int64(10_000), t10, "all 4 fit in top-10 => 100%")
}

func TestRatioBps(t *testing.T) {
	v, ok := ratioBps(math.NewInt(500), math.NewInt(1000))
	require.True(t, ok)
	require.Equal(t, int64(5_000), v)

	_, ok = ratioBps(math.NewInt(1), math.ZeroInt())
	require.False(t, ok, "non-positive denominator is skipped, not emitted as garbage")

	// Numerator above denominator is clamped to 100%.
	v, ok = ratioBps(math.NewInt(2_000), math.NewInt(1_000))
	require.True(t, ok)
	require.Equal(t, int64(10_000), v)

	v, ok = ratioBps(math.NewInt(-5), math.NewInt(1_000))
	require.True(t, ok)
	require.Equal(t, int64(0), v)
}

// TestRecordValidatorObservabilityMetricsIsSafe confirms the EndBlock sweep is a
// safe no-op when off-interval and when there is no bonded validator set (as in
// a freshly set-up test app), and never panics — including when run repeatedly
// (the transition-snapshot path).
func TestRecordValidatorObservabilityMetricsIsSafe(t *testing.T) {
	app := Setup(t, false)
	observability.SetEnabled(true)
	t.Cleanup(func() { observability.SetEnabled(false) })

	offInterval := app.NewContext(false).WithBlockHeight(validatorHealthMetricInterval - 1)
	require.NotPanics(t, func() { app.recordValidatorObservabilityMetrics(offInterval) })

	onInterval := app.NewContext(false).WithBlockHeight(validatorHealthMetricInterval)
	require.NotPanics(t, func() { app.recordValidatorObservabilityMetrics(onInterval) })

	nextInterval := app.NewContext(false).WithBlockHeight(2 * validatorHealthMetricInterval)
	require.NotPanics(t, func() { app.recordValidatorObservabilityMetrics(nextInterval) })
}

func TestEstimatedGrossAPRBps(t *testing.T) {
	// 10% inflation, 70% validator share, annual reference 10_000, bonded 3_500:
	// annual validator rewards = 10_000 * 0.10 * 0.70 = 700; APR = 700/3500 = 20%.
	apr, ok := estimatedGrossAPRBps(math.NewInt(10_000), 1_000, 7_000, math.NewInt(3_500))
	require.True(t, ok)
	require.Equal(t, int64(2_000), apr)

	// Tiny bonded stake can push APR past 100% — must NOT clamp to 10000.
	apr, ok = estimatedGrossAPRBps(math.NewInt(10_000), 1_000, 7_000, math.NewInt(100))
	require.True(t, ok)
	require.Equal(t, int64(70_000), apr)

	_, ok = estimatedGrossAPRBps(math.NewInt(10_000), 0, 7_000, math.NewInt(3_500))
	require.False(t, ok, "zero inflation is skipped")
	_, ok = estimatedGrossAPRBps(math.NewInt(10_000), 1_000, 7_000, math.ZeroInt())
	require.False(t, ok, "zero bonded stake is skipped")
	_, ok = estimatedGrossAPRBps(math.ZeroInt(), 1_000, 7_000, math.NewInt(3_500))
	require.False(t, ok, "zero reference supply is skipped")
}

func TestBondedUptimeBps(t *testing.T) {
	_, _, ok := bondedUptimeBps(nil, nil, 100)
	require.False(t, ok, "no bonded validators")

	vals, consKeys := testValidatorsWithConsAddrs(t, 3)
	missed := map[string]int64{
		consKeys[0]: 0,	// 100% uptime
		consKeys[1]: 5,	// 95%
		consKeys[2]: 50,	// 50%
	}
	minBps, avgBps, ok := bondedUptimeBps(vals, missed, 100)
	require.True(t, ok)
	require.Equal(t, int64(5_000), minBps)
	require.Equal(t, int64(8_166), avgBps, "(10000+9500+5000)/3")

	// Missing signing info counts as fully up; missed above the window clamps.
	missed = map[string]int64{consKeys[2]: 500}
	minBps, avgBps, ok = bondedUptimeBps(vals, missed, 100)
	require.True(t, ok)
	require.Equal(t, int64(0), minBps)
	require.Equal(t, int64(6_666), avgBps, "(10000+10000+0)/3")

	_, _, ok = bondedUptimeBps(vals, nil, 0)
	require.False(t, ok, "non-positive window is skipped")
}

func TestDiffValidatorHealth(t *testing.T) {
	prev := validatorHealthObsSnapshot{
		initialized:		true,
		missedByCons:		map[string]int64{"a": 10, "b": 4},
		jailedByOp:		map[string]bool{"op1": false, "op2": true, "op3": false},
		tombstonedByCons:	map[string]bool{"a": false, "b": false},
	}
	current := validatorHealthObsSnapshot{
		initialized:	true,
		// a: +5 growth; b: shrank (window slide) — only growth counts; c: new
		// validator first seen at 3.
		missedByCons:		map[string]int64{"a": 15, "b": 1, "c": 3},
		jailedByOp:		map[string]bool{"op1": true, "op2": false, "op3": true},
		tombstonedByCons:	map[string]bool{"a": false, "b": true, "c": false},
	}
	// op1's consensus key is tombstoned (double-sign jail); op3's is not
	// (downtime jail + downtime slash); op2 unjailed.
	tombstonedByOp := map[string]bool{"op1": true, "op3": false}

	tr := diffValidatorHealth(prev, current, tombstonedByOp)
	require.Equal(t, int64(8), tr.missedDelta, "5 growth on a + 3 first-seen on c")
	require.Equal(t, 1, tr.doubleSignJails, "op1")
	require.Equal(t, 1, tr.downtimeJails, "op3")
	require.Equal(t, 1, tr.unjails, "op2")
	require.Equal(t, 1, tr.downtimeSlashes, "op3's downtime jail implies a downtime slash")
	require.Equal(t, 1, tr.doubleSignSlashes, "b newly tombstoned")

	// Identical snapshots produce zero transitions.
	require.Equal(t, validatorHealthTransitions{}, diffValidatorHealth(current, current, tombstonedByOp))
}
