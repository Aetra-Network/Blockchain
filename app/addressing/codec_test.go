package addressing_test

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
)

func TestRawAddressFormat(t *testing.T) {
	addr := sdk.AccAddress(bytes20(0x11))

	text := addressing.Format(addr)

	// The raw address form is standard bech32 (ae1…) over the canonical bytes.
	require.True(t, strings.HasPrefix(text, "ae1"))
	require.Equal(t, strings.ToLower(text), text)

	parsed, err := addressing.ParseAccAddress(text)
	require.NoError(t, err)
	require.Equal(t, addr, parsed)
}

func TestUserFacingAddressFormats(t *testing.T) {
	addr := sdk.AccAddress(bytes20(0x22))

	text := addressing.FormatAccAddress(addr)
	requireUserFriendlyAddress(t, text)
	require.Equal(t, "AEJkAs7HLyb3MltB8GgiIiIiIiIiIiIiIiIiIiIiIiIiIg", text)

	parsed, err := addressing.ParseAccAddress(text)
	require.NoError(t, err)
	require.Equal(t, addr, parsed)

	requireUserFriendlyAddress(t, addressing.FormatValAddress(sdk.ValAddress(addr)))
	requireUserFriendlyAddress(t, addressing.FormatConsAddress(sdk.ConsAddress(addr)))
}

func TestAEAccountValidatorAndConsensusAddressRoundTrip(t *testing.T) {
	addr := sdk.AccAddress(bytes20(0x2a))

	tests := map[string]string{
		"account":	addressing.FormatAccAddress(addr),
		"validator":	addressing.FormatValAddress(sdk.ValAddress(addr)),
		"consensus":	addressing.FormatConsAddress(sdk.ConsAddress(addr)),
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			requireUserFriendlyAddress(t, text)
			parsed, err := addressing.Parse(text)
			require.NoError(t, err)
			require.Equal(t, addr.Bytes(), parsed)
		})
	}
}

func TestRawLongAddressRoundTrip(t *testing.T) {
	raw, err := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)

	text := addressing.Format(raw)
	require.Equal(t, "ae1qy352euf40x77qfrg4ncn27dauqjx3t83x4ummcpydzk0zdtehhs84m4qt", text)

	parsed, err := addressing.Parse(text)
	require.NoError(t, err)
	require.Equal(t, raw, parsed)
}

// TestParseBech32BranchCanonicalizesZeroPaddedTwin is the regression test for
// FINDING-012: a 20-byte account and its non-canonical, directly-bech32-
// encoded zero-padded 32-byte "twin" must decode to the SAME bytes (and thus
// the same sdk.AccAddress bech32 string, which is the key the bank module
// and every other keeper actually use), instead of silently diverging as two
// different-length byte slices that only addressBytesKey treats as equal.
func TestParseBech32BranchCanonicalizesZeroPaddedTwin(t *testing.T) {
	short := bytes20(0x2f)

	shortText := addressing.Format(short)

	padded := make([]byte, 32)
	copy(padded[12:], short)
	// Bypass addressing.Format's canonicalization entirely and bech32-encode
	// the non-canonical zero-padded 32-byte payload directly, exactly like an
	// external tool or attacker constructing a non-canonical string would.
	paddedText, err := bech32.ConvertAndEncode(addressing.Bech32HRP, padded)
	require.NoError(t, err)
	require.NotEqual(t, shortText, paddedText, "the two encodings must be textually different inputs")

	x, err := addressing.Parse(shortText)
	require.NoError(t, err)
	y, err := addressing.Parse(paddedText)
	require.NoError(t, err)

	require.Equal(t, x, y, "20-byte account and its zero-padded 32-byte twin must canonicalize to the same bytes")
	require.Len(t, y, 20, "the zero-padded twin must collapse to its 20-byte canonical form")
	require.Equal(t, sdk.AccAddress(x).String(), sdk.AccAddress(y).String(),
		"canonicalized bytes must also match at the bank/keeper bech32-string layer")
}

