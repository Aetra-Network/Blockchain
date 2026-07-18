package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// cryptoBuiltinSource exercises every new byte-exact hash / byte builtin in a
// getter that returns the builtin's exact result type, so each one must lower to
// its dedicated opcode. INPUT is a compile-time byte constant (bytes.fromHex).
const cryptoBuiltinSource = `
const INPUT = bytes.fromHex("00112233445566778899aabbccddeeff")

@storage
struct CryptoState {
  value: u64 = 0
}

@message(11)
struct Poke {}

type CryptoMsg = Poke

contract CryptoDemo {
  storage: CryptoState
  incomingMessages: CryptoMsg
  namespace "cryptodemo"
  chain "avm-local"

  @store
  func CryptoState.load() {
    return CryptoState.fromChunk(contract.getData())
  }

  @store
  func CryptoState.save(self) {
    contract.setData(self.toChunk())
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy CryptoMsg.fromSegment(in.body)
    match (msg) {
      Poke => {
        var st = lazy CryptoState.load()
        st.value += 1
        st.save()
      }
      else => {
        assert (in.body.isEmpty()) throw 0xFFFF
      }
    }
  }

  @bounced
  func onBouncedMessage(in: InMessageBounced) {
  }

  @get func gSha256(): hash32 { return sha256(INPUT) }
  @get func gKeccak256(): hash32 { return keccak256(INPUT) }
  @get func gBlake2b(): hash32 { return blake2b(INPUT) }
  @get func gRipemd160(): bytes { return ripemd160(INPUT) }
  @get func gSha512(): bytes { return sha512(INPUT) }
  @get func gConcat(): bytes { return concat(INPUT, INPUT) }
  @get func gSlice(): bytes { return subBytes(INPUT, 1, 2) }
  @get func gByteAt(): uint8 { return byteAt(INPUT, 1) }
  @get func gToBytesBE(): bytes { return toBytesBE(258, 4) }
  @get func gFromBytesBE(): uint256 { return fromBytesBE(subBytes(INPUT, 0, 8)) }
}
`

// TestCryptoBuiltinsLowerToOpcodes proves every new source builtin lowers to its
// dedicated VM opcode (the compiler and interpreter halves stay in sync) and
// that the resulting module still verifies with those opcodes present.
func TestCryptoBuiltinsLowerToOpcodes(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(cryptoBuiltinSource))
	require.NoError(t, err)
	require.NotNil(t, res)

	for _, op := range []avm.Opcode{
		avm.OpSha256,
		avm.OpKeccak256,
		avm.OpBlake2b,
		avm.OpRipemd160,
		avm.OpSha512,
		avm.OpConcat,
		avm.OpSlice,
		avm.OpByteAt,
		avm.OpToBytesBE,
		avm.OpFromBytesBE,
	} {
		require.Truef(t, hasOpcode(res.Module.Code, op),
			"builtin must lower to opcode 0x%02x", byte(op))
	}

	// The module (carrying the new opcodes) must verify.
	verifier, err := avm.NewVerifier(avm.DefaultParams())
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(res.Module))
}

// TestCryptoBuiltinsModuleRoundTrips confirms the generic instruction wire
// format encodes/decodes the new opcodes losslessly (they need no serializer
// change) and the decoded module still verifies.
func TestCryptoBuiltinsModuleRoundTrips(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(cryptoBuiltinSource))
	require.NoError(t, err)

	encoded, err := avm.EncodeModule(res.Module)
	require.NoError(t, err)

	decoded, err := avm.DecodeModule(encoded)
	require.NoError(t, err)

	// Each new opcode must survive the decode...
	for _, op := range []avm.Opcode{
		avm.OpSha256, avm.OpKeccak256, avm.OpBlake2b, avm.OpRipemd160, avm.OpSha512,
		avm.OpConcat, avm.OpSlice, avm.OpByteAt, avm.OpToBytesBE, avm.OpFromBytesBE,
	} {
		require.Truef(t, hasOpcode(decoded.Code, op), "opcode 0x%02x must survive decode", byte(op))
	}

	// ...and the wire format must be stable (re-encoding the decoded module
	// yields identical bytes). A struct compare of the decoded Code would trip on
	// the pre-existing nil-vs-empty Data normalization of empty-key storage ops,
	// so compare the canonical encodings instead.
	reencoded, err := avm.EncodeModule(decoded)
	require.NoError(t, err)
	require.Equal(t, encoded, reencoded, "new opcodes must round-trip losslessly through the module wire format")

	verifier, err := avm.NewVerifier(avm.DefaultParams())
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(decoded))
}

// TestByteConstantFolds proves a byte constant can be declared at compile time
// via bytes.fromHex and used as an identifier (lowered to a constant push) —
// the domain-separation tags / magic prefixes bridge and PoW contracts need.
func TestByteConstantFolds(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(cryptoBuiltinSource))
	require.NoError(t, err)
	require.True(t, hasOpcode(res.Module.Code, avm.OpPushBytes),
		"a bytes.fromHex constant used in a getter must lower to an OpPushBytes")
}
