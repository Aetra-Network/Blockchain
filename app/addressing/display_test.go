package addressing_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
)

func TestInspectAddressWarnsForNewRecipientAndShowsChecksum(t *testing.T) {
	recipient := addressing.FormatAccAddress(bytes20(0x61))

	display, err := addressing.InspectAddress(recipient, addressing.AddressInspectionContext{
		ChainID:               "aetra-local-1",
		IncludeSystemContacts: true,
	})
	require.NoError(t, err)
	require.Equal(t, recipient, display.UserFriendly)
	require.Regexp(t, `^4:[0-9a-f]{64}$`, display.Raw)
	require.Equal(t, addressing.ShortenAddress(recipient), display.Short)
	require.True(t, strings.HasPrefix(display.ChainBoundChecksum, "aetra-local-1:"))
	require.False(t, display.Known)
	requireWarning(t, display.Warnings, addressing.AddressWarningNewAddress)
}

func TestInspectAddressOnlyVerifiesOnChainOrSignedLabels(t *testing.T) {
	localAddr := addressing.FormatAccAddress(bytes20(0x62))
	signedAddr := addressing.FormatAccAddress(bytes20(0x63))
	book := []addressing.AddressBookEntry{
		{Address: localAddr, Label: "Alice", LabelSource: addressing.LabelSourceLocal},
		{Address: signedAddr, Label: "Treasury Ops", LabelSource: addressing.LabelSourceSignedAttestation, Attestation: "sig:example"},
	}

	local, err := addressing.InspectAddress(localAddr, addressing.AddressInspectionContext{ChainID: "aetra-local-1", AddressBook: book})
	require.NoError(t, err)
	require.True(t, local.Known)
	require.Equal(t, "Alice", local.LocalLabel)
	require.Empty(t, local.VerifiedLabel)
	requireWarning(t, local.Warnings, addressing.AddressWarningUnverifiedLabel)

	signed, err := addressing.InspectAddress(signedAddr, addressing.AddressInspectionContext{ChainID: "aetra-local-1", AddressBook: book})
	require.NoError(t, err)
	require.True(t, signed.Known)
	require.Equal(t, "Treasury Ops", signed.VerifiedLabel)
	requireNoWarning(t, signed.Warnings, addressing.AddressWarningUnverifiedLabel)
}

func TestInspectAddressDetectsShortCollisionAndLookalike(t *testing.T) {
	known := addressing.FormatAccAddress(bytes20(0x44))
	collidingAddr := oneCharAddressLookalike(t, known)
	require.Equal(t, addressing.ShortenAddress(known), addressing.ShortenAddress(collidingAddr))

	display, err := addressing.InspectAddress(collidingAddr, addressing.AddressInspectionContext{
		AddressBook: []addressing.AddressBookEntry{{
			Address:     known,
			Label:       "Known Receiver",
			LabelSource: addressing.LabelSourceOnChain,
		}},
	})
	require.NoError(t, err)
	requireWarning(t, display.Warnings, addressing.AddressWarningPrefixSuffixMatch)

	display, err = addressing.InspectAddress(collidingAddr, addressing.AddressInspectionContext{
		AddressBook: []addressing.AddressBookEntry{{
			Address:     known,
			Label:       "Known Receiver",
			LabelSource: addressing.LabelSourceOnChain,
		}},
	})
	require.NoError(t, err)
	requireWarning(t, display.Warnings, addressing.AddressWarningLookalike)
}

func TestInspectAddressDetectsRecentAndConfusableLabels(t *testing.T) {
	target := addressing.FormatAccAddress(bytes20(0x71))
	other := addressing.FormatAccAddress(bytes20(0x72))
	book := []addressing.AddressBookEntry{
		{Address: target, Label: "A1ice", LabelSource: addressing.LabelSourceLocal, CreatedHeight: 95, LastSeenHeight: 100},
		{Address: other, Label: "Alice", LabelSource: addressing.LabelSourceOnChain},
	}

	display, err := addressing.InspectAddress(target, addressing.AddressInspectionContext{
		CurrentHeight:       100,
		RecentAddressWindow: 10,
		AddressBook:         book,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(95), display.CreatedHeight)
	requireWarning(t, display.Warnings, addressing.AddressWarningRecentlyCreated)
	requireWarning(t, display.Warnings, addressing.AddressWarningConfusableLabel)
}

func TestAddressBookEntryRequiresAttestationForSignedLabel(t *testing.T) {
	addr := addressing.FormatAccAddress(bytes20(0x73))

	_, err := addressing.NewAddressBookEntry("Ops", addr, addressing.LabelSourceSignedAttestation, "", 0, 0)
	require.ErrorContains(t, err, "attestation")

	entry, err := addressing.NewAddressBookEntry("Ops", addr, addressing.LabelSourceSignedAttestation, "sig:example", 10, 11)
	require.NoError(t, err)
	require.Equal(t, addressing.LabelSourceSignedAttestation, entry.LabelSource)
}

func oneCharAddressLookalike(t *testing.T, address string) string {
	t.Helper()
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	for i := 12; i < len(address)-8; i++ {
		original := address[i]
		for j := 0; j < len(alphabet); j++ {
			if alphabet[j] == original {
				continue
			}
			candidate := address[:i] + string(alphabet[j]) + address[i+1:]
			if _, err := addressing.Parse(candidate); err == nil {
				return candidate
			}
		}
	}
	t.Fatalf("failed to build address lookalike for %s", address)
	return ""
}

func requireWarning(t *testing.T, warnings []addressing.AddressWarning, code string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code {
			return
		}
	}
	t.Fatalf("missing warning %s in %#v", code, warnings)
}

func requireNoWarning(t *testing.T, warnings []addressing.AddressWarning, code string) {
	t.Helper()
	for _, warning := range warnings {
		require.NotEqual(t, code, warning.Code)
	}
}
