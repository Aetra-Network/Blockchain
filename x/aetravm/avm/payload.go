package avm

import (
	"errors"
	"fmt"

	"github.com/sovereign-l1/l1/x/aetravm/chunk"
)

const maxPayloadChunkDepth = 4096

// BuildChunkPayload constructs a canonical chunk tree for an arbitrary payload.
// Small payloads stay in a single chunk. Larger payloads are split into a stable
// 8-way tree so code/state/message bodies can spill over without changing hash
// determinism.
func BuildChunkPayload(data []byte, typeTag chunk.TypeTag) (*chunk.Chunk, error) {
	if typeTag == chunk.TypePruned {
		return nil, errors.New("AVM chunk payload cannot use pruned chunk type")
	}
	return buildChunkPayload(data, typeTag, 0)
}

func buildChunkPayload(data []byte, typeTag chunk.TypeTag, depth int) (*chunk.Chunk, error) {
	if depth > maxPayloadChunkDepth {
		return nil, errors.New("AVM chunk payload spillover depth exceeded")
	}
	if len(data) <= chunk.MaxDataBits/8 {
		return chunk.NewBuilder().
			SetTypeTag(typeTag).
			SetData(data, uint16(len(data))*8).
			Build()
	}

	partCount := (len(data) + (chunk.MaxDataBits / 8) - 1) / (chunk.MaxDataBits / 8)
	if partCount > chunk.MaxRefs {
		partCount = chunk.MaxRefs
	}
	partSize := (len(data) + partCount - 1) / partCount

	builder := chunk.NewBuilder().SetTypeTag(typeTag)
	for i := 0; i < partCount; i++ {
		start := i * partSize
		if start >= len(data) {
			break
		}
		end := start + partSize
		if end > len(data) {
			end = len(data)
		}
		child, err := buildChunkPayload(data[start:end], typeTag, depth+1)
		if err != nil {
			return nil, err
		}
		builder.SetRef(i, child)
	}
	return builder.Build()
}

// FlattenChunkPayload reconstructs payload bytes from a canonical chunk tree.
// It accepts both leaf chunks and spillover trees and rejects pruned chunks.
func FlattenChunkPayload(root *chunk.Chunk) ([]byte, error) {
	if root == nil {
		return nil, errors.New("AVM chunk payload must not be nil")
	}
	if root.TypeTag() == chunk.TypePruned {
		return nil, errors.New("AVM chunk payload cannot be a pruned chunk")
	}
	return flattenChunkPayload(root, 0)
}

func flattenChunkPayload(root *chunk.Chunk, depth int) ([]byte, error) {
	if depth > maxPayloadChunkDepth {
		return nil, errors.New("AVM chunk payload spillover depth exceeded")
	}
	if root == nil {
		return nil, nil
	}
	if root.TypeTag() == chunk.TypePruned {
		return nil, errors.New("AVM chunk payload cannot be a pruned chunk")
	}

	out := append([]byte(nil), root.Data()...)
	for i := 0; i < chunk.MaxRefs; i++ {
		child := root.RefAt(i)
		if child == nil {
			continue
		}
		childBytes, err := flattenChunkPayload(child, depth+1)
		if err != nil {
			return nil, fmt.Errorf("AVM chunk payload ref %d: %w", i, err)
		}
		out = append(out, childBytes...)
	}
	return out, nil
}
