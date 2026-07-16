package addressing

import (
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	SystemAddressStatusActive	= "active"

	ReservedUserWorkchain	= 4
	ReservedSystemWorkchain	= -7

	SystemAddressAETElectorName		= "AETElector"
	SystemAddressAETConfigName		= "AETConfig"
	SystemAddressAETMintName		= "AETMint"
	SystemAddressAETBurnName		= "AETBurn"
	SystemAddressAETElectorUserFriendly	= "AEAAAQEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEELECTOR"
	SystemAddressAETConfigUserFriendly	= "AEAAAQCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCONFIG"
	SystemAddressAETMintUserFriendly	= "AEAAAQMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMMINT"
	SystemAddressAETBurnUserFriendly	= "AEAAAQBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBURN"
	SystemAddressAETElectorRaw		= "ae1qyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgy93qfxwgsytz7gk"
	SystemAddressAETConfigRaw		= "ae1qzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpr352grqdcrmfc"
	SystemAddressAETMintRaw			= "ae1qvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpssdfsd52leu"
	SystemAddressAETBurnRaw			= "ae1qpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsg9g3xsnus8fg"
)

type SystemAddress struct {
	Name			string
	ModuleName		string
	Raw			string
	UserFriendly		string
	Core			bool
	CanHoldFunds		bool
	// CanReceiveUserFunds allows a plain bank send to Raw/UserFriendly above.
	// For any entity that also has a reserved module account (see
	// app/accounts.ReservedSystemModuleAccount), that module account -- not
	// this catalog address -- is what the module actually reads/writes, so
	// setting this true without review can silently strand user funds at an
	// address the module never looks at (security-audit FINDING-017). See
	// app/accounts.fundStrandingReviewedExceptions for the enforced allowlist.
	CanReceiveUserFunds	bool
	CanSendFunds		bool
	Status			string
}

// Description returns a short, human-facing explanation of what this system
// entity is and does, keyed by module — so an explorer or wallet can label a
// reserved address clearly instead of showing a bare module name. Every
// module in reservedSystemAddresses must have an entry here;
// TestSystemAddressDescriptionsCoverAllModules enforces it.
func (s SystemAddress) Description() string {
	if d, ok := systemAddressDescriptions[s.ModuleName]; ok {
		return d
	}
	return "Aetra system module account."
}

var systemAddressDescriptions = map[string]string{
	"validator-election":    "Elects and rotates the active validator set each epoch.",
	"config":                "Holds the chain's live, governance-adjustable parameters.",
	"constitution":          "Anchors the chain's constitutional rules and amendment history.",
	"system-registry":       "Registry of record for every reserved system entity on the chain.",
	"validator-registry":    "Tracks registered validator operators and their metadata.",
	"config-voting":         "Runs governance votes that change chain parameters.",
	"mint-authority":        "Mints new AET under the chain's emission schedule.",
	"burn":                  "Destination for AET permanently removed from supply.",
	"evidence":              "Records slashing / misbehavior evidence against validators.",
	"reporter":              "Pays rewards to reporters of validated evidence.",
	"nominator-pool":        "Pools nominator (delegator) stake behind validators.",
	"single-nominator-pool": "A single-nominator variant of the staking pool.",
	"validator-insurance":   "Insurance fund covering validator-side slashing risk.",
	"delegator-protection":  "Protects delegators from certain validator-side losses.",
	"reputation":            "Tracks validator reputation scores derived from performance.",
	"performance-oracle":    "Feeds validator performance metrics into the scoring system.",
	"stake-concentration":   "Monitors and discourages excessive stake concentration.",
	"dynamic-commission":    "Adjusts validator commission rates dynamically.",
	"emissions":             "Accounts for the chain's block-reward emission schedule.",
	"fee-collector":         "Collects transaction fees before distribution.",
	"treasury":              "Chain treasury: holds funds for governance-directed spending.",
	"scheduler":             "Schedules deferred / autonomous protocol-level actions.",
	"avm-scheduler":         "Schedules deferred AVM contract message delivery.",
	"actor-registry":        "Registry of on-chain actors participating in protocol modules.",
	"storage-rent":          "Collects contract storage rent and manages frozen / expired contracts.",
	"identity-root":         "Root of the chain's on-chain identity system.",
	"bridge-hub":            "Coordinates cross-chain bridge message routing.",
	"cross-chain-registry":  "Registry of external chains recognized for bridging.",
	"sharding-coordinator":  "Coordinates zone/shard assignment and load distribution.",
}

