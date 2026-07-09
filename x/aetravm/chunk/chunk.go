package chunk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"lukechampine.com/blake3"
)

const (
	MaxDataBits	= 2048
	MaxRefs		= 8
	HashSize	= 32
	// MaxChunkTreeDepth caps the nesting depth of a serialized chunk tree on
	// the decode path as defense-in-depth against maliciously deep inputs.
	// The Build path already implies a ~255-level cap (level is uint8); 256 is
	// a safe explicit bound.
	MaxChunkTreeDepth = 256
)

// Structured ref indices
const (
	RefData0	= 0
	RefData1	= 1
	RefData2	= 2
	RefData3	= 3
	RefControl0	= 4
	RefControl1	= 5
	RefMetadata	= 6
	RefSystem	= 7
)

type TypeTag uint8

const (
	TypeNormal	TypeTag	= 0
	TypePruned	TypeTag	= 1
	TypeSnapshot	TypeTag	= 2
	TypeDiff	TypeTag	= 3
	TypeProof	TypeTag	= 4
	TypeSystem	TypeTag	= 5
)

// Chunk represents a content-addressed, immutable Merkle DAG node.
type Chunk struct {
	typeTag		TypeTag
	level		uint8
	data		[]byte
	bitCount	uint16
	refBitmap	uint8
	refs		[MaxRefs]*Chunk
	hashes		[][HashSize]byte
}

func (c *Chunk) TypeTag() TypeTag	{ return c.typeTag }
func (c *Chunk) Level() uint8		{ return c.level }
func (c *Chunk) Data() []byte		{ return c.data }
func (c *Chunk) BitCount() uint16	{ return c.bitCount }

func (c *Chunk) Refs() []*Chunk {
	var out []*Chunk
	for i := 0; i < MaxRefs; i++ {
		if (c.refBitmap & (1 << i)) != 0 {
			out = append(out, c.refs[i])
		}
	}
	return out
}

func (c *Chunk) RefAt(i int) *Chunk {
	if i < 0 || i >= MaxRefs {
		return nil
	}
	if (c.refBitmap & (1 << i)) == 0 {
		return nil
	}
	return c.refs[i]
}

func (c *Chunk) RefHashes(layer int) [][]byte {
	var out [][]byte
	for i := 0; i < MaxRefs; i++ {
		if (c.refBitmap & (1 << i)) != 0 {
			out = append(out, c.refs[i].hashes[layer][:])
		}
	}
	return out
}

func (c *Chunk) Hash() []byte	{ return c.hashes[0][:] }
func (c *Chunk) HashLayer(i int) []byte {
	if i >= len(c.hashes) {
		return nil
	}
	return c.hashes[i][:]
}

