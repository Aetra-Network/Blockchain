package chunk

// BuildTree packs an arbitrary byte blob into the canonical chunk tree: a
// blob that fits in one chunk becomes a single data chunk; larger blobs are
// split into up to MaxRefs parts recursively. This is THE canonical packing —
// the compiler uses it for the module code chunk (CodeChunkHash), and
// explorers use it to render the "cells" view of bytecode and raw data, so
// the hashes shown off-chain match the on-chain commitments.
func BuildTree(data []byte) (*Chunk, error) {
	if len(data) == 0 {
		return NewEmptyChunk(), nil
	}
	if len(data) <= MaxDataBits/8 {
		return NewBuilder().SetTypeTag(TypeNormal).SetData(data, uint16(len(data)*8)).Build()
	}
	parts := splitParts(data, MaxRefs)
	builder := NewBuilder().SetTypeTag(TypeNormal)
	for i, part := range parts {
		child, err := BuildTree(part)
		if err != nil {
			return nil, err
		}
		builder.SetRef(i, child)
	}
	return builder.Build()
}

// splitParts divides data into at most maxParts contiguous slices, merging
// evenly when a first pass produces too many.
func splitParts(data []byte, maxParts int) [][]byte {
	if len(data) == 0 {
		return nil
	}
	if maxParts <= 1 {
		return [][]byte{append([]byte(nil), data...)}
	}
	partSize := (len(data) + maxParts - 1) / maxParts
	var out [][]byte
	for i := 0; i < len(data); i += partSize {
		end := i + partSize
		if end > len(data) {
			end = len(data)
		}
		out = append(out, append([]byte(nil), data[i:end]...))
	}
	for len(out) > maxParts {
		next := make([][]byte, 0, maxParts)
		groupSize := (len(out) + maxParts - 1) / maxParts
		for i := 0; i < len(out); i += groupSize {
			end := i + groupSize
			if end > len(out) {
				end = len(out)
			}
			var merged []byte
			for _, part := range out[i:end] {
				merged = append(merged, part...)
			}
			next = append(next, merged)
		}
		out = next
	}
	return out
}
