package avm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"sort"
	"unicode/utf8"

	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
	"lukechampine.com/blake3"
)

// ValueTag defines the runtime type tag for AVM values.
// Every Value MUST carry a deterministic type tag.
// No untyped values are allowed in runtime.
type ValueTag uint8

const (
	TagNull ValueTag = iota
	TagBool
	TagInt8
	TagInt16
	TagInt32
	TagInt64
	TagInt128
	TagInt256
	TagUint8
	TagUint16
	TagUint32
	TagUint64
	TagUint128
	TagUint256
	TagCoins
	TagTimestamp
	TagAddress
	TagHash
	TagBytes
	TagString
	TagTuple
	TagChunkRef
	TagReaderCursor
	TagWriterHandle
	TagExecFrameRef
	TagMap
)

func (t ValueTag) String() string {
	switch t {
	case TagNull:
		return "null"
	case TagBool:
		return "bool"
	case TagInt8:
		return "int8"
	case TagInt16:
		return "int16"
	case TagInt32:
		return "int32"
	case TagInt64:
		return "int64"
	case TagInt128:
		return "int128"
	case TagInt256:
		return "int256"
	case TagUint8:
		return "uint8"
	case TagUint16:
		return "uint16"
	case TagUint32:
		return "uint32"
	case TagUint64:
		return "uint64"
	case TagUint128:
		return "uint128"
	case TagUint256:
		return "uint256"
	case TagCoins:
		return "coins"
	case TagTimestamp:
		return "timestamp"
	case TagAddress:
		return "address"
	case TagHash:
		return "hash"
	case TagBytes:
		return "bytes"
	case TagString:
		return "string"
	case TagTuple:
		return "tuple"
	case TagChunkRef:
		return "chunk_ref"
	case TagReaderCursor:
		return "reader_cursor"
	case TagWriterHandle:
		return "writer_handle"
	case TagExecFrameRef:
		return "exec_frame_ref"
	case TagMap:
		return "map"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// ValueBitWidth returns the bit width for integer types.
func ValueBitWidth(tag ValueTag) (int, bool) {
	switch tag {
	case TagInt8, TagUint8:
		return 8, true
	case TagInt16, TagUint16:
		return 16, true
	case TagInt32, TagUint32:
		return 32, true
	case TagInt64, TagUint64:
		return 64, true
	case TagInt128, TagUint128, TagCoins:
		return 128, true
	case TagInt256, TagUint256:
		return 256, true
	default:
		return 0, false
	}
}

// IsSigned returns true for signed integer types.
func IsSigned(tag ValueTag) bool {
	switch tag {
	case TagInt8, TagInt16, TagInt32, TagInt64, TagInt128, TagInt256:
		return true
	default:
		return false
	}
}

// IsInteger returns true for any integer type.
func IsInteger(tag ValueTag) bool {
	_, ok := ValueBitWidth(tag)
	return ok
}

// RuntimeValue is the AVM runtime tagged union value.
// All runtime values MUST be one of these.
// No untyped values, no implicit casts, no runtime reflection.
type RuntimeValue struct {
	Tag       ValueTag
	boolVal   bool
	intVal    *big.Int
	coinsVal  [16]byte
	addrVal   string
	stateInit *contracttypes.StateInit
	hashVal   [32]byte
	bytesVal  []byte
	strVal    string
	tupleVal  []RuntimeValue
	mapVal    []runtimeMapEntry
	chunkRef  *chunk.Chunk
	readerOff uint32
	writerPtr *ValueWriter
	frameRef  *KernelExecutionFrame
}

type runtimeMapEntry struct {
	Key      RuntimeValue
	Value    RuntimeValue
	keyBytes []byte
}

// runtimeValueSizeUnits returns a deterministic measure of the O(N) work
// that cloning, normalizing, or canonically encoding v would perform: the
// number of map entries, tuple elements, or bytes/string length. Runtime
// values of any other tag (integers, bool, address, hash, coins,
// timestamp, ...) are fixed-size regardless of their contents, so this is
// 0 for them -- opcodes that only ever touch such values are charged no
// extra gas by Params.GasPerOperandUnit.
//
// This is the size metric behind the FINDING-001 fix
// (security-audit/05-findings/FINDING-001-avm-gas-mispricing-dos.md): every
// AVM opcode that clones or iterates a value proportional to this size
// (OpDup, OpLoadLocal, OpStoreLocal, OpReturn, the OpMap* family, and
// OpReadStorage's whole-state snapshot form) must charge gas proportional
// to it, not a flat per-opcode constant, since maps in particular have no
// runtime cap on element count.
func runtimeValueSizeUnits(v RuntimeValue) uint64 {
	switch v.Tag {
	case TagMap:
		return uint64(len(v.mapVal))
	case TagTuple:
		return uint64(len(v.tupleVal))
	case TagBytes:
		return uint64(len(v.bytesVal))
	case TagString:
		return uint64(len(v.strVal))
	default:
		return 0
	}
}

func runtimeMapEntryFrom(key, value RuntimeValue) (runtimeMapEntry, error) {
	keyBytes, err := CanonicalEncode(key)
	if err != nil {
		return runtimeMapEntry{}, err
	}
	return runtimeMapEntry{Key: key.clone(), Value: value.clone(), keyBytes: keyBytes}, nil
}

func runtimeMapClone(entries []runtimeMapEntry) []runtimeMapEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]runtimeMapEntry, len(entries))
	for i, entry := range entries {
		out[i] = runtimeMapEntry{
			Key:      entry.Key.clone(),
			Value:    entry.Value.clone(),
			keyBytes: append([]byte(nil), entry.keyBytes...),
		}
	}
	return out
}

