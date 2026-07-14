package appconfig

import (
	"fmt"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
)

func TestConfigureSDKSetsSDKBech32CompatibilityAndBondDenom(t *testing.T) {
	home := ConfigureSDK(".aetra")

	require.True(t, strings.HasSuffix(home, ".aetra"), home)
	require.Equal(t, SDKBech32AccountPrefix, sdk.GetConfig().GetBech32AccountAddrPrefix())
	require.Equal(t, SDKBech32AccountPrefix, sdk.GetConfig().GetBech32ValidatorAddrPrefix())
	require.Equal(t, SDKBech32AccountPrefix, sdk.GetConfig().GetBech32ConsensusAddrPrefix())
	require.Equal(t, appparams.BaseDenom, sdk.DefaultBondDenom)
}

// TestConfigureSDKSetsAddressVerifierRejectingNonCanonicalLengths is the
// regression test for FINDING-012's second remediation step: the SDK's
// global address verifier must be wired to this chain's 20-or-32-byte
// invariant, not left at the SDK default (which only rejects length 0 or
// > 255). This protects any SDK-native bech32 address parsing (e.g.
// sdk.AccAddressFromBech32), not just this repo's own addressing.Parse.
func TestConfigureSDKSetsAddressVerifierRejectingNonCanonicalLengths(t *testing.T) {
	ConfigureSDK(".aetra")

	require.NoError(t, sdk.VerifyAddressFormat(make([]byte, 20)), "20-byte plain accounts must remain valid")
	require.NoError(t, sdk.VerifyAddressFormat(make([]byte, 32)), "32-byte v2 identities must remain valid")

	for _, n := range []int{0, 1, 19, 21, 31, 33, 64, 255} {
		err := sdk.VerifyAddressFormat(make([]byte, n))
		require.Errorf(t, err, "address length %d must be rejected by the configured verifier", n)
	}
}

func TestUserFacingAddressFormatIsAEBase64URLNotSDKBech32(t *testing.T) {
	addr := sdk.AccAddress([]byte{
		0x01, 0x02, 0x03, 0x04, 0x05,
		0x06, 0x07, 0x08, 0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14,
	})

	userFacing, err := addressing.FormatUserFriendly(addr)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(userFacing, "AE"))
	require.Regexp(t, fmt.Sprintf(`^[A-Za-z0-9_-]{%d}$`, addressing.UserFriendlyLength), userFacing)
	require.False(t, strings.HasPrefix(userFacing, SDKBech32AccountPrefix+"1"))
	require.Equal(t, "AE", AccountAddressPrefix)
	require.Equal(t, "AE", ValidatorAddressPrefix)
	require.Equal(t, "AE", ConsensusAddressPrefix)
}
