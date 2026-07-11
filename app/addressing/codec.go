package addressing

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	coreaddress "cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
)

const (
	// Bech32HRP is the human-readable prefix of the raw address form (ae1…). The
	// raw address form on this chain IS standard bech32 over the canonical bytes
	// (20 for a plain account, 32 for a native-account v2 identity) — the legacy
	// "4:<hex>" and "-7:<hex>" prefixed-hex forms are no longer produced or
	// parsed.
	Bech32HRP = "ae"

	UserFriendlyLength		= 46
	UserFriendlyLegacyLength	= 48
	UserFriendlyPrefix		= "AE"
	// ZeroUserFriendly is the AE user-facing form of the all-zero address.
	ZeroUserFriendly	= "AEAAAQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	rawPayloadLength	= 32
	shortAddressLength	= 20
	longAddressPadLength	= rawPayloadLength - shortAddressLength
	userFriendlyChecksumLength	= 10
	userFriendlyLegacyVersion	= byte(1)
	userFriendlyCanonicalVersion	= byte(2)
)

var (
	userFriendlyLegacyMagic	= [3]byte{0x00, 0x40, 0x00}
	userFriendlyCanonicalMagic	= [3]byte{0x00, 0x42, 0x64}

	// ZeroRawAddress is the bech32 (ae1…) raw form of the all-zero address.
	ZeroRawAddress = mustFormatRaw(make([]byte, shortAddressLength))
)

type Codec struct{}

var _ coreaddress.Codec = Codec{}

func (Codec) BytesToString(bz []byte) (string, error) {
	if len(bz) == 0 {
		return "", nil
	}
	return FormatUserFriendly(bz)
}

func (Codec) StringToBytes(text string) ([]byte, error) {
	return Parse(text)
}

// Format renders the raw address form: standard bech32 (ae1…) over the canonical
// address bytes. A 20-byte account and a 32-byte v2 identity each encode at their
// natural width; a legacy zero-padded 32-byte input collapses to its 20
// significant bytes first, so Format is stable regardless of which width a caller
// happens to hold.
func Format(bz []byte) string {
	return mustFormatRaw(bz)
}

func mustFormatRaw(bz []byte) string {
	text, err := formatRaw(bz)
	if err != nil {
		panic(err)
	}
	return text
}

func formatRaw(bz []byte) (string, error) {
	canonical, err := canonicalBytes(bz)
	if err != nil {
		return "", err
	}
	return bech32.ConvertAndEncode(Bech32HRP, canonical)
}

// canonicalBytes normalizes any accepted address byte width (20, 32, or a
// zero-padded 32) to the canonical form: 20 bytes for a plain account, 32 for a
// v2 identity.
func canonicalBytes(bz []byte) ([]byte, error) {
	raw, err := ToRawPayload(bz)
	if err != nil {
		return nil, err
	}
	canonical := FromRawPayload(raw)
	if canonical == nil {
		return nil, fmt.Errorf("unsupported address byte length %d", len(bz))
	}
	return canonical, nil
}

func FormatAccAddress(addr sdk.AccAddress) string {
	return mustFormatUserFriendly(addr.Bytes())
}

func FormatValAddress(addr sdk.ValAddress) string {
	return mustFormatUserFriendly(addr.Bytes())
}

func FormatConsAddress(addr sdk.ConsAddress) string {
	return mustFormatUserFriendly(addr.Bytes())
}

func IsZero(bz []byte) bool {
	raw, err := ToRawPayload(bz)
	if err != nil {
		return false
	}
	for _, b := range raw {
		if b != 0 {
			return false
		}
	}
	return true
}

func IsZeroAccAddress(addr sdk.AccAddress) bool {
	return IsZero(addr.Bytes())
}

