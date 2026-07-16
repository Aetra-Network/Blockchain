package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/sovereign-l1/l1/x/burn/types"
)

// TestValidateRejectsNonEmptyPermissionsOmittingFeeCollector proves the actual
// security property: a governance proposal that submits a NON-empty
// ProtocolBurnPermissions list which simply omits fee_collector (the natural
// shape of a "tighten burn permissions" proposal) must be rejected by
// Validate(). Before the fix, only the empty-list case was backfilled by
// NormalizeParams, so this exact shape passed validation and would have
// deterministically halted the chain at the next emission epoch when
// app/native_economy.go's BurnProtocolCoins(ctx, fee_collector, ...) call
// failed with an unguarded authorization error.
func TestValidateRejectsNonEmptyPermissionsOmittingFeeCollector(t *testing.T) {
	params := types.DefaultParams()
	// Non-empty list that "tightens" permissions by dropping fee_collector,
	// but keeps another module's permission so the list is not empty.
	params.ProtocolBurnPermissions = []types.BurnPermission{
		{
			ModuleName:	types.ModuleName,
			AllowedDenoms:	[]string{types.BaseDenom},
		},
	}

	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), authtypes.FeeCollectorName)
}

// TestValidateRejectsEmptyPermissions locks in that an empty
// ProtocolBurnPermissions list also fails direct Validate() (Validate() no
// longer silently tolerates a missing fee_collector permission regardless of
// list shape). Callers that want the empty-list backfill must still go
// through NormalizeParams first, as SetParams/GetParams/GenesisState.Validate
// already do.
func TestValidateRejectsEmptyPermissions(t *testing.T) {
	params := types.DefaultParams()
	params.ProtocolBurnPermissions = nil

	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), authtypes.FeeCollectorName)
}

// TestValidateAcceptsPermissionsIncludingFeeCollector is the positive control:
// a proposal that keeps fee_collector authorized alongside other modules
// still validates cleanly.
func TestValidateAcceptsPermissionsIncludingFeeCollector(t *testing.T) {
	params := types.DefaultParams()
	params.ProtocolBurnPermissions = []types.BurnPermission{
		{
			ModuleName:	types.ModuleName,
			AllowedDenoms:	[]string{types.BaseDenom},
		},
		{
			ModuleName:	authtypes.FeeCollectorName,
			AllowedDenoms:	[]string{types.BaseDenom},
		},
	}

	require.NoError(t, params.Validate())
}

// TestNormalizeParamsBackfillsEmptyPermissions locks in the pre-existing
// empty-list backfill behavior that NormalizeParams performs before
// Validate() is ever reached in the SetParams/GetParams/GenesisState paths.
func TestNormalizeParamsBackfillsEmptyPermissions(t *testing.T) {
	params := types.DefaultParams()
	params.ProtocolBurnPermissions = nil

	normalized := types.NormalizeParams(params)
	require.NoError(t, normalized.Validate())

	found := false
	for _, permission := range normalized.ProtocolBurnPermissions {
		if permission.ModuleName == authtypes.FeeCollectorName {
			found = true
		}
	}
	require.True(t, found)
}
