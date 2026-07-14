package prefixgenesis

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/internal/kvtest"
)

type testGenesis struct {
	Version	uint64
	Params	testParams
	State	testState
}

type testParams struct {
	Enabled bool
}

type testState struct {
	Records []string
}

func TestLoadMigratesLegacyGenesisBlobToPrefixLayout(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	legacyKey := []byte{0x01}
	legacy := testGenesis{
		Version:	2,
		Params:		testParams{Enabled: true},
		State:		testState{Records: []string{"b", "a"}},
	}
	bz, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, service.RawStore().Set(legacyKey, bz))

	loaded, migrated, err := Load(ctx, service, legacyKey, testGenesis{})
	require.NoError(t, err)
	require.True(t, migrated)
	require.Equal(t, legacy, loaded)

	legacyValue, err := service.RawStore().Get(legacyKey)
	require.NoError(t, err)
	require.Empty(t, legacyValue)
	marker, err := service.RawStore().Get(layoutKey)
	require.NoError(t, err)
	require.Equal(t, []byte("v2"), marker)
	state, err := service.RawStore().Get([]byte("prefix_genesis/state"))
	require.NoError(t, err)
	require.NotEmpty(t, state)

	reloaded, found, err := Load(ctx, service, legacyKey, testGenesis{})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, legacy, reloaded)
}

// TestSaveOnlyRewritesChangedFields is the regression test for FINDING-009
// (O(total-state) reserialization on every write). It proves that a second
// Save call touching only one of several top-level fields performs a store
// write for just that field (plus the already-accepted legacy-key delete),
// instead of unconditionally re-marshaling and rewriting every field.
func TestSaveOnlyRewritesChangedFields(t *testing.T) {
	ctx := context.Background()
	service := kvtest.NewStoreService()
	legacyKey := []byte{0x01}

	initial := testGenesis{
		Version:	1,
		Params:		testParams{Enabled: true},
		State:		testState{Records: []string{"a", "b", "c"}},
	}
	require.NoError(t, Save(ctx, service, legacyKey, initial))

	versionKey := []byte("prefix_genesis/version")
	paramsKey := []byte("prefix_genesis/params")
	stateKey := []byte("prefix_genesis/state")

	// First Save call: layout marker and all three fields are new, so every
	// key is written once.
	require.Equal(t, uint64(1), service.RawStore().SetCount(layoutKey))
	require.Equal(t, uint64(1), service.RawStore().SetCount(versionKey))
	require.Equal(t, uint64(1), service.RawStore().SetCount(paramsKey))
	require.Equal(t, uint64(1), service.RawStore().SetCount(stateKey))

	// Mutate only State; Version and Params are carried over unchanged, as a
	// real keeper's saveGenesis/writeGenesisState does for a targeted update.
	changed := initial
	changed.State = testState{Records: []string{"a", "b", "c", "d"}}

	service.RawStore().ResetWriteCounts()
	require.NoError(t, Save(ctx, service, legacyKey, changed))

	// Only the field that actually changed is rewritten.
	require.Zero(t, service.RawStore().SetCount(versionKey),
		"unchanged Version field must not be rewritten")
	require.Zero(t, service.RawStore().SetCount(paramsKey),
		"unchanged Params field must not be rewritten")
	require.Equal(t, uint64(1), service.RawStore().SetCount(stateKey),
		"changed State field must be rewritten exactly once")
	// The layout marker is already "v2" from the first Save, so it is left
	// untouched too.
	require.Zero(t, service.RawStore().SetCount(layoutKey),
		"layout marker already at v2 must not be rewritten")

	// Load must still observe the mutation -- skipping redundant writes must
	// not change what a reader sees.
	reloaded, found, err := Load(ctx, service, legacyKey, testGenesis{})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, changed, reloaded)

	// A third Save with no changes at all must not rewrite anything,
	// including the legacy-key delete's target field keys.
	service.RawStore().ResetWriteCounts()
	require.NoError(t, Save(ctx, service, legacyKey, changed))
	require.Zero(t, service.RawStore().SetCount(versionKey))
	require.Zero(t, service.RawStore().SetCount(paramsKey))
	require.Zero(t, service.RawStore().SetCount(stateKey))
	require.Zero(t, service.RawStore().SetCount(layoutKey))
}