// TestParseBech32BranchRejectsNonCanonicalLengths locks in that Parse's
// bech32 branch only ever accepts the two legal address widths (20 or 32
// bytes) -- payloads of any other length that no other address path on this
// chain accepts must now be rejected, not silently passed through.
func TestParseBech32BranchRejectsNonCanonicalLengths(t *testing.T) {
	for _, n := range []int{1, 10, 19, 21, 25, 31, 33, 40, 64, 100} {
		t.Run(fmt.Sprintf("%d_bytes", n), func(t *testing.T) {
			bz := make([]byte, n)
			for i := range bz {
				bz[i] = byte(i + 1)
			}
			text, err := bech32.ConvertAndEncode(addressing.Bech32HRP, bz)
			require.NoError(t, err)

			_, err = addressing.Parse(text)
			require.Errorf(t, err, "a %d-byte bech32 payload must be rejected", n)
		})
	}
}

// TestSystemRawAddressRoundTrip: reserved system addresses no longer use a
// distinct "-7:" raw form — they render as ordinary bech32 (ae1…) 32-byte raw
// addresses like any other, and still round-trip through Format/Parse.
func TestSystemRawAddressRoundTrip(t *testing.T) {
	raw, err := hex.DecodeString("01041041041041041041041041041041041041041041041041041042c4093391")
	require.NoError(t, err)

	text := addressing.Format(raw)
	require.True(t, strings.HasPrefix(text, "ae1"))

	parsed, err := addressing.Parse(text)
	require.NoError(t, err)
	require.Equal(t, raw, parsed)
}

func TestZeroAddressFormats(t *testing.T) {
	zero := sdk.AccAddress(bytes20(0))

	require.Equal(t, addressing.ZeroRawAddress, addressing.Format(zero))
	require.Equal(t, addressing.ZeroUserFriendly, addressing.FormatAccAddress(zero))
	require.True(t, addressing.IsZeroAccAddress(zero))

	userFriendly, err := addressing.FormatUserFriendly(zero)
	require.NoError(t, err)
	require.Equal(t, addressing.ZeroUserFriendly, userFriendly)

	rawParsed, err := addressing.ParseAccAddress(addressing.ZeroRawAddress)
	require.NoError(t, err)
	require.True(t, addressing.IsZeroAccAddress(rawParsed))

	friendlyParsed, err := addressing.ParseAccAddress(addressing.ZeroUserFriendly)
	require.NoError(t, err)
	require.True(t, addressing.IsZeroAccAddress(friendlyParsed))
}

func TestZeroAddressValidationPolicy(t *testing.T) {
	valid := sdk.AccAddress(bytes20(0x33))
	validText := addressing.FormatAccAddress(valid)

	require.NoError(t, addressing.ValidateUserAddress("recipient", validText))
	require.NoError(t, addressing.ValidateAuthorityAddress("authority", validText))
	require.NoError(t, addressing.ValidateContractAddress("contract", validText))
	require.NoError(t, addressing.RejectZeroAddress("signer", valid.Bytes()))

	require.ErrorContains(t, addressing.ValidateUserAddress("recipient", addressing.ZeroRawAddress), "must use AE user-facing address format")
	require.ErrorContains(t, addressing.ValidateUserAddress("recipient", addressing.ZeroUserFriendly), "must not be zero address")
	require.ErrorContains(t, addressing.ValidateAuthorityAddress("authority", addressing.ZeroRawAddress), "must not be zero address")
	require.ErrorContains(t, addressing.ValidateContractAddress("contract", addressing.ZeroRawAddress), "must use AE user-facing address format")
	require.ErrorContains(t, addressing.RejectZeroAddress("signer", sdk.AccAddress(bytes20(0)).Bytes()), "must not be zero address")

	_, present, err := addressing.ParseOptionalAdminAddress("admin", "")
	require.NoError(t, err)
	require.False(t, present)
	require.ErrorContains(t, addressing.ValidateOptionalAdminAddress("admin", addressing.ZeroRawAddress), "must use AE user-facing address format")
}