func runtimeMapNormalize(entries []runtimeMapEntry) ([]runtimeMapEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	type indexedEntry struct {
		entry    runtimeMapEntry
		keyBytes []byte
	}
	normalized := make([]indexedEntry, 0, len(entries))
	for _, entry := range entries {
		keyBytes := entry.keyBytes
		if len(keyBytes) == 0 {
			var err error
			keyBytes, err = CanonicalEncode(entry.Key)
			if err != nil {
				return nil, err
			}
		}
		normalized = append(normalized, indexedEntry{
			entry: runtimeMapEntry{
				Key:      entry.Key.clone(),
				Value:    entry.Value.clone(),
				keyBytes: append([]byte(nil), keyBytes...),
			},
			keyBytes: append([]byte(nil), keyBytes...),
		})
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return bytes.Compare(normalized[i].keyBytes, normalized[j].keyBytes) < 0
	})
	out := make([]runtimeMapEntry, 0, len(normalized))
	for _, item := range normalized {
		if len(out) > 0 && bytes.Equal(out[len(out)-1].keyBytes, item.keyBytes) {
			out[len(out)-1] = item.entry
			continue
		}
		out = append(out, item.entry)
	}
	return out, nil
}

func runtimeMapEmpty() []runtimeMapEntry {
	return []runtimeMapEntry{}
}

func runtimeMapLen(entries []runtimeMapEntry) int {
	return len(entries)
}

func runtimeMapLookup(entries []runtimeMapEntry, key RuntimeValue) (RuntimeValue, bool, error) {
	keyBytes, err := CanonicalEncode(key)
	if err != nil {
		return RuntimeValue{}, false, err
	}
	if len(entries) == 0 {
		return RuntimeValue{}, false, nil
	}
	lo, hi := 0, len(entries)
	for lo < hi {
		mid := lo + (hi-lo)/2
		cmp := bytes.Compare(entries[mid].keyBytes, keyBytes)
		switch {
		case cmp < 0:
			lo = mid + 1
		case cmp > 0:
			hi = mid
		default:
			return entries[mid].Value.clone(), true, nil
		}
	}
	return RuntimeValue{}, false, nil
}

func runtimeMapSet(entries []runtimeMapEntry, key, value RuntimeValue) ([]runtimeMapEntry, error) {
	keyBytes, err := CanonicalEncode(key)
	if err != nil {
		return nil, err
	}
	out := make([]runtimeMapEntry, 0, len(entries)+1)
	inserted := false
	for _, entry := range entries {
		cmp := bytes.Compare(entry.keyBytes, keyBytes)
		if cmp == 0 {
			if !inserted {
				out = append(out, runtimeMapEntry{Key: key.clone(), Value: value.clone(), keyBytes: append([]byte(nil), keyBytes...)})
				inserted = true
			}
			continue
		}
		if !inserted && cmp > 0 {
			out = append(out, runtimeMapEntry{Key: key.clone(), Value: value.clone(), keyBytes: append([]byte(nil), keyBytes...)})
			inserted = true
		}
		out = append(out, runtimeMapEntry{
			Key:      entry.Key.clone(),
			Value:    entry.Value.clone(),
			keyBytes: append([]byte(nil), entry.keyBytes...),
		})
	}
	if !inserted {
		out = append(out, runtimeMapEntry{Key: key.clone(), Value: value.clone(), keyBytes: append([]byte(nil), keyBytes...)})
	}
	return out, nil
}

func runtimeMapDelete(entries []runtimeMapEntry, key RuntimeValue) ([]runtimeMapEntry, error) {
	keyBytes, err := CanonicalEncode(key)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return runtimeMapEmpty(), nil
	}
	out := make([]runtimeMapEntry, 0, len(entries))
	removed := false
	for _, entry := range entries {
		if !removed && bytes.Equal(entry.keyBytes, keyBytes) {
			removed = true
			continue
		}
		out = append(out, runtimeMapEntry{
			Key:      entry.Key.clone(),
			Value:    entry.Value.clone(),
			keyBytes: append([]byte(nil), entry.keyBytes...),
		})
	}
	return out, nil
}

func runtimeMapKeys(entries []runtimeMapEntry, limit uint64) RuntimeValue {
	if limit == 0 || len(entries) == 0 {
		return ValueEmptyTuple()
	}
	if limit > uint64(len(entries)) {
		limit = uint64(len(entries))
	}
	out := make([]RuntimeValue, 0, limit)
	for i := uint64(0); i < limit; i++ {
		out = append(out, entries[i].Key.clone())
	}
	return ValueTuple(out)
}

func runtimeMapEntriesValue(entries []runtimeMapEntry, limit uint64) RuntimeValue {
	if limit == 0 || len(entries) == 0 {
		return ValueEmptyTuple()
	}
	if limit > uint64(len(entries)) {
		limit = uint64(len(entries))
	}
	out := make([]RuntimeValue, 0, limit)
	for i := uint64(0); i < limit; i++ {
		out = append(out, ValueTuple([]RuntimeValue{entries[i].Key.clone(), entries[i].Value.clone()}))
	}
	return ValueTuple(out)
}

func ValueNull() RuntimeValue {
	return RuntimeValue{Tag: TagNull}
}

func ValueBool(v bool) RuntimeValue {
	return RuntimeValue{Tag: TagBool, boolVal: v}
}

func ValueInt8(v int8) RuntimeValue {
	return RuntimeValue{Tag: TagInt8, intVal: big.NewInt(int64(v))}
}

func ValueInt16(v int16) RuntimeValue {
	return RuntimeValue{Tag: TagInt16, intVal: big.NewInt(int64(v))}
}

func ValueInt32(v int32) RuntimeValue {
	return RuntimeValue{Tag: TagInt32, intVal: big.NewInt(int64(v))}
}

func ValueInt64(v int64) RuntimeValue {
	return RuntimeValue{Tag: TagInt64, intVal: big.NewInt(v)}
}

func ValueUint8(v uint8) RuntimeValue {
	return RuntimeValue{Tag: TagUint8, intVal: new(big.Int).SetUint64(uint64(v))}
}

func ValueUint16(v uint16) RuntimeValue {
	return RuntimeValue{Tag: TagUint16, intVal: new(big.Int).SetUint64(uint64(v))}
}

