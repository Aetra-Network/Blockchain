package accounts

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	mintauthoritytypes "github.com/sovereign-l1/l1/x/mint-authority/types"
)

func TestModuleAccountPermissionsAreCloned(t *testing.T) {
	perms := ModuleAccountPermissions()
	perms[mintauthoritytypes.ModuleName] = nil

	fresh := ModuleAccountPermissions()
	require.Equal(t, []string{authtypes.Minter}, fresh[mintauthoritytypes.ModuleName])
}

func TestReservedSystemModuleAccountWiringValidatesBlockedPolicy(t *testing.T) {
	blocked := BlockedAddresses()

	require.NoError(t, ValidateReservedSystemModuleAccountWiring(blocked))

	mint, found := ReservedSystemModuleAccountByName("AETMint")
	require.True(t, found)
	require.False(t, mint.CanReceiveUserFunds)

	addr, found, err := ReservedSystemModuleAccountAddress(mint.ModuleAccountName)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, blocked[addr.String()])
}

// --- security-audit FINDING-017: vanity-vs-module-account fund-stranding regression tests ---

// TestValidateFundStrandingExceptionsAcceptsCurrentCatalog confirms the real,
// current catalog passes the gate: today only AETBurn has
// CanReceiveUserFunds=true among the reserved module accounts, and it is a
// reviewed exception.
func TestValidateFundStrandingExceptionsAcceptsCurrentCatalog(t *testing.T) {
	require.NoError(t, validateFundStrandingExceptions(ReservedSystemModuleAccounts()))

	burn, found := ReservedSystemModuleAccountByName("AETBurn")
	require.True(t, found)
	require.True(t, burn.CanReceiveUserFunds, "test assumes AETBurn is the one reviewed fund-stranding exception")
	require.True(t, fundStrandingReviewedExceptions[burn.Name])
}

// TestValidateFundStrandingExceptionsRejectsUnreviewedCanReceiveUserFunds
// reproduces the exact FINDING-017 scenario synthetically: a fund-bearing
// reserved module account (AETReporterRewards) flips CanReceiveUserFunds to
// true without being added to fundStrandingReviewedExceptions. The gate must
// reject it, since a plain send to its reserved catalog address would strand
// funds the reporter-rewards module never reads (it only ever reads its own
// distinct cosmos module account).
func TestValidateFundStrandingExceptionsRejectsUnreviewedCanReceiveUserFunds(t *testing.T) {
	accounts := ReservedSystemModuleAccounts()
	found := false
	for i := range accounts {
		if accounts[i].Name == "AETReporterRewards" {
			require.False(t, accounts[i].CanReceiveUserFunds, "precondition: AETReporterRewards must not already allow user funds")
			accounts[i].CanReceiveUserFunds = true
			found = true
		}
	}
	require.True(t, found, "AETReporterRewards must exist in the reserved module account catalog")

	err := validateFundStrandingExceptions(accounts)
	require.ErrorContains(t, err, "AETReporterRewards")
	require.ErrorContains(t, err, "FINDING-017")
}

// TestValidateFundStrandingExceptionsRejectsUnreviewedNameEvenIfPlausible
// guards against widening fundStrandingReviewedExceptions casually: any name
// not explicitly listed must still fail, regardless of which entity it is.
func TestValidateFundStrandingExceptionsRejectsUnreviewedNameEvenIfPlausible(t *testing.T) {
	accounts := []ReservedSystemModuleAccount{
		{Name: "AETSomeFutureModule", ModuleAccountName: "some-future-module", CanReceiveUserFunds: true},
	}
	err := validateFundStrandingExceptions(accounts)
	require.ErrorContains(t, err, "AETSomeFutureModule")
}