var reservedSystemAddresses = []SystemAddress{
	systemAddress(SystemAddressAETElectorName, "validator-election", SystemAddressAETElectorRaw, SystemAddressAETElectorUserFriendly, true, false, false, false),
	systemAddress(SystemAddressAETConfigName, "config", SystemAddressAETConfigRaw, SystemAddressAETConfigUserFriendly, true, false, false, false),
	systemAddress("AETConstitution", "constitution", "ae1qdxnf56dxnf56dxnf56dxnf56dxnf56dxnf56dxnf56dxnf56dxsd58wh5", "AEAAAQNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNN", true, false, false, false),
	systemAddress("AETSystemRegistry", "system-registry", "ae1q3g529z3g529z3g529z3g529z3g529z3g529z3g529z3g529z3gs92n6gy", "AEAAAQRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRR", true, false, false, false),
	systemAddress("AETValidatorRegistry", "validator-registry", "ae1q42424242424242424242424242424242424242424242424242sr903zu", "AEAAAQVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVV", true, false, false, false),
	systemAddress("AETConfigVoting", "config-voting", "ae1qxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrqwjq8cx", "AEAAAQGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG", true, false, false, false),

	systemAddress(SystemAddressAETMintName, "mint-authority", SystemAddressAETMintRaw, SystemAddressAETMintUserFriendly, false, false, false, false),
	systemAddress(SystemAddressAETBurnName, "burn", SystemAddressAETBurnRaw, SystemAddressAETBurnUserFriendly, false, false, true, false),
	systemAddress("AETEvidence", "evidence", "ae1qrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpsa6rxg9", "AEAAAQDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD", false, false, false, false),
	systemAddress("AETReporterRewards", "reporter", "ae1q08neu708neu708neu708neu708neu708neu708neu708neu708st94ymj", "AEAAAQPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPP", false, true, false, false),
	// CanHoldFunds=true (was false): the pool now custodies real deposits and
	// real delegations via its cosmos module account (authtypes.NewModuleAddress
	// of the module name below); the reserved catalog address stays distinct
	// and unfunded per the two-layer model, same as AETTreasury/AETStorageRent.
	systemAddress("AETNominatorPool", "nominator-pool", "ae1qw8r3cuw8r3cuw8r3cuw8r3cuw8r3cuw8r3cuw8r3cuw8r3cuw8q7z2wpf", "AEAAAQOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO", false, true, false, false),
	systemAddress("AETSingleNominatorPool", "single-nominator-pool", "ae1qjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfqku767e", "AEAAAQSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS", false, false, false, false),
	systemAddress("AETValidatorInsurance", "validator-insurance", "ae1qgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyq7uy08h", "AEAAAQIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII", false, true, false, false),
	systemAddress("AETDelegatorProtection", "delegator-protection", "ae1qt9jevkt9jevkt9jevkt9jevkt9jevkt9jevkt9jevkt9jevkt9sd2f032", "AEAAAQLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLL", false, true, false, false),
	systemAddress("AETReputation", "reputation", "ae1q529z3g529z3g529z3g529z3g529z3g529z3g529z3g529z3g52qkzsmc8", "AEAAAQUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUUU", false, false, false, false),
	systemAddress("AETPerformanceOracle", "performance-oracle", "ae1q9z3g529z3g529z3g529z3g529z3g529z3g529z3g529z3g529zsayd8wm", "AEAAAQFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF", false, false, false, false),
	systemAddress("AETStakeConcentration", "stake-concentration", "ae1q29z3g529z3g529z3g529z3g529z3g529z3g529z3g529z3g529qcdk9t3", "AEAAAQKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK", false, false, false, false),
	systemAddress("AETDynamicCommission", "dynamic-commission", "ae1qfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfyjfystmm9av", "AEAAAQJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJ", false, false, false, false),
	systemAddress("AETEmissions", "emissions", "ae1qcvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvxrpscvqqaxets", "AEAAAQYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY", false, false, false, false),
	systemAddress("AETFeeCollector", "fee-collector", "ae1qsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgqsdvsjl", "AEAAAQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ", false, true, false, false),
	systemAddress("AETTreasury", "treasury", "ae1qnf56dxnf56dxnf56dxnf56dxnf56dxnf56dxnf56dxnf56dxnfsrmpsyz", "AEAAAQTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTT", false, true, false, false),
	systemAddress("AETScheduler", "scheduler", "ae1q8r3cuw8r3cuw8r3cuw8r3cuw8r3cuw8r3cuw8r3cuw8r3cuw8rsm4ldza", "AEAAAQHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHH", false, false, false, false),
	systemAddress("AETAVMScheduler", "avm-scheduler", "ae1qkt9jevkt9jevkt9jevkt9jevkt9jevkt9jevkt9jevkt9jevktqsnz35p", "AEAAAQWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWW", false, false, false, false),
	systemAddress("AETActorRegistry", "actor-registry", "ae1qht46awht46awht46awht46awht46awht46awht46awht46awhts95amw6", "AEAAAQXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX", false, false, false, false),
	systemAddress("AETStorageRent", "storage-rent", "ae1qevkt9jevkt9jevkt9jevkt9jevkt9jevkt9jevkt9jevkt9jevs46en3t", "AEAAAQZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ", false, true, false, false),
	systemAddress("AETIdentityRoot", "identity-root", "ae1qggjzys3yyfpzggjzys3yyfpzggjzys3yyfpzggjzys3yyfpzggsc5rpwe", "AEAAAQIRIRIRIRIRIRIRIRIRIRIRIRIRIRIRIRIRIRIRIRIR", false, false, false, false),
	systemAddress("AETBridgeHub", "bridge-hub", "ae1qprsguz8q3cywprsguz8q3cywprsguz8q3cywprsguz8q3cywprsmj8vve", "AEAAAQBHBHBHBHBHBHBHBHBHBHBHBHBHBHBHBHBHBHBHBHBH", false, false, false, false),
	systemAddress("AETCrossChainRegistry", "cross-chain-registry", "ae1qzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqsgyzpqgauvj7", "AEAAAQCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC", false, false, false, false),
	systemAddress("AETShardingCoordinator", "sharding-coordinator", "ae1qjr5say8fp6gwjr5say8fp6gwjr5say8fp6gwjr5say8fp6gwjrsxas8ts", "AEAAAQSHSHSHSHSHSHSHSHSHSHSHSHSHSHSHSHSHSHSHSHSH", false, false, false, false),
}