func ValueUint32(v uint32) RuntimeValue {
	return RuntimeValue{Tag: TagUint32, intVal: new(big.Int).SetUint64(uint64(v))}
}

func ValueUint64(v uint64) RuntimeValue {
	return RuntimeValue{Tag: TagUint64, intVal: new(big.Int).SetUint64(v)}
}

func ValueBigUint128(v *big.Int) RuntimeValue {
	b := make([]byte, 16)
	v.FillBytes(b)
	val := [16]byte{}
	copy(val[:], b)
	return RuntimeValue{Tag: TagUint128, intVal: new(big.Int).Set(v), coinsVal: val}
}

func ValueBigInt256(v *big.Int) RuntimeValue {
	return RuntimeValue{Tag: TagInt256, intVal: new(big.Int).Set(v)}
}

func ValueCoins(v *big.Int) RuntimeValue {
	b := make([]byte, 16)
	v.FillBytes(b)
	val := [16]byte{}
	copy(val[:], b)
	return RuntimeValue{Tag: TagCoins, intVal: new(big.Int).Set(v), coinsVal: val}
}

func ValueTimestamp(v uint64) RuntimeValue {
	return RuntimeValue{Tag: TagTimestamp, intVal: new(big.Int).SetUint64(v)}
}

func ValueAddress(addr string) RuntimeValue {
	return RuntimeValue{Tag: TagAddress, addrVal: addr}
}

func ValueAddressWithStateInit(addr string, stateInit *contracttypes.StateInit) RuntimeValue {
	out := ValueAddress(addr)
	if stateInit != nil {
		normalized := stateInit.Normalize()
		out.stateInit = &normalized
	}
	return out
}

func ValueHash(h [32]byte) RuntimeValue {
	return RuntimeValue{Tag: TagHash, hashVal: h}
}

func ValueHashFromBytes(h []byte) RuntimeValue {
	var hash [32]byte
	copy(hash[:], h)
	return RuntimeValue{Tag: TagHash, hashVal: hash}
}

func ValueBytes(b []byte) RuntimeValue {
	cp := make([]byte, len(b))
	copy(cp, b)
	return RuntimeValue{Tag: TagBytes, bytesVal: cp}
}

func ValueString(s string) RuntimeValue {
	return RuntimeValue{Tag: TagString, strVal: s}
}

func ValueTuple(elements []RuntimeValue) RuntimeValue {
	cp := make([]RuntimeValue, len(elements))
	copy(cp, elements)
	return RuntimeValue{Tag: TagTuple, tupleVal: cp}
}

func ValueEmptyTuple() RuntimeValue {
	return RuntimeValue{Tag: TagTuple, tupleVal: []RuntimeValue{}}
}

func ValueChunkRef(c *chunk.Chunk) RuntimeValue {
	return RuntimeValue{Tag: TagChunkRef, chunkRef: c}
}

func ValueReaderCursor(c *chunk.Chunk, offset uint32) RuntimeValue {
	return RuntimeValue{Tag: TagReaderCursor, chunkRef: c, readerOff: offset}
}

func ValueWriterHandle(w *ValueWriter) RuntimeValue {
	return RuntimeValue{Tag: TagWriterHandle, writerPtr: w}
}

func ValueExecFrameRef(f *KernelExecutionFrame) RuntimeValue {
	return RuntimeValue{Tag: TagExecFrameRef, frameRef: f}
}

func ValueMapEmpty() RuntimeValue {
	return RuntimeValue{Tag: TagMap, mapVal: runtimeMapEmpty()}
}

func ValueMap(entries []runtimeMapEntry) RuntimeValue {
	normalized, err := runtimeMapNormalize(entries)
	if err != nil {
		return RuntimeValue{Tag: TagMap}
	}
	return RuntimeValue{Tag: TagMap, mapVal: normalized}
}

func (v RuntimeValue) AsBool() (bool, error) {
	if v.Tag != TagBool {
		return false, typeError(TagBool, v.Tag)
	}
	return v.boolVal, nil
}

func (v RuntimeValue) AsInt64() (int64, error) {
	if !IsSignedInteger(v.Tag) {
		return 0, typeError(TagInt64, v.Tag)
	}
	if v.intVal == nil {
		return 0, fmt.Errorf("AVM: nil int value")
	}
	if !v.intVal.IsInt64() {
		return 0, fmt.Errorf("AVM: int value overflows int64")
	}
	return v.intVal.Int64(), nil
}

func (v RuntimeValue) AsUint64() (uint64, error) {
	if !IsUnsignedInteger(v.Tag) && v.Tag != TagTimestamp {
		return 0, typeError(TagUint64, v.Tag)
	}
	if v.intVal == nil {
		return 0, fmt.Errorf("AVM: nil uint value")
	}
	if !v.intVal.IsUint64() {
		return 0, fmt.Errorf("AVM: uint value overflows uint64")
	}
	return v.intVal.Uint64(), nil
}

func (v RuntimeValue) AsBigInt() (*big.Int, error) {
	if !IsInteger(v.Tag) && v.Tag != TagCoins {
		return nil, typeError(TagInt256, v.Tag)
	}
	if v.intVal == nil {
		return nil, fmt.Errorf("AVM: nil big int value")
	}
	return new(big.Int).Set(v.intVal), nil
}

func (v RuntimeValue) AsAddress() (string, error) {
	if v.Tag != TagAddress {
		return "", typeError(TagAddress, v.Tag)
	}
	return v.addrVal, nil
}

func (v RuntimeValue) AsHash() ([32]byte, error) {
	if v.Tag != TagHash {
		return [32]byte{}, typeError(TagHash, v.Tag)
	}
	return v.hashVal, nil
}

