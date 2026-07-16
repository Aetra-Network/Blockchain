package accounts

import (
	"fmt"
	"maps"
	"slices"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	protocolpooltypes "github.com/cosmos/cosmos-sdk/x/protocolpool/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	burntypes "github.com/sovereign-l1/l1/x/burn/types"
	configtypes "github.com/sovereign-l1/l1/x/config/types"
	delegatorprotectiontypes "github.com/sovereign-l1/l1/x/delegator-protection/types"
	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	mintauthoritytypes "github.com/sovereign-l1/l1/x/mint-authority/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
	systemregistrytypes "github.com/sovereign-l1/l1/x/system-registry/types"
	treasurytypes "github.com/sovereign-l1/l1/x/treasury/types"
	validatorelectiontypes "github.com/sovereign-l1/l1/x/validator-election/types"
	validatorinsurancetypes "github.com/sovereign-l1/l1/x/validator-insurance/types"
)

type ReservedSystemModuleAccount struct {
	Name			string
	ModuleName		string
	ModuleAccountName	string
	Raw			string
	UserFriendly		string
	Core			bool
	CanHoldFunds		bool
	// CanReceiveUserFunds allows a plain bank send to this entity's reserved
	// catalog ("vanity") address, Raw/UserFriendly above. That address is
	// deliberately distinct from the cosmos module account this entity's
	// module actually reads/writes (authtypes.NewModuleAddress(ModuleAccountName);
	// see the "two-layer address model" documented on
	// TestReservedSystemModuleAccountsBackedByLiveModuleAccounts in
	// app/system_module_accounts_test.go). Coins sent to the catalog address
	// are therefore NOT visible to the module's own SendCoinsFromModule*/
	// BurnCoins accounting -- they simply sit there, unspendable, forever.
	// Setting this true for a fund-bearing module without confirming the
	// module never needs to read that balance is security-audit FINDING-017
	// (a fund-loss footgun found on AETReporterRewards); see
	// fundStrandingReviewedExceptions, which validateFundStrandingExceptions
	// enforces via ValidateReservedSystemModuleAccountWiring.
	CanReceiveUserFunds	bool
	CanSendFunds		bool
	Permissions		[]string
}

// fundStrandingReviewedExceptions lists reserved system entities where
// CanReceiveUserFunds=true is a deliberate, reviewed exception despite the
// entity's real funds being custodied by a distinct cosmos module account
// (see the CanReceiveUserFunds doc comment above). AETBurn is the only
// current member: a plain send to the AETBurn catalog address lands coins at
// an address nobody holds the key for, which is economically indistinguishable
// from an on-chain burn even though the x/burn module's own accounting never
// reads it -- TestReservedSystemBankSendPolicy (app/system_module_accounts_test.go)
// exercises this on purpose.
//
// Do not add a new name here without the same reasoning: the destination
// module must have no need to ever read balance received at the catalog
// address, or user funds sent there (e.g. expecting a rewards pool to later
// pay them out) will be silently and permanently stranded.
var fundStrandingReviewedExceptions = map[string]bool{
	"AETBurn": true,
}

// validateFundStrandingExceptions enforces that CanReceiveUserFunds=true is
// only set for a reserved module account when it is an explicitly reviewed
// exception. It is factored out of ValidateReservedSystemModuleAccountWiring
// so the FINDING-017 regression tests can exercise it against synthetic
// accounts without mutating the real address catalog.
func validateFundStrandingExceptions(accounts []ReservedSystemModuleAccount) error {
	for _, account := range accounts {
		if account.CanReceiveUserFunds && !fundStrandingReviewedExceptions[account.Name] {
			return fmt.Errorf("reserved module account %s allows direct user sends to its reserved catalog address but is not a reviewed fund-stranding exception: its funds are custodied by the distinct module account %q, which a plain bank send to the catalog address would never reach (security-audit FINDING-017)", account.Name, account.ModuleAccountName)
		}
	}
	return nil
}

