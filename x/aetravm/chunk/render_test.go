package chunk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderSourceTree(t *testing.T) {
	left, err := NewBuilder().SetTypeTag(TypeNormal).SetData([]byte{0xAA, 0xBB}, 16).Build()
	require.NoError(t, err)
	right, err := NewBuilder().SetTypeTag(TypeNormal).SetData([]byte{0xCC}, 8).Build()
	require.NoError(t, err)
	root, err := NewBuilder().SetTypeTag(TypeNormal).SetRef(0, left).SetRef(3, right).Build()
	require.NoError(t, err)

	require.Equal(t, "[\n  [AABB]\n  [CC]\n]", RenderSource(root))
}
