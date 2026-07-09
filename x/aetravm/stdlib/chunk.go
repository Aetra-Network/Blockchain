package stdlib

import (
	"errors"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
)

type Chunk struct {
	root *chunk.Chunk
}

type Segment struct {
	root *chunk.Chunk
}

func NewChunk(data []byte, tag chunk.TypeTag) (Chunk, error) {
	root, err := avm.BuildChunkPayload(data, tag)
	if err != nil {
		return Chunk{}, err
	}
	return Chunk{root: root}, nil
}

func ChunkFromChunk(root *chunk.Chunk) Chunk {
	return Chunk{root: root}
}

func (c Chunk) Root() *chunk.Chunk {
	return c.root
}

func (c Chunk) Bytes() ([]byte, error) {
	if c.root == nil {
		return nil, errors.New("chunk is empty")
	}
	return avm.FlattenChunkPayload(c.root)
}

func (c Chunk) Hash() [32]byte {
	if c.root == nil {
		return [32]byte{}
	}
	var out [32]byte
	copy(out[:], c.root.Hash())
	return out
}

func (c Chunk) BitsHash() [32]byte {
	if c.root == nil {
		return [32]byte{}
	}
	var out [32]byte
	copy(out[:], c.root.HashLayer(1))
	return out
}

func (c Chunk) Segment() Segment {
	return Segment{root: c.root}
}

func NewSegment(data []byte, tag chunk.TypeTag) (Segment, error) {
	chunkValue, err := NewChunk(data, tag)
	if err != nil {
		return Segment{}, err
	}
	return chunkValue.Segment(), nil
}

func SegmentFromChunk(root *chunk.Chunk) Segment {
	return Segment{root: root}
}

func (s Segment) Root() *chunk.Chunk {
	return s.root
}

func (s Segment) Bytes() ([]byte, error) {
	return Chunk{root: s.root}.Bytes()
}

func (s Segment) Hash() [32]byte {
	return Chunk{root: s.root}.Hash()
}

func (s Segment) BitsHash() [32]byte {
	return Chunk{root: s.root}.BitsHash()
}

func (s Segment) IsZero() bool {
	return s.root == nil || len(s.root.Data()) == 0
}
