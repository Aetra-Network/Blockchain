package addressing_test

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	"github.com/sovereign-l1/l1/app/addressing"
)

type policyTx struct{ msgs []sdk.Msg }

func (tx policyTx) GetMsgs() []sdk.Msg	{ return tx.msgs }
func (tx policyTx) GetMsgsV2() ([]protov2.Message, error) {
	out := make([]protov2.Message, 0, len(tx.msgs))
	for _, msg := range tx.msgs {
		msgV2, ok := msg.(protov2.Message)
		if !ok {
			return nil, nil
		}
		out = append(out, msgV2)
	}
	return out, nil
}

func TestReservedSystemAddressesParseAndMatch(t *testing.T) {
	require.NoError(t, addressing.ValidateReservedSystemAddressCatalog())

	seenNames := map[string]struct{}{}
	seenBytes := map[string]string{}

	for _, address := range addressing.AllSystemAddresses() {
		t.Run(address.Name, func(t *testing.T) {
			require.Equal(t, addressing.SystemAddressStatusActive, address.Status)
			require.Equal(t, strings.ToUpper(address.UserFriendly), address.UserFriendly)

			rawBytes, err := addressing.Parse(address.Raw)
			require.NoError(t, err)
			ufBytes, err := addressing.Parse(address.UserFriendly)
			require.NoError(t, err)

			rawKey, err := addressing.AddressTextBytesKey(address.Raw)
			require.NoError(t, err)
			ufKey, err := addressing.AddressTextBytesKey(address.UserFriendly)
			require.NoError(t, err)
			require.Equal(t, rawKey, ufKey)
			require.Equal(t, rawBytes, ufBytes)
			require.True(t, addressing.IsReservedSystemAddressBytes(rawBytes))
			require.True(t, addressing.IsReservedSystemAddressText(address.Raw))
			require.True(t, addressing.IsReservedSystemAddressText(address.UserFriendly))

			_, duplicateName := seenNames[address.Name]
			require.False(t, duplicateName, "duplicate reserved system address name %s", address.Name)
			seenNames[address.Name] = struct{}{}

			if other, duplicateBytes := seenBytes[rawKey]; duplicateBytes {
				t.Fatalf("duplicate reserved system address bytes used by %s and %s", other, address.Name)
			}
			seenBytes[rawKey] = address.Name
		})
	}
}

func TestReservedSystemAddressVanitySuffixes(t *testing.T) {
	require.Equal(t, 4, addressing.ReservedUserWorkchain)
	require.Equal(t, -7, addressing.ReservedSystemWorkchain)
	require.True(t, strings.HasPrefix(addressing.SystemAddressAETElectorRaw, "ae1"))

	elector, found := addressing.SystemAddressByName(addressing.SystemAddressAETElectorName)
	require.True(t, found)
	require.Equal(t, addressing.SystemAddressAETElectorUserFriendly, elector.UserFriendly)
	require.True(t, strings.HasSuffix(elector.UserFriendly, "ELECTOR"))

	config, found := addressing.SystemAddressByName(addressing.SystemAddressAETConfigName)
	require.True(t, found)
	require.Equal(t, addressing.SystemAddressAETConfigUserFriendly, config.UserFriendly)
	require.True(t, strings.HasSuffix(config.UserFriendly, "CONFIG"))

	mint, found := addressing.SystemAddressByName(addressing.SystemAddressAETMintName)
	require.True(t, found)
	require.Equal(t, addressing.SystemAddressAETMintUserFriendly, mint.UserFriendly)
	require.True(t, strings.HasSuffix(mint.UserFriendly, "MINT"))

	burn, found := addressing.SystemAddressByName(addressing.SystemAddressAETBurnName)
	require.True(t, found)
	require.Equal(t, addressing.SystemAddressAETBurnUserFriendly, burn.UserFriendly)
	require.True(t, strings.HasSuffix(burn.UserFriendly, "BURN"))
}

func TestReservedSystemAddressSignerAndRecipientPolicy(t *testing.T) {
	mint, found := addressing.SystemAddressByName(addressing.SystemAddressAETMintName)
	require.True(t, found)
	require.ErrorContains(t, addressing.ValidateUserSignerAddress(mint.Raw), "reserved system address")
	require.ErrorContains(t, addressing.ValidateUserRecipientAddress(mint.Raw), "cannot receive user funds")

	burn, found := addressing.SystemAddressByName(addressing.SystemAddressAETBurnName)
	require.True(t, found)
	require.ErrorContains(t, addressing.ValidateUserSignerAddress(burn.UserFriendly), "reserved system address")
	require.NoError(t, addressing.ValidateUserRecipientAddress(burn.UserFriendly))
}

func TestZeroAddressPolicyRejectsSignerRecipientAdminAuthority(t *testing.T) {
	require.ErrorContains(t, addressing.ValidateUserSignerAddress(addressing.ZeroRawAddress), "zero address")
	require.ErrorContains(t, addressing.ValidateUserRecipientAddress(addressing.ZeroUserFriendly), "zero address")
	require.ErrorContains(t, addressing.ValidateUserAdminAddress("admin", addressing.ZeroUserFriendly), "zero address")
	require.ErrorContains(t, addressing.ValidateTxAuthorityAddress("authority", addressing.ZeroRawAddress), "zero address")
	require.ErrorContains(t, addressing.ValidateNewUserAccountAddress("activation", addressing.ZeroUserFriendly), "zero address")
}

