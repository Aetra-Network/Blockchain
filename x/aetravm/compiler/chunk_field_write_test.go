package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// chunkFieldWriteSource seals a struct into a typed Chunk<T> storage field
// (`st.meta = built.toChunk()`) and reads it back (`Meta.fromChunk(st.meta)`).
const chunkFieldWriteSource = `
struct Meta { kind: uint2  uri: string }

@storage struct S { meta: Chunk<Meta> }

@message(0x01) struct Set { kind: uint2  uri: string }
type IM = Set

contract C {
  storage: S
  incomingExternal: IM
  @store func S.load() { return S.fromChunk(contract.getData()) }
  @store func S.save(self) { contract.setData(self.toChunk()) }
  @external func onExternalMessage(inMsg: Segment) {
    const msg = lazy IM.fromSegment(inMsg)
    match (msg) {
      Set => {
        var st = lazy S.load()
        const built = Meta { kind: msg.kind, uri: msg.uri }
        st.meta = built.toChunk()
        st.save()
      }
      else => { assert (inMsg.isEmpty()) throw 0xFFFF }
    }
  }
  @bounced func onBouncedMessage(in: InMessageBounced) {}
  @get func uri(): string { const st = lazy S.load()  const m = Meta.fromChunk(st.meta)  return m.uri }
}
`

// TestChunkTypedFieldWriteRoundTrips is the regression test for the
// compatibleTypes fix that lets a bare chunk value (the type `value.toChunk()`
// infers to) be assigned into a parameterized `Chunk<T>` storage field. Before
// the fix, the equal-name arity check in compatibleTypes returned false for
// (Chunk, Chunk<Meta>) before the isChunkLikeType fallback could allow it, so
// a `Chunk<T>` field could be declared and read but never written — the write
// failed to compile with "cannot assign Chunk to Chunk<Meta>".
func TestChunkTypedFieldWriteRoundTrips(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(chunkFieldWriteSource))
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	body, err := res.MessageBodies["Set"].Encode(map[string]any{"kind": uint64(1), "uri": "ipfs://doc.json"})
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: res.MessageBodyOpcodes["Set"], QueryID: 1, Body: body, GasLimit: 2_000_000},
		GasLimit: 2_000_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)

	get, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, "uri"), QueryID: 2, GasLimit: 2_000_000},
		GasLimit: 2_000_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, get.ResultCode)
	s, err := get.ReturnValue.AsString()
	require.NoError(t, err)
	require.Equal(t, "ipfs://doc.json", s)
}