func (v RuntimeValue) AsBytes() ([]byte, error) {
	if v.Tag != TagBytes && v.Tag != TagString {
		return nil, typeError(TagBytes, v.Tag)
	}
	if v.Tag == TagBytes {
		cp := make([]byte, len(v.bytesVal))
		copy(cp, v.bytesVal)
		return cp, nil
	}
	return []byte(v.strVal), nil
}

func (v RuntimeValue) AsString() (string, error) {
	if v.Tag != TagString {
		return "", typeError(TagString, v.Tag)
	}
	return v.strVal, nil
}

func (v RuntimeValue) AsTuple() ([]RuntimeValue, error) {
	if v.Tag != TagTuple {
		return nil, typeError(TagTuple, v.Tag)
	}
	return v.tupleVal, nil
}

func (v RuntimeValue) AsMap() ([]runtimeMapEntry, error) {
	if v.Tag != TagMap {
		return nil, typeError(TagMap, v.Tag)
	}
	return runtimeMapClone(v.mapVal), nil
}

func (v RuntimeValue) AsChunkRef() (*chunk.Chunk, error) {
	if v.Tag != TagChunkRef {
		return nil, typeError(TagChunkRef, v.Tag)
	}
	return v.chunkRef, nil
}

func (v RuntimeValue) AsReaderCursor() (*chunk.Chunk, uint32, error) {
	if v.Tag != TagReaderCursor {
		return nil, 0, typeError(TagReaderCursor, v.Tag)
	}
	return v.chunkRef, v.readerOff, nil
}

func (v RuntimeValue) AsWriterHandle() (*ValueWriter, error) {
	if v.Tag != TagWriterHandle {
		return nil, typeError(TagWriterHandle, v.Tag)
	}
	return v.writerPtr, nil
}

func (v RuntimeValue) AsExecFrameRef() (*KernelExecutionFrame, error) {
	if v.Tag != TagExecFrameRef {
		return nil, typeError(TagExecFrameRef, v.Tag)
	}
	return v.frameRef, nil
}

func (v RuntimeValue) IsNull() bool  { return v.Tag == TagNull }
func (v RuntimeValue) IsBool() bool  { return v.Tag == TagBool }
func (v RuntimeValue) IsInt() bool   { return IsSignedInteger(v.Tag) }
func (v RuntimeValue) IsUint() bool  { return IsUnsignedInteger(v.Tag) }
func (v RuntimeValue) IsCoins() bool { return v.Tag == TagCoins }
func (v RuntimeValue) IsMap() bool    { return v.Tag == TagMap }

func IsSignedInteger(tag ValueTag) bool {
	return tag == TagInt8 || tag == TagInt16 || tag == TagInt32 || tag == TagInt64 || tag == TagInt128 || tag == TagInt256
}

func IsUnsignedInteger(tag ValueTag) bool {
	return tag == TagUint8 || tag == TagUint16 || tag == TagUint32 || tag == TagUint64 || tag == TagUint128 || tag == TagUint256
}

// CanonicalEncode encodes a RuntimeValue into deterministic bytes.
func CanonicalEncode(v RuntimeValue) ([]byte, error) {
	buf := []byte{byte(v.Tag)}

	switch v.Tag {
	case TagNull:

	case TagBool:
		if v.boolVal {
			buf = append(buf, 0x01)
		} else {
			buf = append(buf, 0x00)
		}
	case TagInt8, TagUint8:
		encoded, err := encodeIntBytes(v.intVal, 1, IsSigned(v.Tag))
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	case TagInt16, TagUint16:
		encoded, err := encodeIntBytes(v.intVal, 2, IsSigned(v.Tag))
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	case TagInt32, TagUint32:
		encoded, err := encodeIntBytes(v.intVal, 4, IsSigned(v.Tag))
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	case TagInt64, TagUint64:
		encoded, err := encodeIntBytes(v.intVal, 8, IsSigned(v.Tag))
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	case TagInt128, TagUint128, TagCoins:
		encoded, err := encodeIntBytes(v.intVal, 16, IsSigned(v.Tag))
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	case TagInt256, TagUint256:
		encoded, err := encodeIntBytes(v.intVal, 32, IsSigned(v.Tag))
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	case TagTimestamp:
		encoded, err := encodeIntBytes(v.intVal, 8, false)
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	case TagAddress:
		addrBytes := []byte(v.addrVal)
		buf = append(buf, encodeLengthPrefix(uint32(len(addrBytes)))...)
		buf = append(buf, addrBytes...)
	case TagHash:
		buf = append(buf, v.hashVal[:]...)
	case TagBytes:
		buf = append(buf, encodeLengthPrefix(uint32(len(v.bytesVal)))...)
		buf = append(buf, v.bytesVal...)
	case TagString:
		if !utf8.ValidString(v.strVal) {
			return nil, fmt.Errorf("AVM: invalid UTF-8 string")
		}
		strBytes := []byte(v.strVal)
		buf = append(buf, encodeLengthPrefix(uint32(len(strBytes)))...)
		buf = append(buf, strBytes...)
	case TagTuple:
		buf = append(buf, encodeLengthPrefix(uint32(len(v.tupleVal)))...)
		for i, elem := range v.tupleVal {
			encoded, err := CanonicalEncode(elem)
			if err != nil {
				return nil, fmt.Errorf("AVM: tuple element %d: %w", i, err)
			}
			buf = append(buf, encoded...)
		}
	case TagMap:
		entries, err := runtimeMapNormalize(v.mapVal)
		if err != nil {
			return nil, fmt.Errorf("AVM: map: %w", err)
		}
		buf = append(buf, encodeLengthPrefix(uint32(len(entries)))...)
		for i, entry := range entries {
			keyBytes, err := CanonicalEncode(entry.Key)
			if err != nil {
				return nil, fmt.Errorf("AVM: map key %d: %w", i, err)
			}
			valueBytes, err := CanonicalEncode(entry.Value)
			if err != nil {
				return nil, fmt.Errorf("AVM: map value %d: %w", i, err)
			}
			buf = append(buf, encodeLengthPrefix(uint32(len(keyBytes)))...)
			buf = append(buf, keyBytes...)
			buf = append(buf, encodeLengthPrefix(uint32(len(valueBytes)))...)
			buf = append(buf, valueBytes...)
		}
	case TagChunkRef:
		// Chunk/Code values must survive a storage round trip so stored code
		// can later be used to deploy child contracts (autoDeployAddress /
		// counterfactualAddress read it back from state). A hash alone
		// cannot be reconstructed, so the full tree is embedded here.
		if v.chunkRef == nil {
			buf = append(buf, 0x00)
		} else {
			tree, err := v.chunkRef.SerializeTree()
			if err != nil {
				return nil, fmt.Errorf("AVM: chunk ref: %w", err)
			}
			if uint32(len(tree)) > MaxChunkTreeBytes {
				return nil, fmt.Errorf("AVM: chunk ref tree is %d bytes, exceeds limit %d", len(tree), MaxChunkTreeBytes)
			}
			buf = append(buf, 0x01)
			lenPrefix, err := encodeIntBytes(big.NewInt(int64(len(tree))), 4, false)
			if err != nil {
				return nil, err
			}
			buf = append(buf, lenPrefix...)
			buf = append(buf, tree...)
		}
	case TagReaderCursor:
		if v.chunkRef == nil {
			buf = append(buf, make([]byte, 32)...)
		} else {
			buf = append(buf, v.chunkRef.Hash()...)
		}
		encoded, err := encodeIntBytes(big.NewInt(int64(v.readerOff)), 4, false)
		if err != nil {
			return nil, err
		}
		buf = append(buf, encoded...)
	case TagWriterHandle:
		if v.writerPtr == nil {
			buf = append(buf, 0x00)
		} else {
			buf = append(buf, 0x01)

			buf = append(buf, make([]byte, 32)...)
		}
	case TagExecFrameRef:
		buf = append(buf, 0x00)
	default:
		return nil, fmt.Errorf("AVM: unknown value tag %d", v.Tag)
	}

	return buf, nil
}

