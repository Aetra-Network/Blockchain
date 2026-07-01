package standards

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultRegistryCanonicalAndStable(t *testing.T) {
	r := DefaultRegistry()
	require.Equal(t, CanonicalVersion, r.Version)
	require.Len(t, r.Types, 9)

	names := make([]string, len(r.Types))
	for i, typ := range r.Types {
		names[i] = typ.Name
		require.Equal(t, CanonicalVersion, typ.Version)
		require.NotEmpty(t, typ.Description)
	}
	require.Equal(t, []string{"Address", "Cell", "Coins", "List", "Map", "Option", "Ref", "Result", "Slice"}, names)
	require.NotEqual(t, [32]byte{}, r.Hash())

	canonical := r.Canonical()
	require.Equal(t, r, canonical)

	shuffled := Registry{
		Version: CanonicalVersion,
		Types: []TypeDescriptor{
			{Name: "Map", Version: CanonicalVersion, Kind: "generic", Arity: 2, Description: "deterministically ordered key/value map"},
			{Name: "Address", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "chain-bound account or contract address"},
			{Name: "Slice", Version: CanonicalVersion, Kind: "cell", Arity: 0, Description: "bounded view over a Cell payload"},
			{Name: "Ref", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "typed reference to an out-of-line payload"},
			{Name: "Result", Version: CanonicalVersion, Kind: "generic", Arity: 2, Description: "success or error value"},
			{Name: "Option", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "optional value"},
			{Name: "List", Version: CanonicalVersion, Kind: "generic", Arity: 1, Description: "bounded canonical list"},
			{Name: "Coins", Version: CanonicalVersion, Kind: "scalar", Arity: 0, Description: "non-negative coin amount in base units"},
			{Name: "Cell", Version: CanonicalVersion, Kind: "cell", Arity: 0, Description: "canonical chunk/cell payload root"},
		},
	}
	require.Equal(t, r.Hash(), shuffled.Hash())
}

func TestRegistryValidate(t *testing.T) {
	r := DefaultRegistry()
	require.NoError(t, r.Validate("Map", 2))
	require.ErrorContains(t, r.Validate("Map", 1), "requires 2 type arguments")
	require.ErrorContains(t, r.Validate("missing", 0), "unknown standard type")
}