func systemAddress(name, moduleName, raw, userFriendly string, core, canHoldFunds, canReceiveUserFunds, canSendFunds bool) SystemAddress {
	return SystemAddress{
		Name:			name,
		ModuleName:		moduleName,
		Raw:			raw,
		UserFriendly:		userFriendly,
		Core:			core,
		CanHoldFunds:		canHoldFunds,
		CanReceiveUserFunds:	canReceiveUserFunds,
		CanSendFunds:		canSendFunds,
		Status:			SystemAddressStatusActive,
	}
}

func AllSystemAddresses() []SystemAddress {
	out := make([]SystemAddress, len(reservedSystemAddresses))
	copy(out, reservedSystemAddresses)
	return out
}

func ValidateReservedSystemAddressCatalog() error {
	return ValidateSystemAddressCatalog(reservedSystemAddresses)
}

func ValidateSystemAddressCatalog(addresses []SystemAddress) error {
	seenNames := map[string]struct{}{}
	seenModules := map[string]struct{}{}
	seenBytes := map[string]string{}
	for _, address := range addresses {
		if strings.TrimSpace(address.Name) == "" {
			return fmt.Errorf("reserved system address name is required")
		}
		if strings.TrimSpace(address.ModuleName) == "" {
			return fmt.Errorf("reserved system address module is required for %s", address.Name)
		}
		if _, found := seenNames[address.Name]; found {
			return fmt.Errorf("duplicate reserved system address name %s", address.Name)
		}
		seenNames[address.Name] = struct{}{}
		if _, found := seenModules[address.ModuleName]; found {
			return fmt.Errorf("duplicate reserved system address module %s", address.ModuleName)
		}
		seenModules[address.ModuleName] = struct{}{}
		rawBytes, err := Parse(address.Raw)
		if err != nil {
			return fmt.Errorf("reserved system address %s raw address invalid: %w", address.Name, err)
		}
		if IsZero(rawBytes) {
			return fmt.Errorf("reserved system address %s must not use zero address", address.Name)
		}
		userBytes, err := Parse(address.UserFriendly)
		if err != nil {
			return fmt.Errorf("reserved system address %s user-friendly address invalid: %w", address.Name, err)
		}
		if IsZero(userBytes) {
			return fmt.Errorf("reserved system address %s user-friendly address must not be zero address", address.Name)
		}
		rawKey, err := addressTextKey(address.Raw)
		if err != nil {
			return err
		}
		userKey, err := addressTextKey(address.UserFriendly)
		if err != nil {
			return err
		}
		if rawKey != userKey {
			return fmt.Errorf("reserved system address %s raw and AE addresses mismatch", address.Name)
		}
		if other, found := seenBytes[rawKey]; found {
			return fmt.Errorf("duplicate reserved system address bytes used by %s and %s", other, address.Name)
		}
		seenBytes[rawKey] = address.Name
		if address.Status != SystemAddressStatusActive {
			return fmt.Errorf("reserved system address %s has invalid status %q", address.Name, address.Status)
		}
	}
	return nil
}