// CanonicalDecode decodes a RuntimeValue from deterministic bytes.
func CanonicalDecode(data []byte) (RuntimeValue, int, error) {
	return canonicalDecodeAt(data, 0)
}

// canonicalDecodeAt is CanonicalDecode's recursion-depth-tracking
// implementation. depth is incremented on every tuple/map element so a
// crafted, deeply nested value is rejected once it exceeds
// MaxCanonicalDecodeDepth instead of exhausting the Go call stack.
func canonicalDecodeAt(data []byte, depth int) (RuntimeValue, int, error) {
	if depth > MaxCanonicalDecodeDepth {
		return RuntimeValue{}, 0, fmt.Errorf("AVM: canonical decode nesting depth exceeds limit %d", MaxCanonicalDecodeDepth)
	}
	if len(data) < 1 {
		return RuntimeValue{}, 0, fmt.Errorf("AVM: empty data for canonical decode")
	}

	tag := ValueTag(data[0])
	offset := 1

	switch tag {
	case TagNull:
		return ValueNull(), 1, nil
	case TagBool:
		if len(data) < offset+1 {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated bool")
		}
		return ValueBool(data[offset] != 0), 2, nil
	case TagInt8, TagUint8:
		width := 1
		if len(data) < offset+width {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated int8/uint8")
		}
		v := decodeIntBytes(data[offset:offset+width], IsSigned(tag))
		offset += width
		return RuntimeValue{Tag: tag, intVal: v}, offset, nil
	case TagInt16, TagUint16:
		width := 2
		if len(data) < offset+width {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated int16/uint16")
		}
		v := decodeIntBytes(data[offset:offset+width], IsSigned(tag))
		offset += width
		return RuntimeValue{Tag: tag, intVal: v}, offset, nil
	case TagInt32, TagUint32:
		width := 4
		if len(data) < offset+width {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated int32/uint32")
		}
		v := decodeIntBytes(data[offset:offset+width], IsSigned(tag))
		offset += width
		return RuntimeValue{Tag: tag, intVal: v}, offset, nil
	case TagInt64, TagUint64, TagTimestamp:
		width := 8
		if len(data) < offset+width {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated int64/uint64/timestamp")
		}
		v := decodeIntBytes(data[offset:offset+width], IsSigned(tag))
		offset += width
		return RuntimeValue{Tag: tag, intVal: v}, offset, nil
	case TagInt128, TagUint128, TagCoins:
		width := 16
		if len(data) < offset+width {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated int128/uint128/coins")
		}
		v := decodeIntBytes(data[offset:offset+width], IsSigned(tag))
		offset += width
		return RuntimeValue{Tag: tag, intVal: v}, offset, nil
	case TagInt256, TagUint256:
		width := 32
		if len(data) < offset+width {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated int256/uint256")
		}
		v := decodeIntBytes(data[offset:offset+width], IsSigned(tag))
		offset += width
		return RuntimeValue{Tag: tag, intVal: v}, offset, nil
	case TagAddress:
		length, n, err := decodeLengthPrefix(data[offset:])
		if err != nil {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: address length: %w", err)
		}
		offset += n
		if uint32(len(data)-offset) < length {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated address")
		}
		addr := string(data[offset : offset+int(length)])
		offset += int(length)
		return ValueAddress(addr), offset, nil
	case TagHash:
		width := 32
		if len(data) < offset+width {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated hash")
		}
		var h [32]byte
		copy(h[:], data[offset:offset+width])
		offset += width
		return ValueHash(h), offset, nil
	case TagBytes:
		length, n, err := decodeLengthPrefix(data[offset:])
		if err != nil {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: bytes length: %w", err)
		}
		offset += n
		if uint32(len(data)-offset) < length {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated bytes")
		}
		b := make([]byte, length)
		copy(b, data[offset:offset+int(length)])
		offset += int(length)
		return ValueBytes(b), offset, nil
	case TagString:
		length, n, err := decodeLengthPrefix(data[offset:])
		if err != nil {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: string length: %w", err)
		}
		offset += n
		if uint32(len(data)-offset) < length {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated string")
		}
		s := string(data[offset : offset+int(length)])
		if !utf8.ValidString(s) {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: invalid UTF-8 in canonical string")
		}
		offset += int(length)
		return ValueString(s), offset, nil
	case TagTuple:
		count, n, err := decodeLengthPrefix(data[offset:])
		if err != nil {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: tuple count: %w", err)
		}
		offset += n
		// Bound the declared count before allocating: reject values that
		// exceed the tuple cap or that cannot possibly fit in the remaining
		// input (every element needs at least one tag byte). Without this a
		// crafted 5-byte value could force a multi-terabyte make().
		if count > MaxTupleElements {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: tuple element count %d exceeds limit %d", count, MaxTupleElements)
		}
		if uint64(count) > uint64(len(data)-offset) {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: tuple count %d exceeds remaining input", count)
		}
		elements := make([]RuntimeValue, count)
		for i := uint32(0); i < count; i++ {
			elem, consumed, err := canonicalDecodeAt(data[offset:], depth+1)
			if err != nil {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: tuple element %d: %w", i, err)
			}
			elements[i] = elem
			offset += consumed
		}
		return ValueTuple(elements), offset, nil
	case TagMap:
		count, n, err := decodeLengthPrefix(data[offset:])
		if err != nil {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: map count: %w", err)
		}
		offset += n
		// Bound the declared entry count before pre-sizing the backing slice.
		// Each entry needs at least a 4-byte key length + 4-byte value length,
		// so count cannot exceed remaining/8; also cap at the tuple limit.
		if count > MaxTupleElements {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: map entry count %d exceeds limit %d", count, MaxTupleElements)
		}
		if uint64(count) > uint64(len(data)-offset)/8 {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: map count %d exceeds remaining input", count)
		}
		entries := make([]runtimeMapEntry, 0, count)
		var previous []byte
		for i := uint32(0); i < count; i++ {
			keyLen, n, err := decodeLengthPrefix(data[offset:])
			if err != nil {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: map key length: %w", err)
			}
			offset += n
			if uint32(len(data)-offset) < keyLen {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated map key")
			}
			keyValue, keyConsumed, err := canonicalDecodeAt(data[offset:offset+int(keyLen)], depth+1)
			if err != nil {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: map key %d: %w", i, err)
			}
			if keyConsumed != int(keyLen) {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: map key %d has trailing bytes", i)
			}
			offset += int(keyLen)
			valueLen, n, err := decodeLengthPrefix(data[offset:])
			if err != nil {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: map value length: %w", err)
			}
			offset += n
			if uint32(len(data)-offset) < valueLen {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated map value")
			}
			valueValue, valueConsumed, err := canonicalDecodeAt(data[offset:offset+int(valueLen)], depth+1)
			if err != nil {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: map value %d: %w", i, err)
			}
			if valueConsumed != int(valueLen) {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: map value %d has trailing bytes", i)
			}
			offset += int(valueLen)
			keyBytes, err := CanonicalEncode(keyValue)
			if err != nil {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: map key %d canonicalization: %w", i, err)
			}
			if len(previous) > 0 && bytes.Compare(previous, keyBytes) >= 0 {
				return RuntimeValue{}, 0, fmt.Errorf("AVM: map entries must be strictly sorted by canonical key")
			}
			previous = append(previous[:0], keyBytes...)
			entries = append(entries, runtimeMapEntry{
				Key:      keyValue,
				Value:    valueValue,
				keyBytes: keyBytes,
			})
		}
		return RuntimeValue{Tag: TagMap, mapVal: entries}, offset, nil
	case TagChunkRef:
		if len(data) < offset+1 {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated chunk ref")
		}
		present := data[offset]
		offset++
		if present == 0x00 {
			return RuntimeValue{Tag: TagChunkRef, chunkRef: nil}, offset, nil
		}
		if present != 0x01 {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: invalid chunk ref presence marker 0x%02x", present)
		}
		if len(data) < offset+4 {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated chunk ref length")
		}
		treeLen := binary.BigEndian.Uint32(data[offset:])
		offset += 4
		if treeLen > MaxChunkTreeBytes {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: chunk ref tree length %d exceeds limit %d", treeLen, MaxChunkTreeBytes)
		}
		if uint32(len(data)-offset) < treeLen {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated chunk ref tree")
		}
		tree, consumed, err := chunk.ParseChunkTree(data[offset : offset+int(treeLen)])
		if err != nil {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: chunk ref: %w", err)
		}
		if consumed != int(treeLen) {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: chunk ref tree has trailing bytes")
		}
		offset += int(treeLen)
		return RuntimeValue{Tag: TagChunkRef, chunkRef: tree}, offset, nil
	case TagReaderCursor:
		width := 32 + 4
		if len(data) < offset+width {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated reader cursor")
		}
		off := binary.BigEndian.Uint32(data[offset+32:])
		return RuntimeValue{Tag: TagReaderCursor, chunkRef: nil, readerOff: off}, offset + width, nil
	case TagWriterHandle:
		if len(data) < offset+1 {
			return RuntimeValue{}, 0, fmt.Errorf("AVM: truncated writer handle")
		}

		offset++
		return RuntimeValue{Tag: TagWriterHandle, writerPtr: nil}, offset, nil
	case TagExecFrameRef:
		return RuntimeValue{Tag: TagExecFrameRef}, 1, nil
	default:
		return RuntimeValue{}, 0, fmt.Errorf("AVM: unknown tag %d in canonical decode", tag)
	}
}