// Serialize returns the canonical byte representation of the chunk's own
// header and data. Child references are written as hashes only (matching
// the hashing scheme in encodeCanonical), so this form is not sufficient to
// reconstruct a full tree — use SerializeTree/ParseChunkTree for that.
func (c *Chunk) Serialize() ([]byte, error) {
	var buf bytes.Buffer
	if err := c.encodeCanonical(&buf, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SerializeTree returns a self-contained byte representation of the chunk
// and its entire ref subtree (recursively), sufficient for ParseChunkTree to
// reconstruct an identical tree with the same Hash(). Unlike Serialize, this
// embeds full child content instead of child hashes, so it round-trips
// through storage without losing data.
func (c *Chunk) SerializeTree() ([]byte, error) {
	var buf bytes.Buffer
	if err := c.encodeTree(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Chunk) encodeTree(w io.Writer) error {
	if err := binary.Write(w, binary.BigEndian, uint8(c.typeTag)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, c.level); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, c.bitCount); err != nil {
		return err
	}
	byteLen := (int(c.bitCount) + 7) / 8
	if byteLen > len(c.data) {
		return fmt.Errorf("chunk data shorter than bit count")
	}
	if _, err := w.Write(c.data[:byteLen]); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, c.refBitmap); err != nil {
		return err
	}
	for i := 0; i < MaxRefs; i++ {
		if (c.refBitmap & (1 << i)) == 0 {
			continue
		}
		childBytes, err := c.refs[i].encodeTreeBytes()
		if err != nil {
			return err
		}
		if err := binary.Write(w, binary.BigEndian, uint32(len(childBytes))); err != nil {
			return err
		}
		if _, err := w.Write(childBytes); err != nil {
			return err
		}
	}
	return nil
}

func (c *Chunk) encodeTreeBytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := c.encodeTree(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseChunkTree reconstructs a chunk (and its full ref subtree) from bytes
// produced by SerializeTree. Hashes are recomputed bottom-up via the normal
// Builder path, so the result is indistinguishable from the original tree,
// including Hash().
func ParseChunkTree(data []byte) (*Chunk, int, error) {
	return parseChunkTreeAt(data, 0)
}

// parseChunkTreeAt is the depth-tracking recursive implementation behind
// ParseChunkTree. depth is the current nesting level; it is incremented for
// each child so maliciously deep serialized trees are rejected with an error
// rather than exhausting the stack.
func parseChunkTreeAt(data []byte, depth int) (*Chunk, int, error) {
	if depth > MaxChunkTreeDepth {
		return nil, 0, fmt.Errorf("chunk tree: nesting depth exceeds limit %d", MaxChunkTreeDepth)
	}
	if len(data) < 1+1+2+1 {
		return nil, 0, fmt.Errorf("chunk tree: truncated header")
	}
	offset := 0
	typeTag := TypeTag(data[offset])
	offset++
	level := data[offset]
	_ = level // recomputed by Build(); kept for forward-compat header shape
	offset++
	bitCount := binary.BigEndian.Uint16(data[offset:])
	offset += 2
	if bitCount > MaxDataBits {
		return nil, 0, fmt.Errorf("chunk tree: bit count %d exceeds limit %d", bitCount, MaxDataBits)
	}
	byteLen := (int(bitCount) + 7) / 8
	if len(data) < offset+byteLen+1 {
		return nil, 0, fmt.Errorf("chunk tree: truncated data")
	}
	chunkData := append([]byte(nil), data[offset:offset+byteLen]...)
	offset += byteLen
	refBitmap := data[offset]
	offset++

	builder := NewBuilder().SetTypeTag(typeTag).SetData(chunkData, bitCount)
	for i := 0; i < MaxRefs; i++ {
		if (refBitmap & (1 << i)) == 0 {
			continue
		}
		if len(data) < offset+4 {
			return nil, 0, fmt.Errorf("chunk tree: truncated ref length")
		}
		childLen := int(binary.BigEndian.Uint32(data[offset:]))
		offset += 4
		if childLen < 0 || len(data) < offset+childLen {
			return nil, 0, fmt.Errorf("chunk tree: truncated ref data")
		}
		child, consumed, err := parseChunkTreeAt(data[offset:offset+childLen], depth+1)
		if err != nil {
			return nil, 0, fmt.Errorf("chunk tree: ref %d: %w", i, err)
		}
		if consumed != childLen {
			return nil, 0, fmt.Errorf("chunk tree: ref %d has trailing bytes", i)
		}
		offset += childLen
		builder.SetRef(i, child)
	}

	built, err := builder.Build()
	if err != nil {
		return nil, 0, fmt.Errorf("chunk tree: rebuild: %w", err)
	}
	return built, offset, nil
}

// NewPrunedChunk creates a chunk that only contains its hashes and level,
// but no data or refs. Used for light sync and proofs.
func NewPrunedChunk(level uint8, hashes [][]byte) (*Chunk, error) {
	if len(hashes) == 0 {
		return nil, fmt.Errorf("pruned chunk must have at least H0")
	}
	c := &Chunk{
		typeTag:	TypePruned,
		level:		level,
		hashes:		make([][HashSize]byte, len(hashes)),
	}
	for i, h := range hashes {
		if len(h) != HashSize {
			return nil, fmt.Errorf("invalid hash size at layer %d", i)
		}
		copy(c.hashes[i][:], h)
	}
	return c, nil
}

// NewBuilder creates a new Chunk builder.
func NewBuilder() *Builder {
	return &Builder{}
}

type Builder struct {
	typeTag		TypeTag
	data		[]byte
	bitCount	uint16
	refBitmap	uint8
	refs		[MaxRefs]*Chunk
}

func (b *Builder) SetTypeTag(t TypeTag) *Builder {
	b.typeTag = t
	return b
}

func (b *Builder) SetData(data []byte, bitCount uint16) *Builder {
	b.data = data
	b.bitCount = bitCount
	return b
}

func (b *Builder) AddRef(ref *Chunk) *Builder {
	for i := 0; i < MaxRefs; i++ {
		if (b.refBitmap & (1 << i)) == 0 {
			return b.SetRef(i, ref)
		}
	}
	return b
}

func (b *Builder) SetRef(i int, ref *Chunk) *Builder {
	if i < 0 || i >= MaxRefs {
		return b
	}
	if ref == nil {
		b.refBitmap &= ^(1 << i)
		b.refs[i] = nil
	} else {
		b.refBitmap |= (1 << i)
		b.refs[i] = ref
	}
	return b
}

func (b *Builder) Build() (*Chunk, error) {
	if b.bitCount > MaxDataBits {
		return nil, fmt.Errorf("chunk data bits %d exceeds limit %d", b.bitCount, MaxDataBits)
	}

	c := &Chunk{
		typeTag:	b.typeTag,
		data:		append([]byte(nil), b.data...),
		bitCount:	b.bitCount,
		refBitmap:	b.refBitmap,
		refs:		b.refs,
	}

	// Calculate level: max(ref.level) + 1
	var maxLevel uint8
	hasRefs := false
	for i := 0; i < MaxRefs; i++ {
		if (b.refBitmap & (1 << i)) != 0 {
			hasRefs = true
			if b.refs[i].level > maxLevel {
				maxLevel = b.refs[i].level
			}
		}
	}
	if hasRefs {
		if maxLevel == 255 {
			return nil, fmt.Errorf("max chunk level reached")
		}
		c.level = maxLevel + 1
	} else {
		c.level = 0
	}

	h0, err := c.calculateHash(0)
	if err != nil {
		return nil, err
	}
	h1, err := c.calculateHash(1)
	if err != nil {
		return nil, err
	}

	c.hashes = [][HashSize]byte{h0, h1}

	return c, nil
}

func (c *Chunk) calculateHash(layer int) ([HashSize]byte, error) {
	h := blake3.New(HashSize, nil)
	if err := c.encodeCanonical(h, layer); err != nil {
		return [HashSize]byte{}, err
	}
	var out [HashSize]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

func (c *Chunk) encodeCanonical(w io.Writer, layer int) error {

	if err := binary.Write(w, binary.BigEndian, uint8(c.typeTag)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, c.level); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, c.bitCount); err != nil {
		return err
	}

	byteLen := (int(c.bitCount) + 7) / 8
	if byteLen > len(c.data) {
		return fmt.Errorf("chunk data shorter than bit count")
	}
	if _, err := w.Write(c.data[:byteLen]); err != nil {
		return err
	}

	if err := binary.Write(w, binary.BigEndian, c.refBitmap); err != nil {
		return err
	}

	for i := 0; i < MaxRefs; i++ {
		if (c.refBitmap & (1 << i)) != 0 {
			hash := c.refs[i].hashes[layer]
			if _, err := w.Write(hash[:]); err != nil {
				return err
			}
		}
	}

	return nil
}