func TestAddressValidationRejectsEmptyMalformedAndLegacyFormats(t *testing.T) {
	validLegacy, err := sdk.Bech32ifyAddressBytes("orb", bytes20(0x44))
	require.NoError(t, err)

	validFriendly, err := addressing.FormatUserFriendly(sdk.AccAddress(bytes20(0x46)))
	require.NoError(t, err)

	tests := map[string]string{
		"empty":			"",
		"blank":			"   ",
		"malformed bech32":		"ae1notvalid",
		"foreign bech32":		"cosmos1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqp2n8k9",
		"old raw prefix":		"0:0000000000000000000000000000000000000000000000000000000000000000",
		"mixed case raw":		"4:ABCDEFabcdef0000000000000000000000000000000000000000000000000000",
		"mixed case system raw":	"-7:ABCDEFabcdef0000000000000000000000000000000000000000000000000000",
		"wrong system raw length":	"-7:00000000000000000000000000000000000000000000000000000000000000",
		"wrong length raw":		"4:00000000000000000000000000000000000000000000000000000000000000",
		"old userfriendly prefix":	"ORBAAQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"wrong userfriendly prefix":	"AF" + validFriendly[2:],
		"non base64url userfriendly":	"AE+/" + validFriendly[4:],
		"old bech32 account prefix":	validLegacy,
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			require.Error(t, addressing.ValidateUserAddress("sender", text))
		})
	}
}

// TestUserFriendlyChecksumlessCanonicalMagicIsRejected locks in that the
// 36-byte checksumless decode path accepts ONLY the legacy (v1) magic. The
// canonical (v2) form is always emitted checksummed (34/46 bytes), so a
// 36-byte payload carrying the v2 magic can only be a malformed/spoofed input
// -- accepting it would let a typo in a v2-tagged address decode unverified.
func TestUserFriendlyChecksumlessCanonicalMagicIsRejected(t *testing.T) {
	raw32 := make([]byte, 32)
	for i := range raw32 {
		raw32[i] = byte(i + 1)
	}

	// header + 32 raw bytes = 36-byte payload, no checksum.
	canonicalMagic36 := append([]byte{0x00, 0x42, 0x64, 0x02}, raw32...)
	legacyMagic36 := append([]byte{0x00, 0x40, 0x00, 0x01}, raw32...)

	canonicalAddr := base64.RawURLEncoding.EncodeToString(canonicalMagic36)
	legacyAddr := base64.RawURLEncoding.EncodeToString(legacyMagic36)

	// v2 magic at the checksumless 36-byte length must be rejected.
	_, err := addressing.Parse(canonicalAddr)
	require.Error(t, err)

	// Genuine legacy (v1) addresses in this form must still parse (backward
	// compatibility), and round-trip to the same 32 raw bytes.
	got, err := addressing.Parse(legacyAddr)
	require.NoError(t, err)
	require.Equal(t, raw32, got)
}

func TestAddressValidationRejectsCurrentSDKBech32InUserFacingAPIs(t *testing.T) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount("ae", "aepub")

	valid, err := sdk.Bech32ifyAddressBytes("ae", bytes20(0x45))
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(valid, "ae1"))
	require.ErrorContains(t, addressing.ValidateUserAddress("sender", valid), "must use AE user-facing address format")
}

func TestAddressPairRoundTripIsStable(t *testing.T) {
	addr := sdk.AccAddress(bytes20(0x55))
	user := addressing.FormatAccAddress(addr)
	raw := addressing.Format(addr)

	fromUser, err := addressing.PairFromUserAddress(addressing.AddressRoleAccount, user)
	require.NoError(t, err)
	fromRaw, err := addressing.PairFromRawAddress(addressing.AddressRoleAccount, raw)
	require.NoError(t, err)

	require.Equal(t, user, fromUser.User)
	require.Equal(t, raw, fromUser.Raw)
	require.Equal(t, fromUser, fromRaw)
	require.NoError(t, fromUser.Validate())
}

