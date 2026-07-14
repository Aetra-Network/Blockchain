package appconfig

import (
	clienthelpers "cosmossdk.io/client/v2/helpers"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
)

const AppName = appparams.ChainName

const (
	// SDKBech32AccountPrefix is a Cosmos SDK compatibility prefix only.
	// User-facing Aetra addresses use app/addressing's AE... base64url format.
	SDKBech32AccountPrefix	= "ae"
	BondDenom		= appparams.BaseDenom
)

const (
	AccountAddressPrefix	= addressing.UserFriendlyPrefix
	ValidatorAddressPrefix	= addressing.UserFriendlyPrefix
	ConsensusAddressPrefix	= addressing.UserFriendlyPrefix
)

const (
	sdkBech32ValidatorPrefix	= SDKBech32AccountPrefix
	sdkBech32ConsensusPrefix	= SDKBech32AccountPrefix
)

func ConfigureSDK(homeName string) string {
	nodeHome, err := clienthelpers.GetNodeHomeDirectory(homeName)
	if err != nil {
		panic(err)
	}
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(SDKBech32AccountPrefix, SDKBech32AccountPrefix+"pub")
	cfg.SetBech32PrefixForValidator(sdkBech32ValidatorPrefix, sdkBech32ValidatorPrefix+"pub")
	cfg.SetBech32PrefixForConsensusNode(sdkBech32ConsensusPrefix, sdkBech32ConsensusPrefix+"pub")
	// Restrict every SDK-native bech32 address parse (not just this repo's own
	// addressing.Parse) to this chain's two legal address widths, closing the
	// gap where sdk.VerifyAddressFormat's default check only rejects length 0
	// or > 255 (FINDING-012).
	cfg.SetAddressVerifier(addressing.VerifyAddressBytes)
	sdk.DefaultBondDenom = BondDenom
	return nodeHome
}
