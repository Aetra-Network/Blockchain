package compiler

import (
	"encoding/json"
	"testing"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	"github.com/stretchr/testify/require"
)

type codecFixture struct {
	Alpha   uint64
	Payload map[string]uint64
	Gamma   string
}

func TestCodecEncodeDefaultsUsesDeclaredFieldOrder(t *testing.T) {
	codec := Codec{
		Name: "DemoState",
		Fields: []CodecField{
			{Name: "alpha", Type: TypeRef{Name: "u64"}},
			{Name: "payload", Type: TypeRef{Name: "Map", Args: []TypeRef{{Name: "string"}, {Name: "u64"}}}},
			{Name: "gamma", Type: TypeRef{Name: "string"}},
		},
	}

	encoded, err := codec.EncodeDefaults()
	require.NoError(t, err)

	var values []codecFieldValue
	require.NoError(t, json.Unmarshal(encoded, &values))
	require.Len(t, values, 3)
	require.Equal(t, "alpha", values[0].Name)
	require.Equal(t, "payload", values[1].Name)
	require.Equal(t, "gamma", values[2].Name)
	require.Equal(t, "u64", values[0].Type)
	require.Equal(t, "Map<string,u64>", values[1].Type)
	require.Equal(t, "string", values[2].Type)
}

func TestCodecDecodeRejectsNonCanonicalFieldOrder(t *testing.T) {
	codec := Codec{
		Name: "DemoState",
		Fields: []CodecField{
			{Name: "alpha", Type: TypeRef{Name: "u64"}},
			{Name: "payload", Type: TypeRef{Name: "Map", Args: []TypeRef{{Name: "string"}, {Name: "u64"}}}},
			{Name: "gamma", Type: TypeRef{Name: "string"}},
		},
	}
	input := codecFixture{
		Alpha:   7,
		Payload: map[string]uint64{"b": 2, "a": 1},
		Gamma:   "z",
	}

	encoded, err := codec.Encode(input)
	require.NoError(t, err)

	var values []codecFieldValue
	require.NoError(t, json.Unmarshal(encoded, &values))
	require.Len(t, values, 3)
	values[0], values[1] = values[1], values[0]
	reordered, err := json.Marshal(values)
	require.NoError(t, err)

	var out codecFixture
	err = codec.Decode(reordered, &out)
	require.Error(t, err)
	require.ErrorContains(t, err, "name mismatch")
}

func TestCodecEncodeIsDeterministicForMapFields(t *testing.T) {
	codec := Codec{
		Name: "DemoState",
		Fields: []CodecField{
			{Name: "payload", Type: TypeRef{Name: "Map", Args: []TypeRef{{Name: "string"}, {Name: "u64"}}}},
		},
	}

	left := codecFixture{Payload: map[string]uint64{"b": 2, "a": 1}}
	right := codecFixture{Payload: map[string]uint64{"a": 1, "b": 2}}

	leftBytes, err := codec.Encode(left)
	require.NoError(t, err)
	rightBytes, err := codec.Encode(right)
	require.NoError(t, err)

	require.Equal(t, leftBytes, rightBytes)

	var values []codecFieldValue
	require.NoError(t, json.Unmarshal(leftBytes, &values))
	require.Len(t, values, 1)
	require.Contains(t, string(values[0].Value), "\"a\"")
	require.Contains(t, string(values[0].Value), "\"b\"")
}

func TestCodecEncodesCodeSnapshotsFromChunkPayloads(t *testing.T) {
	root, err := avm.ToChunkPayload([]byte("AVM bytecode"), chunk.TypeNormal)
	require.NoError(t, err)

	codec := Codec{
		Name: "DemoState",
		Fields: []CodecField{
			{Name: "tokenWalletCode", Type: TypeRef{Name: "Code"}},
		},
	}

	encoded, err := codec.Encode(map[string]any{
		"tokenWalletCode": root,
	})
	require.NoError(t, err)

	var values []codecFieldValue
	require.NoError(t, json.Unmarshal(encoded, &values))
	require.Len(t, values, 1)
	require.Equal(t, "tokenWalletCode", values[0].Name)
	require.Equal(t, "Code", values[0].Type)
	require.Contains(t, string(values[0].Value), "\"hex\"")
	require.Contains(t, string(values[0].Value), "\"base64\"")
	require.Contains(t, string(values[0].Value), "\"hash\"")
	require.Contains(t, string(values[0].Value), "\"chunks\"")
}

func TestCodecEncodesHashAliasesAsHexStrings(t *testing.T) {
	var hash [32]byte
	for i := range hash {
		hash[i] = byte(i)
	}

	codec := Codec{
		Name: "DemoState",
		Fields: []CodecField{
			{Name: "digest", Type: TypeRef{Name: "Hash"}},
		},
	}

	encoded, err := codec.Encode(map[string]any{
		"digest": hash,
	})
	require.NoError(t, err)

	var values []codecFieldValue
	require.NoError(t, json.Unmarshal(encoded, &values))
	require.Len(t, values, 1)
	require.Equal(t, "digest", values[0].Name)
	require.Equal(t, "Hash", values[0].Type)
	require.Equal(t, "\"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f\"", string(values[0].Value))
}

func TestCodecEncodesSignedIntegersWithoutLosingSign(t *testing.T) {
	codec := Codec{
		Name: "DemoState",
		Fields: []CodecField{
			{Name: "delta", Type: TypeRef{Name: "i64"}},
		},
	}

	encoded, err := codec.Encode(map[string]any{
		"delta": int64(-7),
	})
	require.NoError(t, err)

	var values []codecFieldValue
	require.NoError(t, json.Unmarshal(encoded, &values))
	require.Len(t, values, 1)
	require.Equal(t, "delta", values[0].Name)
	require.Equal(t, "i64", values[0].Type)
	require.Equal(t, "-7", string(values[0].Value))
}