var moduleAccountPermissions = map[string][]string{
	authtypes.FeeCollectorName:			nil,
	distrtypes.ModuleName:				nil,
	minttypes.ModuleName:				{authtypes.Minter},
	stakingtypes.BondedPoolName:			{authtypes.Burner, authtypes.Staking},
	stakingtypes.NotBondedPoolName:			{authtypes.Burner, authtypes.Staking},
	govtypes.ModuleName:				{authtypes.Burner},
	protocolpooltypes.ModuleName:			nil,
	protocolpooltypes.ProtocolPoolEscrowAccount:	nil,
	burntypes.ModuleName:				{authtypes.Burner},
	feecollectortypes.CollectorModuleName:		{authtypes.Burner},
	feecollectortypes.TreasuryModuleName:		nil,
	feecollectortypes.ProtectionModuleName:		nil,
	feecollectortypes.ValidatorInsuranceModuleName:	nil,
	feecollectortypes.EcosystemGrantsModuleName:	nil,
	feecollectortypes.StorageRentReserveModuleName:	nil,
	feecollectortypes.BurnModuleName:		nil,
	feecollectortypes.ReporterRewardsModuleName:	nil,
	mintauthoritytypes.ModuleName:			{authtypes.Minter},
	// x/storage-rent, x/delegator-protection and x/validator-insurance
	// deliberately have no module account of their own. None of them custodies
	// coins: storage-rent only moves rent from the payer to the
	// feecollector_storage_rent_reserve bucket above, and the other two hold no
	// bankKeeper at all -- their reserves are the feecollector_protection and
	// feecollector_validator_insurance buckets that
	// DefaultProtocolIncomePolicy credits. Their accounts existed only because
	// the reserved catalog used to name them as custodians; giving them one
	// back now also trips the wiring gate in app/aetra_core_wiring.go, which
	// forbids module account permissions for a prototype or system module that
	// is not a reserved custodian.
	configtypes.ModuleName:				nil,
	systemregistrytypes.ModuleName:			nil,
	validatorelectiontypes.ModuleName:		nil,
	feestypes.ModuleName:				nil,
	// x/nominator-pool now custodies real deposits and delegates them to
	// validators via x/staking (previously a bookkeeping-only ledger with no
	// bank custody). No special permission is needed: x/staking's Delegate
	// path debits the delegator's spendable balance directly
	// (bankKeeper.DelegateCoinsFromAccountToModule targets the BONDED pool,
	// which already holds authtypes.Staking -- the delegator side needs
	// nothing special), and x/distribution reward withdrawal is the same.
	nominatorpooltypes.ModuleName:			nil,
}

var reservedSystemModuleAccountSpecs = []struct {
	addressName		string
	moduleName		string
	moduleAccountName	string
	permissions		[]string
}{
	{"AETMint", mintauthoritytypes.ModuleName, mintauthoritytypes.DefaultMintAuthorityModuleAccount, []string{authtypes.Minter}},
	{"AETFeeCollector", "fee-collector", feecollectortypes.CollectorModuleName, []string{authtypes.Burner}},
	{"AETTreasury", treasurytypes.ModuleName, treasurytypes.TreasuryModuleName, nil},
	{"AETBurn", burntypes.ModuleName, burntypes.ModuleName, []string{authtypes.Burner}},
	// The custodian of each reserve is the fee-collector bucket that
	// DefaultProtocolIncomePolicy actually credits (x/fee-collector
	// protocol_income.go), not the owning module's own account. x/storage-rent
	// and x/contracts send rent to feecollector_storage_rent_reserve, and
	// x/delegator-protection and x/validator-insurance hold no bankKeeper at
	// all, so their own module accounts never custody a single coin. Naming
	// the owning module here instead would have made CanHoldFunds=true point
	// at a permanently empty account -- the same two-layer confusion as
	// FINDING-017. AETReporterRewards below already follows this pattern.
	{"AETStorageRent", "storage-rent", feecollectortypes.StorageRentReserveModuleName, nil},
	{"AETDelegatorProtection", delegatorprotectiontypes.ModuleName, feecollectortypes.ProtectionModuleName, nil},
	{"AETValidatorInsurance", validatorinsurancetypes.ModuleName, feecollectortypes.ValidatorInsuranceModuleName, nil},
	{"AETReporterRewards", "reporter", feecollectortypes.ReporterRewardsModuleName, nil},
	{"AETConfig", configtypes.ModuleName, configtypes.ModuleName, nil},
	{"AETSystemRegistry", systemregistrytypes.ModuleName, systemregistrytypes.ModuleName, nil},
	{"AETElector", validatorelectiontypes.ModuleName, validatorelectiontypes.ModuleName, nil},
	// Unlike storage-rent/delegator-protection/validator-insurance above, the
	// nominator-pool module now holds its own real bankKeeper and is its own
	// custodian: deposits and delegation-derived stake live at
	// authtypes.NewModuleAddress(nominatorpooltypes.ModuleName) directly, not
	// in a fee-collector reserve bucket.
	{"AETNominatorPool", nominatorpooltypes.ModuleName, nominatorpooltypes.ModuleName, nil},
}