// CanonicalDecodeExact decodes a RuntimeValue and rejects trailing bytes.
func CanonicalDecodeExact(data []byte) (RuntimeValue, error) {
	value, consumed, err := CanonicalDecode(data)
	if err != nil {
		return RuntimeValue{}, err
	}
	if consumed != len(data) {
		return RuntimeValue{}, fmt.Errorf("AVM: trailing bytes after canonical decode")
	}
	return value, nil
}

type CastExitCode uint8

const (
	CastOK           CastExitCode = 0
	CastTypeMismatch CastExitCode = 1
	CastOverflow     CastExitCode = 2
	CastTruncation   CastExitCode = 3
	CastInvalidUTF8  CastExitCode = 4
	CastNullToValue  CastExitCode = 5
)

// ExplicitCast performs a type cast between integer widths.
// All casts MUST be explicit. Invalid cast → deterministic exit code.
func ExplicitCast(v RuntimeValue, targetTag ValueTag) (RuntimeValue, CastExitCode) {
	if v.Tag == targetTag {
		return v, CastOK
	}

	if v.Tag == TagNull {
		return RuntimeValue{}, CastNullToValue
	}

	if IsInteger(v.Tag) && IsInteger(targetTag) {
		srcWidth, _ := ValueBitWidth(v.Tag)
		dstWidth, _ := ValueBitWidth(targetTag)

		if dstWidth < srcWidth {
			if !IsSigned(targetTag) && IsSigned(v.Tag) {
				return RuntimeValue{}, CastOverflow
			}
		}

		return RuntimeValue{Tag: targetTag, intVal: new(big.Int).Set(v.intVal)}, CastOK
	}

	if v.Tag == TagCoins && IsInteger(targetTag) {
		return RuntimeValue{Tag: targetTag, intVal: new(big.Int).Set(v.intVal)}, CastOK
	}

	if v.Tag == TagTimestamp && targetTag == TagUint64 {
		return RuntimeValue{Tag: targetTag, intVal: new(big.Int).Set(v.intVal)}, CastOK
	}

	return RuntimeValue{}, CastTypeMismatch
}

