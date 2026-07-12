package app

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/observability"
)

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
// a freshly set-up test app), and never panics.
func TestRecordValidatorObservabilityMetricsIsSafe(t *testing.T) {
	app := Setup(t, false)
	observability.SetEnabled(true)
	t.Cleanup(func() { observability.SetEnabled(false) })

	offInterval := app.NewContext(false).WithBlockHeight(validatorHealthMetricInterval - 1)
	require.NotPanics(t, func() { app.recordValidatorObservabilityMetrics(offInterval) })

	onInterval := app.NewContext(false).WithBlockHeight(validatorHealthMetricInterval)
	require.NotPanics(t, func() { app.recordValidatorObservabilityMetrics(onInterval) })
}