func FormatUserFriendly(bz []byte) (string, error) {
	raw, err := ToRawPayload(bz)
	if err != nil {
		return "", err
	}
	if IsZero(raw) {
		return ZeroUserFriendly, nil
	}
	canonical := FromRawPayload(raw)
	sum := sha256.Sum256(canonical)
	payload := make([]byte, 0, 4+userFriendlyChecksumLength+len(canonical))
	payload = append(payload, userFriendlyCanonicalMagic[:]...)
	payload = append(payload, userFriendlyCanonicalVersion)
	payload = append(payload, sum[:userFriendlyChecksumLength]...)
	payload = append(payload, canonical...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func mustFormatUserFriendly(bz []byte) string {
	text, err := FormatUserFriendly(bz)
	if err != nil {
		panic(err)
	}
	return text
}

func ParseAccAddress(text string) (sdk.AccAddress, error) {
	bz, err := Parse(text)
	if err != nil {
		return nil, err
	}
	return sdk.AccAddress(bz), nil
}

// Parse accepts the two supported address forms — the AE… user-friendly form and
// the ae1… bech32 raw form — and returns the canonical address bytes. The legacy
// "4:<hex>" / "-7:<hex>" prefixed-hex forms are rejected.
func Parse(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("empty address string is not allowed")
	}
	if strings.HasPrefix(text, UserFriendlyPrefix) {
		for _, address := range reservedSystemAddresses {
			if address.UserFriendly == text {
				return Parse(address.Raw)
			}
		}
		return parseUserFriendly(text)
	}
	// Decode the ae1… bech32 raw form directly against the canonical HRP so
	// parsing never depends on the SDK's global bech32 config being initialized
	// (account, validator, and consensus prefixes are all Bech32HRP on this
	// chain, so one decode covers every role).
	if hrp, bz, err := bech32.DecodeAndConvert(text); err == nil && hrp == Bech32HRP {
		if verifyErr := sdk.VerifyAddressFormat(bz); verifyErr != nil {
			return nil, verifyErr
		}
		return bz, nil
	}
	return nil, fmt.Errorf("invalid address format: expected an AE… user-friendly or ae1… bech32 address")
}

func parseUserFriendly(text string) ([]byte, error) {
	payload, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, err
	}
	switch len(payload) {
	case 34:
		if !hasUserFriendlyHeader(payload, userFriendlyCanonicalVersion) {
			return nil, fmt.Errorf("invalid AE userfriendly address header")
		}
		checksum := sha256.Sum256(payload[14:])
		if !bytes.Equal(payload[4:14], checksum[:userFriendlyChecksumLength]) {
			return nil, fmt.Errorf("invalid AE userfriendly address checksum")
		}
		out := make([]byte, shortAddressLength)
		copy(out, payload[14:])
		return out, nil
	case 36:
		if !hasLegacyOrCanonicalUserFriendlyHeader(payload) {
			return nil, fmt.Errorf("invalid AE userfriendly address header")
		}
		return FromRawPayload(payload[4:]), nil
	case 46:
		if !hasUserFriendlyHeader(payload, userFriendlyCanonicalVersion) {
			return nil, fmt.Errorf("invalid AE userfriendly address header")
		}
		checksum := sha256.Sum256(payload[14:])
		if !bytes.Equal(payload[4:14], checksum[:userFriendlyChecksumLength]) {
			return nil, fmt.Errorf("invalid AE userfriendly address checksum")
		}
		return FromRawPayload(payload[14:]), nil
	default:
		return nil, fmt.Errorf("invalid AE userfriendly address length")
	}
}

func hasUserFriendlyHeader(payload []byte, version byte) bool {
	if len(payload) < 4 {
		return false
	}
	return payload[0] == userFriendlyCanonicalMagic[0] &&
		payload[1] == userFriendlyCanonicalMagic[1] &&
		payload[2] == userFriendlyCanonicalMagic[2] &&
		payload[3] == version
}

func hasLegacyOrCanonicalUserFriendlyHeader(payload []byte) bool {
	if len(payload) < 4 {
		return false
	}
	return (payload[0] == userFriendlyLegacyMagic[0] &&
		payload[1] == userFriendlyLegacyMagic[1] &&
		payload[2] == userFriendlyLegacyMagic[2] &&
		payload[3] == userFriendlyLegacyVersion) ||
		(payload[0] == userFriendlyCanonicalMagic[0] &&
			payload[1] == userFriendlyCanonicalMagic[1] &&
			payload[2] == userFriendlyCanonicalMagic[2] &&
			payload[3] == userFriendlyCanonicalVersion)
}

func ToRawPayload(bz []byte) ([]byte, error) {
	switch len(bz) {
	case shortAddressLength:
		raw := make([]byte, rawPayloadLength)
		copy(raw[longAddressPadLength:], bz)
		return raw, nil
	case rawPayloadLength:
		raw := make([]byte, rawPayloadLength)
		copy(raw, bz)
		return raw, nil
	default:
		return nil, fmt.Errorf("unsupported address byte length %d", len(bz))
	}
}

func FromRawPayload(raw []byte) []byte {
	if len(raw) != rawPayloadLength {
		return nil
	}
	for i := 0; i < longAddressPadLength; i++ {
		if raw[i] != 0 {
			out := make([]byte, rawPayloadLength)
			copy(out, raw)
			return out
		}
	}
	out := make([]byte, shortAddressLength)
	copy(out, raw[longAddressPadLength:])
	return out
}