func SystemAddressByName(name string) (SystemAddress, bool) {
	name = strings.TrimSpace(name)
	for _, address := range reservedSystemAddresses {
		if address.Name == name {
			return address, true
		}
	}
	return SystemAddress{}, false
}

func SystemAddressByRaw(raw string) (SystemAddress, bool) {
	raw = strings.TrimSpace(raw)
	for _, address := range reservedSystemAddresses {
		if address.Raw == raw {
			return address, true
		}
	}
	return SystemAddress{}, false
}

func SystemAddressByUserFriendly(uf string) (SystemAddress, bool) {
	uf = strings.TrimSpace(uf)
	for _, address := range reservedSystemAddresses {
		if address.UserFriendly == uf {
			return address, true
		}
	}
	return SystemAddress{}, false
}

func SystemAddressByBytes(bz []byte) (SystemAddress, bool) {
	key, err := addressBytesKey(bz)
	if err != nil {
		return SystemAddress{}, false
	}
	for _, address := range reservedSystemAddresses {
		addressKey, err := addressTextKey(address.Raw)
		if err != nil {
			return SystemAddress{}, false
		}
		if addressKey == key {
			return address, true
		}
	}
	return SystemAddress{}, false
}

func SystemAddressByText(text string) (SystemAddress, bool) {
	bz, err := Parse(text)
	if err != nil {
		return SystemAddress{}, false
	}
	return SystemAddressByBytes(bz)
}

func IsReservedSystemAddressBytes(bz []byte) bool {
	_, found := SystemAddressByBytes(bz)
	return found
}

func IsReservedSystemAddressText(text string) bool {
	_, found := SystemAddressByText(text)
	return found
}

func ValidateNoUserControlledSystemAddresses(userAccounts []string) error {
	for _, account := range userAccounts {
		if err := ValidateUserSignerAddress(account); err != nil {
			return err
		}
	}
	return nil
}

func ValidateUserSignerAddress(account string) error {
	text := strings.TrimSpace(account)
	if text == "" {
		return nil
	}
	bz, err := Parse(text)
	if err != nil {
		return fmt.Errorf("invalid user-controlled account address %q: %w", text, err)
	}
	if IsZero(bz) {
		return fmt.Errorf("user-controlled account %q must not be zero address", text)
	}
	if IsReservedSystemAddressBytes(bz) {
		return fmt.Errorf("user-controlled account %q uses reserved system address", text)
	}
	return nil
}

func ValidateUserRecipientAddress(account string) error {
	text := strings.TrimSpace(account)
	if text == "" {
		return nil
	}
	bz, err := Parse(text)
	if err != nil {
		return fmt.Errorf("invalid user recipient address %q: %w", text, err)
	}
	if IsZero(bz) {
		return fmt.Errorf("user recipient %q must not be zero address", text)
	}
	address, found := SystemAddressByBytes(bz)
	if found && !address.CanReceiveUserFunds {
		return fmt.Errorf("user recipient %q is reserved system address and cannot receive user funds", text)
	}
	return nil
}

func ValidateNewUserAccountAddress(field, text string) error {
	if err := ValidateUserAddress(field, text); err != nil {
		return err
	}
	if IsReservedSystemAddressText(text) {
		return fmt.Errorf("%s must not use reserved system address", field)
	}
	return nil
}

func ValidateUserAdminAddress(field, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if err := ValidateUserAddress(field, text); err != nil {
		return err
	}
	if IsReservedSystemAddressText(text) {
		return fmt.Errorf("%s must not use reserved system address", field)
	}
	return nil
}

func ValidateTxAuthorityAddress(field, text string) error {
	if err := ValidateAuthorityAddress(field, text); err != nil {
		return err
	}
	if IsReservedSystemAddressText(text) {
		return fmt.Errorf("%s must not use reserved system address", field)
	}
	return nil
}

func SystemAddressBytesKey(address SystemAddress) (string, error) {
	return addressTextKey(address.Raw)
}

func AddressTextBytesKey(text string) (string, error) {
	return addressTextKey(text)
}

func addressTextKey(text string) (string, error) {
	bz, err := Parse(text)
	if err != nil {
		return "", err
	}
	return addressBytesKey(bz)
}

func addressBytesKey(bz []byte) (string, error) {
	raw, err := ToRawPayload(bz)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
