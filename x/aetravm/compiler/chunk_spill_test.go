package compiler

import "testing"

// TestLargeStorageStructCompilesWithoutChunkLimit proves the chunk primitive
// limits (MaxDataBits=2048, MaxRefs=8) do NOT cap ATLX storage structs: a
// struct may hold more than 8 Chunk<T> reference fields and far more than 2048
// bits of scalar data. Structs serialize to bounded state bytes and the chunk
// representation auto-nests into a tree where one is needed, so the developer
// never has to hand-shard a struct.
func TestLargeStorageStructCompilesWithoutChunkLimit(t *testing.T) {
	src := `
@storage
struct Leaf { v: uint64 }

@storage
struct WideStorage {
  a: Chunk<Leaf>?
  b: Chunk<Leaf>?
  c: Chunk<Leaf>?
  d: Chunk<Leaf>?
  e: Chunk<Leaf>?
  f: Chunk<Leaf>?
  g: Chunk<Leaf>?
  h: Chunk<Leaf>?
  i: Chunk<Leaf>?
  j: Chunk<Leaf>?
  k: Chunk<Leaf>?
  l: Chunk<Leaf>?
  n0: uint256
  n1: uint256
  n2: uint256
  n3: uint256
  n4: uint256
  n5: uint256
  n6: uint256
  n7: uint256
  owner: address
}

@message(0x1001)
struct Ping { x: uint64 }

type InternalMsg = Ping

contract WideContract {
  storage: WideStorage
  incomingMessages: InternalMsg

  @internal
  func onInternalMessage(in: InMessage) {
  }
}
`
	c, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}
	if _, err := c.Compile([]byte(src)); err != nil {
		t.Fatalf("storage struct with 12 Chunk<T> fields and 8x uint256 must compile, got: %v", err)
	}
}
