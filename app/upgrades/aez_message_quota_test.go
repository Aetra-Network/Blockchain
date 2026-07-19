package upgrades

// FixupAEZMessageQuota is exercised here against a FAKE AEZKeeper rather than
// the real x/aez/keeper.Keeper: x/aez's own store-backed migration-safety
// hazard (an old-shape Params blob whose MessageQuota reads back as the Go
// zero value) is already proven end-to-end, against the real keeper and a
// real committed blob, in
// x/aez/keeper/message_quota_migration_test.go
// (TestGetParamsReadsOldShapeBlobWithMessageQuotaAsZeroValue /
// TestDrainFallsBackToLegacyBudgetForOldShapeParamsBlob). x/internal/kvtest
// is a Go "internal" package scoped to the x/ tree and cannot be imported
// from app/upgrades, so this file's job is narrower and purely logical: it
// verifies FixupAEZMessageQuota's own four branches (no-op when valid, repair
// when invalid, and both error paths) against a keeper double, independent
// of any concrete store implementation.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

// fakeAEZKeeper is a minimal in-memory AEZKeeper double.
type fakeAEZKeeper struct {
	params    aeztypes.Params
	getErr    error
	setErr    error
	setCalled bool
}

func (f *fakeAEZKeeper) GetParams(context.Context) (aeztypes.Params, error) {
	return f.params, f.getErr
}

func (f *fakeAEZKeeper) SetParams(_ context.Context, p aeztypes.Params) error {
	f.setCalled = true
	if f.setErr != nil {
		return f.setErr
	}
	f.params = p
	return nil
}

// TestFixupAEZMessageQuotaIsNoOpWhenAlreadyValid proves the helper is always
// safe to call, even on an already-upgraded chain: if the committed
// MessageQuota already validates, nothing is written.
func TestFixupAEZMessageQuotaIsNoOpWhenAlreadyValid(t *testing.T) {
	params := aeztypes.DefaultParams()
	require.NoError(t, params.MessageQuota.Validate(), "test setup: default params must already carry a valid MessageQuota")
	fake := &fakeAEZKeeper{params: params}

	fixed, err := FixupAEZMessageQuota(context.Background(), fake)
	require.NoError(t, err)
	require.False(t, fixed, "an already-valid MessageQuota must not be touched")
	require.False(t, fake.setCalled, "no-op must never write")
	require.Equal(t, params, fake.params, "params must be byte-for-byte unchanged")
}

// TestFixupAEZMessageQuotaRepairsInvalidMessageQuota is the design doc §5.4
// Layer-1 proof: given committed Params whose MessageQuota fails Validate()
// -- exactly what an old-shape blob unmarshals to (design doc §5.4;
// end-to-end proof against the real store lives in
// x/aez/keeper/message_quota_migration_test.go) -- the fixup replaces
// MessageQuota with DefaultMessageQuotaParams() and commits the repair
// through SetParams, without disturbing any other field.
func TestFixupAEZMessageQuotaRepairsInvalidMessageQuota(t *testing.T) {
	stale := aeztypes.DefaultParams()
	stale.Prototype.Enabled = true
	stale.MessageQuota = aeztypes.MessageQuotaParams{} // the exact old-shape-unmarshal hazard shape
	require.Error(t, stale.MessageQuota.Validate(), "test setup: the stale MessageQuota must be invalid")
	fake := &fakeAEZKeeper{params: stale}

	fixed, err := FixupAEZMessageQuota(context.Background(), fake)
	require.NoError(t, err)
	require.True(t, fixed)
	require.True(t, fake.setCalled)

	require.NoError(t, fake.params.MessageQuota.Validate(), "the repaired MessageQuota must validate")
	require.Equal(t, aeztypes.DefaultMessageQuotaParams(), fake.params.MessageQuota)

	// Nothing else about Params is disturbed by the repair.
	require.True(t, fake.params.Prototype.Enabled)
	require.Equal(t, stale.GasQuota, fake.params.GasQuota)
	require.Equal(t, stale.RoutingEpochLength, fake.params.RoutingEpochLength)
}

func TestFixupAEZMessageQuotaPropagatesGetParamsError(t *testing.T) {
	fake := &fakeAEZKeeper{getErr: errors.New("store fault")}
	_, err := FixupAEZMessageQuota(context.Background(), fake)
	require.ErrorContains(t, err, "store fault")
	require.False(t, fake.setCalled, "a failed read must never be followed by a write")
}

func TestFixupAEZMessageQuotaPropagatesSetParamsError(t *testing.T) {
	fake := &fakeAEZKeeper{params: aeztypes.Params{}, setErr: errors.New("write fault")}
	_, err := FixupAEZMessageQuota(context.Background(), fake)
	require.ErrorContains(t, err, "write fault")
	require.True(t, fake.setCalled)
}
