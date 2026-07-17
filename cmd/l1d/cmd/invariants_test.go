package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	l1app "github.com/sovereign-l1/l1/app"
)

func TestInvariantListCommandReturnsCriticalRoutes(t *testing.T) {
	out, err := executeAVMCommand(NewInvariantsCmd(), "list")
	require.NoError(t, err)

	var res struct {
		Command	string		`json:"command"`
		Routes	[]string	`json:"routes"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	require.Equal(t, "invariants list", res.Command)
	require.ElementsMatch(t, l1app.CriticalAppInvariantRoutes(), res.Routes)
}

func TestInvariantCheckCommandRunsDefaultGenesisRunner(t *testing.T) {
	out, err := executeAVMCommand(NewInvariantsCmd(), "check")
	require.NoError(t, err)

	var report invariantCheckReport
	require.NoError(t, json.Unmarshal([]byte(out), &report), out)
	require.Equal(t, "invariants check", report.Command)
	require.Equal(t, "default-genesis", report.Mode)
	require.True(t, report.Passed, out)
	require.Empty(t, report.Failures)
	require.ElementsMatch(t, l1app.CriticalAppInvariantRoutes(), report.Routes)

	// AppInvariantGenesisExport now RUNS and PASSES rather than being skipped.
	// It previously failed (and was filtered into a "skipped" list) only
	// because the runner checked invariants against an uncommitted -- i.e.
	// empty -- store. The runner now finalizes and commits genesis first, so
	// the export invariant is genuinely exercised and the filter that used to
	// swallow it is gone: an export failure here is now a real failure rather
	// than a silently tolerated one.
	require.Contains(t, report.Routes, "aetra/"+l1app.AppInvariantGenesisExport)
}