func ModuleAccountPermissions() map[string][]string {
	return maps.Clone(moduleAccountPermissions)
}

func BlockedAddresses() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range ModuleAccountPermissions() {
		modAccAddrs[authtypes.NewModuleAddress(acc).String()] = true
	}
	for _, address := range aetraaddress.AllSystemAddresses() {
		bz, err := aetraaddress.Parse(address.Raw)
		if err != nil {
			panic(fmt.Errorf("invalid reserved system address %s: %w", address.Name, err))
		}
		key := sdk.AccAddress(bz).String()
		if address.CanReceiveUserFunds {
			delete(modAccAddrs, key)
			continue
		}
		modAccAddrs[key] = true
	}

	delete(modAccAddrs, authtypes.NewModuleAddress(govtypes.ModuleName).String())

	return modAccAddrs
}

func ReservedSystemModuleAccounts() []ReservedSystemModuleAccount {
	out := make([]ReservedSystemModuleAccount, 0, len(reservedSystemModuleAccountSpecs))
	for _, spec := range reservedSystemModuleAccountSpecs {
		address, found := aetraaddress.SystemAddressByName(spec.addressName)
		if !found {
			panic(fmt.Sprintf("reserved system address %s is not registered", spec.addressName))
		}
		out = append(out, ReservedSystemModuleAccount{
			Name:			address.Name,
			ModuleName:		spec.moduleName,
			ModuleAccountName:	spec.moduleAccountName,
			Raw:			address.Raw,
			UserFriendly:		address.UserFriendly,
			Core:			address.Core,
			CanHoldFunds:		address.CanHoldFunds,
			CanReceiveUserFunds:	address.CanReceiveUserFunds,
			CanSendFunds:		address.CanSendFunds,
			Permissions:		append([]string(nil), spec.permissions...),
		})
	}
	return out
}

func ReservedSystemModuleAccountByName(name string) (ReservedSystemModuleAccount, bool) {
	for _, account := range ReservedSystemModuleAccounts() {
		if account.Name == name {
			return account, true
		}
	}
	return ReservedSystemModuleAccount{}, false
}

func ReservedSystemModuleAccountByModuleAccountName(moduleAccountName string) (ReservedSystemModuleAccount, bool) {
	for _, account := range ReservedSystemModuleAccounts() {
		if account.ModuleAccountName == moduleAccountName {
			return account, true
		}
	}
	return ReservedSystemModuleAccount{}, false
}

func IsReservedSystemModuleAccountName(moduleAccountName string) bool {
	_, found := ReservedSystemModuleAccountByModuleAccountName(moduleAccountName)
	return found
}

func ReservedSystemModuleAccountAddress(moduleAccountName string) (sdk.AccAddress, bool, error) {
	account, found := ReservedSystemModuleAccountByModuleAccountName(moduleAccountName)
	if !found {
		return nil, false, nil
	}
	bz, err := aetraaddress.Parse(account.Raw)
	if err != nil {
		return nil, true, err
	}
	return sdk.AccAddress(bz), true, nil
}

