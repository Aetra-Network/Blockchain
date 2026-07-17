package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCodecBytesFieldRoundTrips is the regression test for the codec asymmetry:
// bytes/hash/hash32 values were hex-encoded on Encode (hex.EncodeToString) but
// decoded by reinterpreting the hex TEXT as raw bytes (field.SetBytes([]byte(s))),
// so a []byte field neither preserved its length nor its contents through an
// Encode -> Decode cycle. Before the fix, decoding []byte{0xAB,0xCD,0x01}
// yielded []byte("abcd01") (6 bytes, the ASCII of the hex string).
func TestCodecBytesFieldRoundTrips(t *testing.T) {
	type blobFixture struct {
		Blob []byte
	}

	codec := Codec{
		Name: "BlobState",
		Fields: []CodecField{
			{Name: "blob", Type: TypeRef{Name: "bytes"}},
		},
	}

	original := blobFixture{Blob: []byte{0xAB, 0xCD, 0x01, 0x00, 0xFF}}

	encoded, err := codec.Encode(original)
	require.NoError(t, err)

	var decoded blobFixture
	require.NoError(t, codec.Decode(encoded, &decoded))

	require.Equal(t, original.Blob, decoded.Blob,
		"FIX REGRESSION: bytes field must round-trip through Encode/Decode; hex-encoded value must be hex-decoded, not read as raw ASCII")
}

// TestCodecHash32FieldRoundTrips covers the fixed-size array alias (hash32 ->
// [32]byte): the array decode path previously fell through to json.Unmarshal of
// a hex string into a byte array, which does not round-trip the hex encoding.
func TestCodecHash32FieldRoundTrips(t *testing.T) {
	type digestFixture struct {
		Digest [32]byte
	}

	codec := Codec{
		Name: "DigestState",
		Fields: []CodecField{
			{Name: "digest", Type: TypeRef{Name: "hash32"}},
		},
	}

	var original digestFixture
	for i := range original.Digest {
		original.Digest[i] = byte(i * 7)
	}

	encoded, err := codec.Encode(original)
	require.NoError(t, err)

	var decoded digestFixture
	require.NoError(t, codec.Decode(encoded, &decoded))

	require.Equal(t, original.Digest, decoded.Digest,
		"FIX REGRESSION: hash32 ([32]byte) field must round-trip through Encode/Decode")
}
