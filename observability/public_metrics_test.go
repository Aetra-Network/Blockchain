package observability

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// emittedRequiredMetricIDs is the honest set of required public metrics that
// production code actually records at runtime today. It is the living
// checklist for the observability wiring effort: add an ID here in the same
// change that wires its emitter (and flips Emitted=true in
// DefaultPublicMetricSpecs). When this set covers every required metric, the
// readiness report goes green again.
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
}

func TestDefaultPublicMetricsCoverRequiredSection14Metrics(t *testing.T) {
	report := BuildPublicMetricsReadinessReport(nil, nil)

	// Every required metric is declared across every surface, so the ONLY
	// readiness failures must be honest not-yet-emitted markers -- never a
	// surface gap or an unknown/duplicate/missing metric.
	require.Equal(t, len(requiredPublicMetricIDs()), report.RequiredCount)
	require.Empty(t, report.PrometheusOnly)
	require.Equal(t, len(requiredPublicSurfaceIDs()), report.SurfaceCount)
	require.Equal(t, report.SurfaceCount, report.SurfacesReady)
	for _, f := range report.Failed {
		require.True(t, strings.HasSuffix(f, ":not_emitted"), "unexpected non-emission failure: %s", f)
	}

	// Exactly the metrics whose emitters are wired count as ready; the rest are
	// reported as not-yet-emitted rather than silently claimed ready.
	require.Equal(t, len(emittedRequiredMetricIDs), report.ReadyCount)
	require.Len(t, report.NotEmitted, report.RequiredCount-len(emittedRequiredMetricIDs))
	for id := range requiredPublicMetricIDs() {
		if emittedRequiredMetricIDs[id] {
			require.NotContains(t, report.NotEmitted, id)
		} else {
			require.Contains(t, report.NotEmitted, id)
		}
	}

	// Until every required emitter is wired, overall readiness is honestly
	// false and validation surfaces the not-emitted set.
	require.False(t, report.Ready)
	require.Error(t, ValidatePublicMetricsReadiness(nil, nil))
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
