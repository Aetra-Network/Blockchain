package standards

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultRegistryCanonicalAndStable(t *testing.T) {
	r := DefaultRegistry()
	require.Equal(t, CanonicalVersion, r.Version)
	require.Len(t, r.Types, 17)

	names := make([]string, len(r.Types))
	for i, typ := range r.Types {
		names[i] = typ.Name
		require.Equal(t, CanonicalVersion, typ.Version)
		require.NotEmpty(t, typ.Description)
	}
	require.Equal(t, []string{"Address", "Bytes", "Chunk", "ChunkLink", "ChunkRef", "Code", "Coins", "Dict", "Hash", "List", "Map", "MapEntry", "Option", "Result", "Segment", "StateInit", "Timestamp"}, names)
	require.NotEqual(t, [32]byte{}, r.Hash())

	canonical := r.Canonical()
	require.Equal(t, r, canonical)

	shuffled := Registry{
		Version: CanonicalVersion,
		Types: []TypeDescriptor{
			{Name: "Map", Version: CanonicalVersion, Kind: "generic", Arity: 2, Description: "deterministically ordered key/value map"},
			{Name: "Dict", Version: CanonicalVersion, Kind: "generic", Arity: 2, Description: "alias of Map for ordered key/value dictionaries"},
			{Name: "Address", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "chain-bound account or contract address"},
			{Name: "Bytes", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "arbitrary byte payload"},
			{Name: "Segment", Version: CanonicalVersion, Kind: "chunk", Arity: 0, Description: "bounded read-only view over a Chunk"},
			{Name: "Code", Version: CanonicalVersion, Kind: "chunk", Arity: 0, Description: "canonical contract bytecode snapshot"},
			{Name: "ChunkLink", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "typed out-of-line reference used when the relationship is part of the public ABI"},
			{Name: "MapEntry", Version: CanonicalVersion, Kind: "generic", Arity: 2, Description: "key/value pair yielded by map iteration"},
			{Name: "Result", Version: CanonicalVersion, Kind: "generic", Arity: 2, Description: "success or error value"},
			{Name: "Option", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "optional value"},
			{Name: "List", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "bounded canonical list"},
			{Name: "Coins", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "non-negative coin amount in base units"},
			{Name: "Chunk", Version: CanonicalVersion, Kind: "chunk", Arity: 0, Description: "canonical public payload root"},
			{Name: "ChunkRef", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "typed out-of-line reference optimized for deferred loading"},
			{Name: "Hash", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "deterministic 32-byte digest"},
			{Name: "StateInit", Version: CanonicalVersion, Kind: "struct", Arity: 0, Description: "canonical deployment descriptor"},
			{Name: "Timestamp", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "block or transaction timestamp"},
		},
	}
	require.Equal(t, r.Hash(), shuffled.Hash())
}

func TestRegistryValidate(t *testing.T) {
	r := DefaultRegistry()
	require.NoError(t, r.Validate("Map", 2))
	require.NoError(t, r.Validate("Dict", 2))
	require.NoError(t, r.Validate("MapEntry", 2))
	require.ErrorContains(t, r.Validate("Map", 1), "requires 2 type arguments")
	require.ErrorContains(t, r.Validate("missing", 0), "unknown standard type")
}
