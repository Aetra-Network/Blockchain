package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Stage 4: ABI round-trip coverage for the six Stage-3 financial struct
// types (BasisPoints/Ratio256/Decimal256/Decimal128/SignedDecimal256/
// SignedDecimal128), plus the codec's new deterministic-rejection behavior
// for malformed/unregistered-shape payloads of those types. See
// docs/architecture/avm-financial-abi.md for the documented wire shape.

type ratio256Fixture struct {
	Num uint64
	Den uint64
}

type basisPointsFixture struct {
	Bps uint64
}

type decimalRawFixture struct {
	Raw uint64
}

func financialCodec(typeName string) Codec {
	return Codec{
		Name: "Fixture",
		Fields: []CodecField{
			{Name: "value", Type: TypeRef{Name: typeName}},
		},
	}
}

// encodeThenReencode performs encode -> decode -> re-encode and asserts the
// two encoded byte strings are identical (the round-trip property Stage 4
// asks for).
func requireByteIdenticalRoundTrip[T any](t *testing.T, codec Codec, original T) T {
	t.Helper()
	encoded, err := codec.Encode(original)
	require.NoError(t, err)

	var decoded T
	require.NoError(t, codec.Decode(encoded, &decoded))

	reencoded, err := codec.Encode(decoded)
	require.NoError(t, err)
	require.Equal(t, string(encoded), string(reencoded), "encode->decode->re-encode must be byte-identical")
	return decoded
}

func TestFinancialABI_Ratio256_RoundTrips(t *testing.T) {
	codec := financialCodec("Ratio256")
	type fixture struct {
		Value ratio256Fixture
	}
	original := fixture{Value: ratio256Fixture{Num: 3, Den: 7}}
	decoded := requireByteIdenticalRoundTrip(t, codec, original)
	require.Equal(t, original, decoded)
}

func TestFinancialABI_BasisPoints_RoundTrips(t *testing.T) {
	codec := financialCodec("BasisPoints")
	type fixture struct {
		Value basisPointsFixture
	}
	original := fixture{Value: basisPointsFixture{Bps: 30}}
	decoded := requireByteIdenticalRoundTrip(t, codec, original)
	require.Equal(t, original, decoded)
}

func TestFinancialABI_Decimal256_RoundTrips(t *testing.T) {
	codec := financialCodec("Decimal256")
	type fixture struct {
		Value decimalRawFixture
	}
	original := fixture{Value: decimalRawFixture{Raw: 2500000000000000000}}
	decoded := requireByteIdenticalRoundTrip(t, codec, original)
	require.Equal(t, original, decoded)
}

func TestFinancialABI_Decimal128_RoundTrips(t *testing.T) {
	codec := financialCodec("Decimal128")
	type fixture struct {
		Value decimalRawFixture
	}
	original := fixture{Value: decimalRawFixture{Raw: 2500000000}}
	decoded := requireByteIdenticalRoundTrip(t, codec, original)
	require.Equal(t, original, decoded)
}

func TestFinancialABI_SignedDecimal256_RoundTrips(t *testing.T) {
	codec := financialCodec("SignedDecimal256")
	type fixture struct {
		Value decimalRawFixture
	}
	original := fixture{Value: decimalRawFixture{Raw: 2500000000000000000}}
	decoded := requireByteIdenticalRoundTrip(t, codec, original)
	require.Equal(t, original, decoded)
}

func TestFinancialABI_SignedDecimal128_RoundTrips(t *testing.T) {
	codec := financialCodec("SignedDecimal128")
	type fixture struct {
		Value decimalRawFixture
	}
	original := fixture{Value: decimalRawFixture{Raw: 2500000000}}
	decoded := requireByteIdenticalRoundTrip(t, codec, original)
	require.Equal(t, original, decoded)
}

func TestFinancialABI_ZeroValueForType_UsesRegisteredShape(t *testing.T) {
	require.Equal(t, map[string]any{"num": 0, "den": 0}, zeroValueForType(TypeRef{Name: "Ratio256"}))
	require.Equal(t, map[string]any{"bps": 0}, zeroValueForType(TypeRef{Name: "BasisPoints"}))
	require.Equal(t, map[string]any{"raw": 0}, zeroValueForType(TypeRef{Name: "Decimal256"}))
}

func TestFinancialABI_DecodeRejectsMissingField(t *testing.T) {
	codec := financialCodec("Ratio256")
	// Only "num" present -- "den" is missing entirely, not silently defaulted.
	payload := `[{"name":"value","type":"Ratio256","value":{"num":3}}]`

	type fixture struct {
		Value ratio256Fixture
	}
	var out fixture
	err := codec.Decode([]byte(payload), &out)
	require.Error(t, err)
	require.ErrorContains(t, err, "expected 2 field")
}

func TestFinancialABI_DecodeRejectsExtraField(t *testing.T) {
	codec := financialCodec("BasisPoints")
	// An extra unknown field alongside the legitimate "bps" field.
	payload := `[{"name":"value","type":"BasisPoints","value":{"bps":30,"unexpected":1}}]`

	type fixture struct {
		Value basisPointsFixture
	}
	var out fixture
	err := codec.Decode([]byte(payload), &out)
	require.Error(t, err)
	require.ErrorContains(t, err, "expected 1 field")
}

func TestFinancialABI_DecodeRejectsNonObjectPayload(t *testing.T) {
	codec := financialCodec("Decimal256")
	// A bare scalar instead of the required {"raw": ...} object shape.
	payload := `[{"name":"value","type":"Decimal256","value":42}]`

	type fixture struct {
		Value decimalRawFixture
	}
	var out fixture
	err := codec.Decode([]byte(payload), &out)
	require.Error(t, err)
	require.ErrorContains(t, err, "expected a JSON object")
}

func TestFinancialABI_EncodeRejectsMissingField(t *testing.T) {
	codec := financialCodec("Ratio256")
	type incompleteFixture struct {
		Value struct {
			Num uint64
			// Den intentionally omitted -- Ratio256 requires it.
		}
	}
	var incomplete incompleteFixture
	incomplete.Value.Num = 5

	_, err := codec.Encode(incomplete)
	require.Error(t, err)
	require.ErrorContains(t, err, "missing field")
}

func TestFinancialABI_UnrecognizedNameStillFallsBackGenerically(t *testing.T) {
	// A type name NOT in the financial registry keeps the pre-existing
	// generic behavior (documented, pre-Stage-4 limitation) -- this pins
	// down that the new registry is scoped to exactly the six Stage-3
	// names and does not change behavior for anything else.
	require.Equal(t, map[string]any{}, zeroValueForType(TypeRef{Name: "SomeFutureStruct"}))
}
