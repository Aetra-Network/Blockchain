package stdlib

import (
	"errors"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
)

type Cell struct {
	root *chunk.Chunk
}

type Slice struct {
	root *chunk.Chunk
}

func NewCell(data []byte, tag chunk.TypeTag) (Cell, error) {
	root, err := avm.BuildChunkPayload(data, tag)
	if err != nil {
		return Cell{}, err
	}
	return Cell{root: root}, nil
}

func CellFromChunk(root *chunk.Chunk) Cell {
	return Cell{root: root}
}

func (c Cell) Chunk() *chunk.Chunk {
	return c.root
}

func (c Cell) Bytes() ([]byte, error) {
	if c.root == nil {
		return nil, errors.New("cell is empty")
	}
	return avm.FlattenChunkPayload(c.root)
}

func (c Cell) Hash() [32]byte {
	if c.root == nil {
		return [32]byte{}
	}
	var out [32]byte
	copy(out[:], c.root.Hash())
	return out
}

func (c Cell) Slice() Slice {
	return Slice{root: c.root}
}

func NewSlice(data []byte, tag chunk.TypeTag) (Slice, error) {
	cell, err := NewCell(data, tag)
	if err != nil {
		return Slice{}, err
	}
	return cell.Slice(), nil
}

func (s Slice) Cell() Cell {
	return Cell{root: s.root}
}

func (s Slice) Bytes() ([]byte, error) {
	return s.Cell().Bytes()
}

func (s Slice) IsZero() bool {
	return s.root == nil || len(s.root.Data()) == 0
}

