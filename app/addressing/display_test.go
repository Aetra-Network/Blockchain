package addressing_test

import (
	"encoding/binary"
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
	require.Regexp(t, `^ae1[0-9a-z]+$`, display.Raw)
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
	// SEC-MED #13: a valid address can no longer be manufactured by mutating a
	// single character of another (the checksum rejects it), so the old
	// one-char-lookalike construction is gone. We instead find two GENUINELY
	// valid v2 addresses that share the same shortened (prefix+suffix) form and
	// assert the prefix/suffix collision warning still fires. The edit-distance
	// AddressWarningLookalike for addresses can no longer be triggered — that is
	// precisely the guarantee this fix provides; the Lookalike heuristic remains
	// exercised for confusable LABELS in TestInspectAddressDetectsRecentAndConfusableLabels.
	known, collidingAddr := collidingShortFormPair(t)
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

// collidingShortFormPair finds two distinct, genuinely-valid v2 user-friendly
// addresses that share the same ShortenAddress (first-10 + last-8) form. The
// last-8 chars come from a fixed payload suffix; the first-10 chars vary only by
// the checksum prefix, so a short birthday search over the leading payload bytes
// yields a collision on the checksum prefix.
func collidingShortFormPair(t *testing.T) (string, string) {
	t.Helper()
	seen := make(map[string]string)
	raw := make([]byte, 32)
	copy(raw[26:], []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}) // fix suffix -> shortened tail is constant
	for n := uint64(1); n < 10_000_000; n++ {
		binary.BigEndian.PutUint64(raw[0:8], n)
		binary.BigEndian.PutUint64(raw[8:16], n*0x9E3779B97F4A7C15)
		binary.BigEndian.PutUint64(raw[16:24], n*0xC2B2AE3D27D4EB4F)
		user, err := addressing.FormatUserFriendly(raw)
		require.NoError(t, err)
		short := addressing.ShortenAddress(user)
		if prev, ok := seen[short]; ok && prev != user {
			return prev, user
		}
		seen[short] = user
	}
	t.Fatalf("failed to find short-form collision")
	return "", ""
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
