package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// nestedStructMapSource is the canonical proof that a message body can carry a
// Map whose values are STRUCTS with nested Chunk<struct> fields, and that every
// level round-trips through field access at runtime. Before the self-describing
// struct-encoding work, a struct-typed message field (or map value) decoded to
// opaque bytes and its fields read back empty; a struct value now encodes as a
// [{name,type,value}] list — the same shape a top-level body uses — which the
// runtime's runtimeMessageFieldValue decoder navigates with no decode-side
// change. Chunk<T> is transparent (toChunk/fromChunk are compile-time
// reinterprets), so Chunk<Init> and Chunk<Meta> carry their structs directly.
const nestedStructMapSource = `
struct Meta { kind: uint2  content: string }
struct Init { ownerAddress: address  content: Chunk<Meta> }
struct BatchItem { attachAetAmount: coins  initParams: Chunk<Init> }

@storage struct S {
  seenOwner: address
  seenContent: string
  seenKind: uint2
  seenAmount: coins
}

@message(0x01) struct Batch { items: Map<uint64, BatchItem> }
type IM = Batch

contract C {
  storage: S
  incomingExternal: IM
  @store func S.load() { return S.fromChunk(contract.getData()) }
  @store func S.save(self) { contract.setData(self.toChunk()) }
  @external func onExternalMessage(inMsg: Segment) {
    const msg = lazy IM.fromSegment(inMsg)
    match (msg) {
      Batch => {
        const items = msg.items
        const found = items.get(7)
        assert (found != null) throw 11
        const item = found!
        const init = Init.fromChunk(item.initParams)
        const meta = Meta.fromChunk(init.content)
        var st = lazy S.load()
        st.seenOwner = init.ownerAddress
        st.seenContent = meta.content
        st.seenKind = meta.kind
        st.seenAmount = item.attachAetAmount
        st.save()
      }
      else => { assert (inMsg.isEmpty()) throw 0xFFFF }
    }
  }
  @bounced func onBouncedMessage(in: InMessageBounced) {}
  @get func seenOwner(): address { const st = lazy S.load() return st.seenOwner }
  @get func seenContent(): string { const st = lazy S.load() return st.seenContent }
  @get func seenKind(): uint2 { const st = lazy S.load() return st.seenKind }
  @get func seenAmount(): coins { const st = lazy S.load() return st.seenAmount }
}
`

func TestNestedStructAndChunkMessageBodyRoundTrips(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)
	res, err := c.Compile([]byte(nestedStructMapSource))
	require.NoError(t, err)

	fields := map[string]any{
		"items": map[uint64]any{
			7: map[string]any{
				"attachAetAmount": uint64(123456),
				"initParams": map[string]any{
					"ownerAddress": "ae1owner",
					"content":      map[string]any{"kind": uint64(1), "content": "ipfs://x.json"},
				},
			},
		},
	}

	body, err := res.MessageBodies["Batch"].Encode(fields)
	require.NoError(t, err)

	// Encoding must be deterministic: the same nested input always produces
	// byte-identical output (struct fields walked in declared order, never Go
	// map order). Two independent encodes must match.
	body2, err := res.MessageBodies["Batch"].Encode(fields)
	require.NoError(t, err)
	require.Equal(t, body, body2, "nested struct encoding must be deterministic")

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryReceiveExternal,
		Message:  async.MessageEnvelope{Opcode: res.MessageBodyOpcodes["Batch"], QueryID: 1, Body: body, GasLimit: 5_000_000},
		GasLimit: 5_000_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)

	run := func(name string) avm.RuntimeValue {
		g, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: getterSelector(t, res, name), QueryID: 2, GasLimit: 2_000_000},
			GasLimit: 2_000_000,
		})
		require.NoError(t, err)
		require.Equalf(t, async.ResultOK, g.ResultCode, "getter %s trapped", name)
		return g.ReturnValue
	}

	owner, err := run("seenOwner").AsAddress()
	require.NoError(t, err)
	require.Equal(t, "ae1owner", owner)

	content, err := run("seenContent").AsString()
	require.NoError(t, err)
	require.Equal(t, "ipfs://x.json", content, "the innermost Chunk<Meta>.content must survive two chunk layers and a map")

	kind, err := run("seenKind").AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(1), kind)

	amount, err := run("seenAmount").AsBigInt()
	require.NoError(t, err)
	require.Equal(t, "123456", amount.String(), "the scalar sibling of a chunk field must decode independently")
}