var maxEncodedSize = map[ValueTag]uint32{
	TagNull:         1,
	TagBool:         2,
	TagInt8:         2,
	TagInt16:        3,
	TagInt32:        5,
	TagInt64:        9,
	TagInt128:       17,
	TagInt256:       33,
	TagUint8:        2,
	TagUint16:       3,
	TagUint32:       5,
	TagUint64:       9,
	TagUint128:      17,
	TagUint256:      33,
	TagCoins:        17,
	TagTimestamp:    9,
	TagAddress:      4 + MaxAddressLength,
	TagHash:         33,
	TagBytes:        4 + MaxBytesLength,
	TagString:       4 + MaxStringLength,
	TagTuple:        5 + MaxTupleElements*33,
	TagChunkRef:     1 + 4 + MaxChunkTreeBytes,
	TagReaderCursor: 37,
	TagWriterHandle: 34,
	TagExecFrameRef: 2,
	TagMap:          4 + MaxTupleElements*(4+MaxBytesLength+4+MaxBytesLength),
}

var gasCostEncode = map[ValueTag]uint64{
	TagNull:         1,
	TagBool:         1,
	TagInt8:         2,
	TagInt16:        2,
	TagInt32:        3,
	TagInt64:        3,
	TagInt128:       5,
	TagInt256:       8,
	TagUint8:        2,
	TagUint16:       2,
	TagUint32:       3,
	TagUint64:       3,
	TagUint128:      5,
	TagUint256:      8,
	TagCoins:        5,
	TagTimestamp:    3,
	TagAddress:      10,
	TagHash:         10,
	TagBytes:        5 + 1,
	TagString:       5 + 1,
	TagTuple:        10,
	TagChunkRef:     15,
	TagReaderCursor: 20,
	TagWriterHandle: 20,
	TagExecFrameRef: 5,
	TagMap:          20,
}

var gasCostDecode = map[ValueTag]uint64{
	TagNull:         1,
	TagBool:         1,
	TagInt8:         2,
	TagInt16:        2,
	TagInt32:        3,
	TagInt64:        3,
	TagInt128:       5,
	TagInt256:       8,
	TagUint8:        2,
	TagUint16:       2,
	TagUint32:       3,
	TagUint64:       3,
	TagUint128:      5,
	TagUint256:      8,
	TagCoins:        5,
	TagTimestamp:    3,
	TagAddress:      10,
	TagHash:         10,
	TagBytes:        5 + 1,
	TagString:       5 + 1,
	TagTuple:        10,
	TagChunkRef:     15,
	TagReaderCursor: 20,
	TagWriterHandle: 20,
	TagExecFrameRef: 5,
	TagMap:          20,
}

// Size bounds for variable-length types
const (
	MaxAddressLength uint32 = 128
	MaxBytesLength   uint32 = 65536
	MaxStringLength  uint32 = 65536
	MaxTupleElements uint32 = 256
	// MaxChunkTreeBytes bounds the fully self-contained (hash-free) encoding
	// of a Chunk/Code value, sized generously above the compiler's default
	// 64 KiB code limit to cover recursive tree header overhead.
	MaxChunkTreeBytes uint32 = 128 * 1024
	// MaxCanonicalDecodeDepth bounds the recursive nesting of tuple/map values
	// CanonicalDecode will descend into. Breadth and byte-length limits alone
	// do not stop a compact, deeply nested value (each level costs only a
	// handful of bytes) from driving unbounded recursion — every level adds a
	// Go stack frame plus recursive-call CPU, so a value can exhaust stack or
	// CPU well before it would ever hit MaxTupleElements or MaxBytesLength.
	// Aligned with chunk.MaxChunkTreeDepth, the analogous bound for chunk
	// trees. Reject over-depth values before recursing further, at the cheap
	// end of the cost curve.
	MaxCanonicalDecodeDepth = 256
)

// MaxEncodedSize returns the maximum encoded size for a value tag.
func MaxEncodedSizeForTag(tag ValueTag) uint32 {
	if s, ok := maxEncodedSize[tag]; ok {
		return s
	}
	return 0
}

