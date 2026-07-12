package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// emittedRequiredMetricIDs is the honest set of required public metrics that
// production code actually records at runtime. It was the living checklist for
// the observability wiring effort; it now covers every required metric, so the
// readiness report is green. If a future change adds a required metric without
// an emitter, drop it from this set — the test below will then correctly report
// the readiness regression instead of silently claiming full coverage.
var emittedRequiredMetricIDs = map[string]bool{
	RequiredMetricBlockTime:			true,
	RequiredMetricFinalityLatency:		true,
	RequiredMetricMissedBlocks:		true,
	RequiredMetricValidatorUptime:		true,
	RequiredMetricValidatorConcentration:	true,
	RequiredMetricTopNVotingPower:		true,
	RequiredMetricInflation:			true,
	RequiredMetricBondedRatio:		true,
	RequiredMetricEstimatedAPR:		true,
	RequiredMetricBurnedFees:		true,
	RequiredMetricTreasuryBalance:		true,
	RequiredMetricSlashingEvents:		true,
	RequiredMetricJailUnjailEvents:		true,
	RequiredMetricContractExecutionGas:	true,
	RequiredMetricFailedTxReasons:		true,
	RequiredMetricNodeSyncStatus:		true,
}

func TestDefaultPublicMetricsCoverRequiredSection14Metrics(t *testing.T) {
	report := BuildPublicMetricsReadinessReport(nil, nil)

	// Every required metric is declared across every surface AND emitted, so the
	// report is fully green: no failures, nothing prometheus-only, nothing
	// not-yet-emitted.
	require.Equal(t, len(requiredPublicMetricIDs()), report.RequiredCount)
	require.Empty(t, report.PrometheusOnly)
	require.Empty(t, report.Failed)
	require.Empty(t, report.NotEmitted)
	require.Equal(t, len(requiredPublicSurfaceIDs()), report.SurfaceCount)
	require.Equal(t, report.SurfaceCount, report.SurfacesReady)

	// Every required metric is wired, so ready count == required count and the
	// emitted checklist covers the full required set (guards against a future
	// required metric being added without an emitter).
	require.Equal(t, report.RequiredCount, report.ReadyCount)
	require.Len(t, emittedRequiredMetricIDs, report.RequiredCount)
	for id := range requiredPublicMetricIDs() {
		require.True(t, emittedRequiredMetricIDs[id], "required metric %s missing from emitted checklist", id)
	}

	require.True(t, report.Ready)
	require.NoError(t, ValidatePublicMetricsReadiness(nil, nil))
}

func TestPublicMetricsRejectMissingRequiredMetricSurface(t *testing.T) {
	metrics := DefaultPublicMetricSpecs()
	metrics[0].CLIQuery = false

	report := BuildPublicMetricsReadinessReport(metrics, nil)
	require.False(t, report.Ready)
	require.Contains(t, report.Failed, metrics[0].ID+":missing_required_surface")
	require.Error(t, ValidatePublicMetricsReadiness(metrics, nil))
}

func TestPublicMetricsRejectPrometheusMetricNotInRegistry(t *testing.T) {
	metrics := DefaultPublicMetricSpecs()
	metrics[0].PrometheusName = "aetra_missing_metric"

	report := BuildPublicMetricsReadinessReport(metrics, nil)
	require.False(t, report.Ready)
	require.Contains(t, report.Failed, metrics[0].ID+":missing_required_surface")
}

func TestPublicMetricsRejectMissingRequiredSurface(t *testing.T) {
	surfaces := DefaultPublicSurfaceSpecs()
	surfaces[0].Ready = false

	report := BuildPublicMetricsReadinessReport(nil, surfaces)
	require.False(t, report.Ready)
	require.Contains(t, report.Failed, surfaces[0].ID+":surface_not_ready")
}

func TestPublicMetricsRejectPrometheusOnlyExposure(t *testing.T) {
	metrics := DefaultPublicMetricSpecs()
	metrics[0].CLIQuery = false
	metrics[0].GRPCQuery = false
	metrics[0].RESTQuery = false
	metrics[0].IndexerEvent = false
	metrics[0].PublicDashboard = false

	report := BuildPublicMetricsReadinessReport(metrics, nil)
	require.False(t, report.Ready)
	require.Contains(t, report.PrometheusOnly, metrics[0].ID)
}
