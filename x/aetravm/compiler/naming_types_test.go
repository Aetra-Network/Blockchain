package compiler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBindingNamesRejectKeywords(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "keyword as local const",
			src:  "func demo() {\n  const if = 1\n}\n",
			want: "cannot use keyword \"if\" as a binding name",
		},
		{
			name: "keyword as local var",
			src:  "func demo() {\n  var send = 2\n}\n",
			want: "cannot use keyword \"send\" as a binding name",
		},
		{
			name: "keyword as top-level const",
			src:  "const match = 1\n",
			want: "cannot use keyword \"match\" as a binding name",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSourceNamed("naming.atlx", tt.src)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestIdentifiersAreASCIIOnly(t *testing.T) {
	// Unicode letters are not identifier characters.
	_, err := ParseSourceNamed("naming.atlx", "const имя = 1\n")
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected character")

	// A dash is an operator, never part of an identifier, so `a-b` cannot be
	// a binding name.
	_, err = ParseSourceNamed("naming.atlx", "const a-b = 1\n")
	require.Error(t, err)

	// Leading digits lex as numbers, not identifiers.
	_, err = ParseSourceNamed("naming.atlx", "const 1x = 1\n")
	require.Error(t, err)

	// Underscore-led and digit-containing names are valid.
	_, err = ParseSourceNamed("naming.atlx", "const _ok_2 = 1\n")
	require.NoError(t, err)
}

func TestSmallIntegerTypesCompileAndRangeCheck(t *testing.T) {
	src := `
@storage
struct Flags {
    mode: uint2
    level: uint4
    delta: int2
    step: int4
    wide: uint32
}

@message(0x1201)
struct SetFlags {
    mode: uint2
    level: uint4
}

type FlagsMsg = SetFlags

contract FlagBox {
    storage: Flags
    incomingMessages: FlagsMsg

    @store
    func Flags.load() {
        return Flags.fromChunk(contract.getData())
    }

    @store
    func Flags.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy FlagsMsg.fromSegment(in.body)

        match (msg) {
            SetFlags => {
                var st = lazy Flags.load()
                st.mode = msg.mode
                st.level = msg.level
                st.save()
            }
            else => {
                assert (in.body.isEmpty()) throw 0xFFFF
            }
        }
    }

    @get
    func mode(): uint2 {
        const st = lazy Flags.load()
        return st.mode
    }
}
`
	c, err := New(Options{})
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)

	// In-range values encode fine.
	_, err = res.MessageBodies["SetFlags"].Encode(map[string]any{
		"mode":  uint64(3),
		"level": uint64(15),
	})
	require.NoError(t, err)

	// Out-of-range values must be rejected by the codec.
	_, err = res.MessageBodies["SetFlags"].Encode(map[string]any{
		"mode":  uint64(4),
		"level": uint64(1),
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "out of range"), "got: %v", err)
}

func TestWideIntegerRangeCheck(t *testing.T) {
	src := `
@storage
struct TinyState {
    seen: uint8
}

@message(0x1301)
struct Tight {
    small: uint8
}

type TightMsg = Tight

contract Tiny {
    storage: TinyState
    incomingMessages: TightMsg

    @store
    func TinyState.load() {
        return TinyState.fromChunk(contract.getData())
    }

    @store
    func TinyState.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy TightMsg.fromSegment(in.body)

        match (msg) {
            Tight => {
                var st = lazy TinyState.load()
                st.seen = msg.small
                st.save()
            }
            else => {
                assert (in.body.isEmpty()) throw 0xFFFF
            }
        }
    }
}
`
	c, err := New(Options{})
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)

	_, err = res.MessageBodies["Tight"].Encode(map[string]any{"small": uint64(255)})
	require.NoError(t, err)

	_, err = res.MessageBodies["Tight"].Encode(map[string]any{"small": uint64(256)})
	require.Error(t, err)
}

func TestCanonicalWideIntegerTypesCompile(t *testing.T) {
	src := `
@storage
struct WideNumbers {
    u128: uint128 = 0
    u256: uint256 = 0
    i128: int128 = 0
    i256: int256 = 0
}

@message(0x1401)
struct Touch {}

type WideMsg = Touch

contract WideBox {
    storage: WideNumbers
    incomingMessages: WideMsg

    @store
    func WideNumbers.load() {
        return WideNumbers.fromChunk(contract.getData())
    }

    @store
    func WideNumbers.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy WideMsg.fromSegment(in.body)

        match (msg) {
            Touch => {}
            else => {
                assert (in.body.isEmpty()) throw 0xFFFF
            }
        }
    }
}
`
	c, err := New(Options{})
	require.NoError(t, err)
	_, err = c.Compile([]byte(src))
	require.NoError(t, err)
}

func TestBareIntegerTypesCompileAsUint256AndInt256(t *testing.T) {
	src := `
@storage
struct BareNumbers {
    amount: uint = 0
    delta: int = 0
}

@message(0x1501)
struct Touch {}

type BareMsg = Touch

contract BareBox {
    storage: BareNumbers
    incomingMessages: BareMsg

    @store
    func BareNumbers.load() {
        return BareNumbers.fromChunk(contract.getData())
    }

    @store
    func BareNumbers.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy BareMsg.fromSegment(in.body)

        match (msg) {
            Touch => {}
            else => {
                assert (in.body.isEmpty()) throw 0xFFFF
            }
        }
    }

    @get
    func totals(): uint {
        const st = lazy BareNumbers.load()
        return st.amount
    }
}
`
	c, err := New(Options{})
	require.NoError(t, err)
	res, err := c.Compile([]byte(src))
	require.NoError(t, err)
	require.Equal(t, "uint256", res.Source.Structs[0].Fields[0].Type.String())
	require.Equal(t, "int256", res.Source.Structs[0].Fields[1].Type.String())
	require.Contains(t, FormatSource(res.Source), "func totals() -> uint256")
}