func TestDerivePubKeyAddressGoldenVectors(t *testing.T) {
	pubKey := &secp256k1.PubKey{Key: mustDecodeHex(t, "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")}

	account, err := addressing.DeriveAccountAddress(pubKey)
	require.NoError(t, err)
	validator, err := addressing.DeriveValidatorAddress(pubKey)
	require.NoError(t, err)
	consensus, err := addressing.DeriveConsensusAddress(pubKey)
	require.NoError(t, err)

	require.Equal(t, addressing.AddressRoleAccount, account.Role)
	require.Equal(t, "AEJkAmWJMy8C610WXuOHXy8gau5U1YrjvPUXF70Dm-xQ4Pt8t-Y4NkVtpC-wIA", account.User)
	require.Equal(t, "ae1sa0j7gr2ae2dtzhrhn63w9aaqwd7c58qld7t0e3cxezkmfp0kqsqqavnc6", account.Raw)
	require.False(t, addressing.IsLegacyPaddedRawAddress(mustParseRaw(t, account.Raw)), "derived raw address must not use legacy zero-padding")
	require.Equal(t, account.User, validator.User)
	require.Equal(t, account.Raw, validator.Raw)
	require.Equal(t, account.User, consensus.User)
	require.Equal(t, account.Raw, consensus.Raw)
	require.NoError(t, account.Validate())
	require.NoError(t, validator.Validate())
	require.NoError(t, consensus.Validate())
	require.Equal(t, account.User, "AEJkAmWJMy8C610WXuOHXy8gau5U1YrjvPUXF70Dm-xQ4Pt8t-Y4NkVtpC-wIA")
}

func mustParseRaw(t *testing.T, raw string) []byte {
	t.Helper()
	bz, err := addressing.Parse(raw)
	require.NoError(t, err)
	return bz
}

func mustDecodeHex(t *testing.T, text string) []byte {
	t.Helper()
	out, err := hex.DecodeString(text)
	require.NoError(t, err)
	return out
}

// TestUserFriendlyLongAddressRejectsSingleCharTypo is the regression guard for
// SEC-MED #13: the 32-byte v2 user-friendly address (the primary form of every
// pubkey-derived account) must carry a checksum so a single-character typo can
// never silently decode into a DIFFERENT valid address (fund loss).
func TestUserFriendlyLongAddressRejectsSingleCharTypo(t *testing.T) {
	raw := mustDecodeHex(t, "875f2f206aee54d58ae3bcf51717bd039bec50e0fb7cb7e63836456da42fb020")

	text, err := addressing.FormatUserFriendly(raw)
	require.NoError(t, err)
	// magic(3)+version(1)+checksum(10)+payload(32) = 46 bytes -> 62 base64url chars.
	require.Len(t, text, 62)
	require.True(t, strings.HasPrefix(text, "AEJk"))

	parsed, err := addressing.Parse(text)
	require.NoError(t, err)
	require.Equal(t, raw, parsed)

	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	variants := 0
	for i := 0; i < len(text); i++ {
		for j := 0; j < len(alphabet); j++ {
			if alphabet[j] == text[i] {
				continue
			}
			candidate := text[:i] + string(alphabet[j]) + text[i+1:]
			variants++
			bz, perr := addressing.Parse(candidate)
			if perr == nil {
				require.Equalf(t, raw, bz,
					"1-char typo %q decoded to a DIFFERENT valid address %x (fund-loss risk)", candidate, bz)
			}
		}
	}
	require.Positive(t, variants)
}

func bytes20(fill byte) []byte {
	out := make([]byte, 20)
	for i := range out {
		out[i] = fill
	}
	return out
}

func requireUserFriendlyAddress(t *testing.T, text string) {
	t.Helper()

	require.Len(t, text, addressing.UserFriendlyLength)
	require.True(t, strings.HasPrefix(text, addressing.UserFriendlyPrefix))
	require.Regexp(t, `^[A-Za-z0-9_-]{46}$`, text)
	require.NotRegexp(t, `^[a-z]+1`, text)
}
