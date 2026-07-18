package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	aezkeeper "github.com/sovereign-l1/l1/x/aez/keeper"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

// TestGasQuotaForZoneReadsCommittedParams proves the accessor reads the
// committed quota table (never a cache), returns the elastic cap for elastic
// zones, and returns the UNCAPPED sentinel 0 for the Core Zone.
func TestGasQuotaForZoneReadsCommittedParams(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	// Core Zone is uncapped: the sentinel 0.
	core, err := k.GasQuotaForZone(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(0), core, "core zone must read as uncapped (0)")

	for z := uint32(1); z < aeztypes.ZoneCount; z++ {
		cap_, err := k.GasQuotaForZone(ctx, z)
		require.NoError(t, err)
		require.Equal(t, uint64(3_000_000), cap_, "elastic zone %d cap", z)
	}

	// Out-of-range zone errors rather than returning a silent 0.
	_, err = k.GasQuotaForZone(ctx, aeztypes.ZoneCount)
	require.Error(t, err)
}

// TestGasQuotaForZoneReflectsGovernedUpdate proves a governed quota change is
// observed by the accessor (committed-state read, F-17-safe), and that a fresh
// keeper (restart) sees the same value.
func TestGasQuotaForZoneReflectsGovernedUpdate(t *testing.T) {
	k, ctx, svc := initGenesis(t, 1)

	params := aeztypes.DefaultParams()
	params.GasQuota.Quotas[1].MaxGas = 2_000_000 // lower zone 1's cap
	require.NoError(t, k.SetParams(ctx, params))

	restarted := aezkeeper.NewPersistentKeeper(svc)
	got, err := restarted.GasQuotaForZone(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(2_000_000), got, "a restarted node must observe the committed cap")
}

// TestSetParamsRejectsOverBudgetQuota proves governance cannot commit an
// over-budget quota table through the keeper: SetParams validates the params,
// and the reserved-Core invariant is part of that validation.
func TestSetParamsRejectsOverBudgetQuota(t *testing.T) {
	k, ctx, _ := initGenesis(t, 1)

	params := aeztypes.DefaultParams()
	// Raise every elastic cap so elastic sum (4*5M=20M) + core reserve (8M) > 20M.
	for i := range params.GasQuota.Quotas {
		if !params.GasQuota.Quotas[i].ZoneID.IsCore() {
			params.GasQuota.Quotas[i].MaxGas = 5_000_000
		}
	}
	require.Error(t, k.SetParams(ctx, params), "over-budget quota table must be rejected by SetParams")
}
