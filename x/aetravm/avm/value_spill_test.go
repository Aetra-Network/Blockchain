package avm

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/chunk"
)

// TestValueWriterSpillsOversizedDataIntoChunkTree proves a serialized value
// larger than one chunk (MaxDataBits) is transparently stored as a nested
// chunk tree and round-trips byte-for-byte, instead of failing with
// "chunk data bits ... exceeds limit 2048".
func TestValueWriterSpillsOversizedDataIntoChunkTree(t *testing.T) {
	for _, size := range []int{
		chunk.MaxDataBits / 8,     // exactly one leaf chunk
		chunk.MaxDataBits/8 + 1,   // one byte over -> must spill
		chunk.MaxDataBits/8*8 + 3, // several levels of spill
		20_000,                    // deep tree
	} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i*7 + 1)
		}

		w := NewValueWriter()
		w.data = append([]byte(nil), data...)
		root, err := w.Build()
		require.NoErrorf(t, err, "value of %d bytes must build a chunk tree", size)

		flat, err := FromChunkPayload(root)
		require.NoError(t, err)
		require.Truef(t, bytes.Equal(data, flat), "value of %d bytes must round-trip byte-for-byte", size)
	}
}

// TestValueWriterSpillIsDeterministic proves identical oversized values build
// identical chunk trees (same content hash) — required for consensus.
func TestValueWriterSpillIsDeterministic(t *testing.T) {
	data := make([]byte, 5000)
	for i := range data {
		data[i] = byte(i % 251)
	}

	w1 := NewValueWriter()
	w1.data = append([]byte(nil), data...)
	root1, err := w1.Build()
	require.NoError(t, err)

	w2 := NewValueWriter()
	w2.data = append([]byte(nil), data...)
	root2, err := w2.Build()
	require.NoError(t, err)

	require.Equal(t, root1.Hash(), root2.Hash(), "identical oversized values must hash identically")
}

// TestValueWriterLeafLayoutUnchanged proves values that already fit one chunk
// keep the exact single-chunk layout (no spill), so no existing state hash
// changes — the spill path is purely additive.
func TestValueWriterLeafLayoutUnchanged(t *testing.T) {
	data := make([]byte, chunk.MaxDataBits/8) // largest leaf
	for i := range data {
		data[i] = byte(i)
	}
	w := NewValueWriter()
	w.data = append([]byte(nil), data...)
	root, err := w.Build()
	require.NoError(t, err)

	// A leaf carries its data directly and has no refs.
	require.Equal(t, data, root.Data())
	for i := 0; i < chunk.MaxRefs; i++ {
		require.Nil(t, root.RefAt(i), "leaf value must have no refs")
	}
}
