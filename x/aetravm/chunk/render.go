package chunk

import (
	"encoding/hex"
	"strings"
)

// RenderSource renders a chunk tree in the minimal AVM source format.
// It exposes only payload bytes and ref structure in positional order.
func RenderSource(root *Chunk) string {
	var b strings.Builder
	renderSourceChunk(&b, root, 0)
	return strings.TrimRight(b.String(), "\n")
}

func renderSourceChunk(b *strings.Builder, node *Chunk, depth int) {
	if node == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	b.WriteString(indent)
	b.WriteString("[")
	byteLen := int((node.BitCount() + 7) / 8)
	if byteLen > 0 {
		b.WriteString(strings.ToUpper(hex.EncodeToString(node.Data()[:byteLen])))
	}
	hasChild := false
	for i := 0; i < MaxRefs; i++ {
		child := node.RefAt(i)
		if child == nil {
			continue
		}
		if !hasChild {
			hasChild = true
		}
		b.WriteString("\n")
		renderSourceChunk(b, child, depth+1)
	}
	if hasChild {
		b.WriteString("\n")
		b.WriteString(indent)
	}
	b.WriteString("]")
}
