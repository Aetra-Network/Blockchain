package stdlib

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
)

func TestAddressMessageAndStateInitBuilders(t *testing.T) {
	addr, err := AddressFromBytes(bytes.Repeat([]byte{0x42}, 20))
	require.NoError(t, err)
	pair, err := addr.MessagePair()
	require.NoError(t, err)
	require.NotEmpty(t, pair.User)
	require.NotEmpty(t, pair.Raw)

	msg, err := NewMessageBuilder().
		External().
		Opcode(11).
		QueryID(7).
		Sender(addr).
		Destination(addr).
		ValueNAET(1).
		GasLimit(1000).
		Body([]byte("payload")).
		Build()
	require.NoError(t, err)
	require.Equal(t, uint64(11), msg.Opcode)
	require.Equal(t, pair.Raw, msg.Sender.Raw)

	chunkValue, err := NewChunk([]byte("hello"), chunk.TypeSystem)
	require.NoError(t, err)
	flattened, err := chunkValue.Bytes()
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), flattened)

	var codeHash [32]byte
	copy(codeHash[:], bytes.Repeat([]byte{0x11}, 32))
	stateInit, hash, err := NewStateInitBuilder().
		CodeHash(codeHash).
		Deployer(addr).
		ChainID("avm-local").
		Namespace("demo").
		InitialStateRoot(&chunkValue).
		Build()
	require.NoError(t, err)
	require.NotNil(t, stateInit)
	require.NotEqual(t, [32]byte{}, hash)
	require.Equal(t, addressing.FormatAccAddress(sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))), stateInit.DeployerAddress)
}

func TestSafeMathAndCollections(t *testing.T) {
	sum, ok := SafeAddUint64(1, 2)
	require.True(t, ok)
	require.Equal(t, uint64(3), sum)
	_, ok = SafeAddUint64(^uint64(0), 1)
	require.False(t, ok)

	coins := NewCoins(10)
	other := NewCoins(15)
	combined, err := coins.Add(other)
	require.NoError(t, err)
	require.Equal(t, uint64(25), combined.Amount)

	opt := Some("value")
	require.Equal(t, "value", opt.OrElse("fallback"))
	require.Equal(t, "fallback", None[string]().OrElse("fallback"))

	items := map[string]int{"b": 2, "a": 1}
	var order []string
	require.NoError(t, IterateMapStrings(items, 4, func(key string, value int) error {
		order = append(order, key)
		return nil
	}))
	require.Equal(t, []string{"a", "b"}, order)
}

func TestChunkAndSegmentHashes(t *testing.T) {
	chunkValue, err := NewChunk([]byte("payload"), chunk.TypeSystem)
	require.NoError(t, err)

	require.NotEqual(t, [32]byte{}, chunkValue.Hash())
	require.NotEqual(t, [32]byte{}, chunkValue.BitsHash())

	segment := chunkValue.Segment()
	require.Equal(t, chunkValue.Hash(), segment.Hash())
	require.Equal(t, chunkValue.BitsHash(), segment.BitsHash())
	require.False(t, segment.IsZero())
}