func TestReservedAddressPolicyRejectsUserCreationAndAnteRoles(t *testing.T) {
	mint, found := addressing.SystemAddressByName(addressing.SystemAddressAETMintName)
	require.True(t, found)
	require.ErrorContains(t, addressing.ValidateNewUserAccountAddress("activation", mint.UserFriendly), "reserved system address")
	require.ErrorContains(t, addressing.ValidateUserAdminAddress("admin", mint.UserFriendly), "reserved system address")
	require.ErrorContains(t, addressing.ValidateTxAuthorityAddress("authority", mint.Raw), "reserved system address")

	err := addressing.ValidateAnteAddressPolicy(policyTx{msgs: []sdk.Msg{&banktypes.MsgSend{
		FromAddress:	mint.UserFriendly,
		ToAddress:	addressing.SystemAddressAETBurnUserFriendly,
		Amount:		sdk.NewCoins(sdk.NewInt64Coin("naet", 1)),
	}}})
	require.ErrorContains(t, err, "reserved system address")
}

// TestAnteAddressPolicyCatchesReservedSignerNestedInAuthzExec is the
// regression test for FINDING-014's app/addressing call site: before the
// fix, ValidateAnteAddressPolicy only reflected over tx.GetMsgs() directly,
// so a reserved-address message wrapped inside an authz.MsgExec was
// invisible to it -- reflection cannot decode MsgExec.Msgs' opaque
// Any.Value. Walking through GetMessages() first (WalkMessages) hands the
// reflective validator a concrete, typed message even when it originated
// inside a MsgExec, so the same policy now applies to nested messages too.
func TestAnteAddressPolicyCatchesReservedSignerNestedInAuthzExec(t *testing.T) {
	mint, found := addressing.SystemAddressByName(addressing.SystemAddressAETMintName)
	require.True(t, found)

	inner := &banktypes.MsgSend{
		FromAddress:	mint.UserFriendly,
		ToAddress:	addressing.SystemAddressAETBurnUserFriendly,
		Amount:		sdk.NewCoins(sdk.NewInt64Coin("naet", 1)),
	}
	grantee := sdk.AccAddress(bytes20(0x09))
	execMsg := authz.NewMsgExec(grantee, []sdk.Msg{inner})

	err := addressing.ValidateAnteAddressPolicy(policyTx{msgs: []sdk.Msg{&execMsg}})
	require.ErrorContains(t, err, "reserved system address")
}

// TestAnteAddressPolicyCatchesZeroRecipientNestedInAuthzExec is the
// companion regression test using a zero-address recipient (the specific
// self-harm scenario called out in FINDING-014's residual-risk analysis)
// instead of a reserved system address.
func TestAnteAddressPolicyCatchesZeroRecipientNestedInAuthzExec(t *testing.T) {
	sender := addressing.FormatAccAddress(sdk.AccAddress(bytes20(0x0a)))
	inner := &banktypes.MsgSend{
		FromAddress:	sender,
		ToAddress:	addressing.ZeroUserFriendly,
		Amount:		sdk.NewCoins(sdk.NewInt64Coin("naet", 1)),
	}
	grantee := sdk.AccAddress(bytes20(0x0b))
	execMsg := authz.NewMsgExec(grantee, []sdk.Msg{inner})

	err := addressing.ValidateAnteAddressPolicy(policyTx{msgs: []sdk.Msg{&execMsg}})
	require.ErrorContains(t, err, "zero address")
}

func TestReservedSystemAddressCatalogRejectsDuplicateAndZeroFixtures(t *testing.T) {
	addresses := addressing.AllSystemAddresses()
	duplicate := append([]addressing.SystemAddress(nil), addresses...)
	duplicate = append(duplicate, addresses[0])
	require.ErrorContains(t, addressing.ValidateSystemAddressCatalog(duplicate), "duplicate reserved system address")

	zero := append([]addressing.SystemAddress(nil), addresses...)
	zero[0].Raw = addressing.ZeroRawAddress
	zero[0].UserFriendly = addressing.ZeroUserFriendly
	require.ErrorContains(t, addressing.ValidateSystemAddressCatalog(zero), "zero address")
}

// TestSystemAddressDescriptionsCoverAllModules guards against a silent
// generic fallback: every reserved system entity must have its own
// human-facing Description so an explorer/wallet never shows the bare
// "Aetra system module account." placeholder for a real entity.
func TestSystemAddressDescriptionsCoverAllModules(t *testing.T) {
	for _, addr := range addressing.AllSystemAddresses() {
		desc := addr.Description()
		require.NotEqual(t, "Aetra system module account.", desc,
			"module %q (%s) has no specific description", addr.ModuleName, addr.Name)
		require.NotEmpty(t, desc)
	}
}