// GasCostEncode returns the gas cost to encode a value of the given tag.
func GasCostEncode(tag ValueTag) uint64 {
	if c, ok := gasCostEncode[tag]; ok {
		return c
	}
	return 100
}

// GasCostDecode returns the gas cost to decode a value of the given tag.
func GasCostDecode(tag ValueTag) uint64 {
	if c, ok := gasCostDecode[tag]; ok {
		return c
	}
	return 100
}

// CanonicalHash returns the BLAKE3 hash of the canonical encoding of a value.
func CanonicalHash(v RuntimeValue) ([32]byte, error) {
	encoded, err := CanonicalEncode(v)
	if err != nil {
		return [32]byte{}, fmt.Errorf("AVM: canonical hash: %w", err)
	}
	return blake3.Sum256(encoded), nil
}

// OptionNone returns the null value representing Option.None.
func OptionNone() RuntimeValue {
	return ValueNull()
}

// OptionSome wraps a value representing Option.Some.
func OptionSome(v RuntimeValue) RuntimeValue {
	return v
}

// IsOptionNone checks if a value is Option.None (null).
func IsOptionNone(v RuntimeValue) bool {
	return v.Tag == TagNull
}

type ValueWriter struct {
	builder *chunk.Builder
	data    []byte
	refs    []*chunk.Chunk
}

func NewValueWriter() *ValueWriter {
	return &ValueWriter{
		builder: chunk.NewBuilder(),
	}
}

func (w *ValueWriter) Builder() *chunk.Builder {
	return w.builder
}

func (w *ValueWriter) WriteValue(v RuntimeValue) error {
	encoded, err := CanonicalEncode(v)
	if err != nil {
		return err
	}
	w.data = append(w.data, encoded...)
	return nil
}

func (w *ValueWriter) Build() (*chunk.Chunk, error) {
	// Values whose serialized data exceeds one chunk (MaxDataBits) spill into a
	// canonical 8-way chunk tree instead of failing, matching ToChunkPayload.
	// A leaf value (<= MaxDataBits/8 bytes) still builds a single chunk with the
	// exact same layout and hash as before, so this is additive: it only makes
	// previously-unstorable large values representable.
	if len(w.data) > chunk.MaxDataBits/8 {
		if len(w.refs) > 0 {
			return nil, fmt.Errorf("AVM value: %d bytes overflow a chunk and cannot be combined with %d explicit refs; nest the large field in its own Chunk<T>", len(w.data), len(w.refs))
		}
		return ToChunkPayload(w.data, chunk.TypeNormal)
	}
	w.builder.SetData(w.data, uint16(len(w.data)*8))
	w.builder.SetTypeTag(chunk.TypeNormal)
	for _, ref := range w.refs {
		w.builder.AddRef(ref)
	}
	return w.builder.Build()
}

func (w *ValueWriter) AddRef(c *chunk.Chunk) {
	w.refs = append(w.refs, c)
}

func ValidateDeterminism(a, b RuntimeValue) error {
	encA, err := CanonicalEncode(a)
	if err != nil {
		return fmt.Errorf("AVM: determinism check A: %w", err)
	}
	encB, err := CanonicalEncode(b)
	if err != nil {
		return fmt.Errorf("AVM: determinism check B: %w", err)
	}
	if a.Tag != b.Tag {
		return fmt.Errorf("AVM: determinism violation: different tags %v vs %v", a.Tag, b.Tag)
	}
	if len(encA) != len(encB) {
		return fmt.Errorf("AVM: determinism violation: different encoded lengths %d vs %d", len(encA), len(encB))
	}
	for i := range encA {
		if encA[i] != encB[i] {
			return fmt.Errorf("AVM: determinism violation: byte %d differs (0x%02x vs 0x%02x)", i, encA[i], encB[i])
		}
	}
	return nil
}

func typeError(expected, got ValueTag) error {
	return fmt.Errorf("AVM type error: expected %s, got %s → EXIT_TYPE_ERROR", expected, got)
}

func encodeIntBytes(v *big.Int, width int, signed bool) ([]byte, error) {
	if v == nil {
		return make([]byte, width), nil
	}
	if width <= 0 {
		return nil, fmt.Errorf("AVM: invalid integer width %d", width)
	}
	bitWidth := uint(width * 8)
	limit := new(big.Int).Lsh(big.NewInt(1), bitWidth)
	if signed {
		half := new(big.Int).Rsh(new(big.Int).Set(limit), 1)
		min := new(big.Int).Neg(half)
		max := new(big.Int).Sub(new(big.Int).Set(half), big.NewInt(1))
		if v.Cmp(min) < 0 || v.Cmp(max) > 0 {
			return nil, fmt.Errorf("AVM: signed integer %s out of range for %d-bit encoding", v.String(), bitWidth)
		}
		encoded := new(big.Int).Set(v)
		if encoded.Sign() < 0 {
			encoded.Add(encoded, limit)
		}
		b := make([]byte, width)
		encoded.FillBytes(b)
		return b, nil
	}
	if v.Sign() < 0 || v.BitLen() > int(bitWidth) {
		return nil, fmt.Errorf("AVM: unsigned integer %s out of range for %d-bit encoding", v.String(), bitWidth)
	}
	b := make([]byte, width)
	v.FillBytes(b)
	return b, nil
}

func decodeIntBytes(data []byte, signed bool) *big.Int {
	v := new(big.Int).SetBytes(data)
	if signed && len(data) > 0 && data[0]&0x80 != 0 {
		limit := new(big.Int).Lsh(big.NewInt(1), uint(len(data)*8))
		v.Sub(v, limit)
	}
	return v
}

func encodeLengthPrefix(length uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, length)
	return b
}

func decodeLengthPrefix(data []byte) (uint32, int, error) {
	if len(data) < 4 {
		return 0, 0, fmt.Errorf("AVM: truncated length prefix")
	}
	length := binary.BigEndian.Uint32(data[:4])
	return length, 4, nil
}