func ValidateReservedSystemModuleAccountWiring(blocked map[string]bool) error {
	seen := map[string]string{}
	for _, address := range aetraaddress.AllSystemAddresses() {
		bz, err := aetraaddress.Parse(address.Raw)
		if err != nil {
			return fmt.Errorf("reserved system address %s raw address invalid: %w", address.Name, err)
		}
		if aetraaddress.IsZero(bz) {
			return fmt.Errorf("reserved system address %s must not use zero address", address.Name)
		}
		key := sdk.AccAddress(bz).String()
		if other, found := seen[key]; found {
			return fmt.Errorf("reserved system address %s duplicates address with %s", address.Name, other)
		}
		seen[key] = address.Name
		if blocked[key] != !address.CanReceiveUserFunds {
			return fmt.Errorf("reserved system address %s blocked policy mismatch", address.Name)
		}
	}

	for _, account := range ReservedSystemModuleAccounts() {
		address, found := aetraaddress.SystemAddressByName(account.Name)
		if !found {
			return fmt.Errorf("reserved module account %s is missing address catalog entry", account.Name)
		}
		if account.Raw != address.Raw || account.UserFriendly != address.UserFriendly {
			return fmt.Errorf("reserved module account %s address mismatch", account.Name)
		}
		// SA2: the spec's owning-module name must match the catalog too, otherwise
		// the two layers can drift invisibly (AETReporterRewards read "reporter"
		// in the catalog but "reporter-rewards" in the spec). Harmless today only
		// because nothing consumes ModuleName — exactly the two-layer confusion
		// class that produced FINDING-017.
		if account.ModuleName != address.ModuleName {
			return fmt.Errorf("reserved module account %s module-name mismatch: spec %q vs catalog %q", account.Name, account.ModuleName, address.ModuleName)
		}
		if account.Core != address.Core || account.CanHoldFunds != address.CanHoldFunds ||
			account.CanReceiveUserFunds != address.CanReceiveUserFunds || account.CanSendFunds != address.CanSendFunds {
			return fmt.Errorf("reserved module account %s policy mismatch", account.Name)
		}
		if permissions, found := moduleAccountPermissions[account.ModuleAccountName]; !found {
			return fmt.Errorf("reserved module account %s missing macc permission entry %s", account.Name, account.ModuleAccountName)
		} else if !sameStringSet(permissions, account.Permissions) {
			return fmt.Errorf("reserved module account %s permission mismatch", account.Name)
		}
		bz, err := aetraaddress.Parse(account.Raw)
		if err != nil {
			return fmt.Errorf("reserved module account %s raw address invalid: %w", account.Name, err)
		}
		if aetraaddress.IsZero(bz) {
			return fmt.Errorf("reserved module account %s must not use zero address", account.Name)
		}
		key := sdk.AccAddress(bz).String()
		if blocked[key] != !account.CanReceiveUserFunds {
			return fmt.Errorf("reserved module account %s blocked policy mismatch", account.Name)
		}
	}
	if mint, found := ReservedSystemModuleAccountByName("AETMint"); !found ||
		mint.ModuleAccountName != mintauthoritytypes.DefaultMintAuthorityModuleAccount ||
		mint.Raw != "ae1qvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpssdfsd52leu" {
		return fmt.Errorf("mint authority address must be AETMint")
	}
	if burn, found := ReservedSystemModuleAccountByName("AETBurn"); !found ||
		burn.ModuleAccountName != burntypes.ModuleName ||
		burn.Raw != "ae1qpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsg9g3xsnus8fg" {
		return fmt.Errorf("burn sink address must be AETBurn")
	}
	if err := validateFundStrandingExceptions(ReservedSystemModuleAccounts()); err != nil {
		return err
	}
	return nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for _, value := range left {
		if !slices.Contains(right, value) {
			return false
		}
	}
	return true
}
