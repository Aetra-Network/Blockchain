package compiler

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
)

// sigMathBuiltinSource exercises the mulDiv / mulDivRoundUp / verifySecp256k1 /
// ecrecover builtins in getters that return each builtin's exact result type, so
// every one must lower to its dedicated opcode.
const sigMathBuiltinSource = `
const INPUT = bytes.fromHex("00112233445566778899aabbccddeeff")

@storage
struct SigMathState {
  value: u64 = 0
}

@message(11)
struct Poke {}

type SigMathMsg = Poke

contract SigMathDemo {
  storage: SigMathState
  incomingMessages: SigMathMsg
  namespace "sigmathdemo"
  chain "avm-local"

  @store
  func SigMathState.load() {
    return SigMathState.fromChunk(contract.getData())
  }

  @store
  func SigMathState.save(self) {
    contract.setData(self.toChunk())
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy SigMathMsg.fromSegment(in.body)
    match (msg) {
      Poke => {
        var st = lazy SigMathState.load()
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

  @get func gMulDiv(): uint256 { return mulDiv(7, 3, 2) }
  @get func gMulDivRoundUp(): uint256 { return mulDivRoundUp(7, 3, 2) }
  @get func gMulDivNearest(): uint256 { return mulDivNearest(7, 3, 2) }
  @get func gMulDivFloor(): uint256 { return mulDivFloor(7, 3, 2) }
  @get func gMulDivCeil(): uint256 { return mulDivCeil(7, 3, 2) }
  @get func gIsqrt(): uint256 { return isqrt(144) }
  @get func gVerifySecp(): uint64 { return verifySecp256k1(sha256(INPUT), INPUT, INPUT) ? 1 : 0 }
  @get func gEcrecover(): bytes { return ecrecover(sha256(INPUT), INPUT) }
  @get func gMulCmp(): int256 { return mulCmp(3, 4, 2, 5) }
  @get func gMulDivSigned(): int256 { return mulDivSigned(6, 7, 3) }
  @get func gSubBytes(): bytes { return subBytes(INPUT, 1, 2) }
}
`

// TestSigMathBuiltinsLowerToOpcodes proves the mulDiv/secp256k1 source builtins
// lower to their dedicated VM opcodes (compiler and interpreter halves stay in
// sync) and the resulting module still verifies with those opcodes present.
func TestSigMathBuiltinsLowerToOpcodes(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(sigMathBuiltinSource))
	require.NoError(t, err)
	require.NotNil(t, res)

	for _, op := range []avm.Opcode{
		avm.OpMulDiv,
		avm.OpMulDivRoundUp,
		avm.OpMulDivNearest,
		avm.OpVerifySecp256k1,
		avm.OpEcrecover,
		avm.OpIsqrt,
		avm.OpMulCmp,
		avm.OpMulDivSigned,
	} {
		require.Truef(t, hasOpcode(res.Module.Code, op),
			"builtin must lower to opcode 0x%02x", byte(op))
	}

	verifier, err := avm.NewVerifier(avm.DefaultParams())
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(res.Module))
}

// mulDivAliasSource (parameterized by which alias is called) proves
// mulDivFloor/mulDivCeil are pure spellings of mulDiv/mulDivRoundUp: each must
// lower to the SAME opcode as its canonical name, and only that opcode.
const mulDivAliasSourceTemplate = `
@storage
struct AliasState {
  value: u64 = 0
}

@message(11)
struct Poke {}

type AliasMsg = Poke

contract MulDivAliasDemo {
  storage: AliasState
  incomingMessages: AliasMsg
  namespace "muldivaliasdemo"
  chain "avm-local"

  @store
  func AliasState.load() {
    return AliasState.fromChunk(contract.getData())
  }

  @store
  func AliasState.save(self) {
    contract.setData(self.toChunk())
  }

  @internal
  func onInternalMessage(in: InMessage) {
    const msg = lazy AliasMsg.fromSegment(in.body)
    match (msg) {
      Poke => {
        var st = lazy AliasState.load()
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

  @get func gAlias(): uint256 { return %s(7, 3, 2) }
}
`

// TestMulDivFloorCeilAreAliases proves mulDivFloor lowers to exactly
// avm.OpMulDiv (not avm.OpMulDivRoundUp) and mulDivCeil lowers to exactly
// avm.OpMulDivRoundUp (not avm.OpMulDiv) -- i.e. they are pure alternate
// spellings, not new opcodes.
func TestMulDivFloorCeilAreAliases(t *testing.T) {
	cases := []struct {
		name        string
		builtin     string
		wantOpcode  avm.Opcode
		otherOpcode avm.Opcode
	}{
		{"mulDivFloor", "mulDivFloor", avm.OpMulDiv, avm.OpMulDivRoundUp},
		{"mulDivCeil", "mulDivCeil", avm.OpMulDivRoundUp, avm.OpMulDiv},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(DefaultOptions())
			require.NoError(t, err)

			src := fmt.Sprintf(mulDivAliasSourceTemplate, tc.builtin)
			res, err := c.Compile([]byte(src))
			require.NoError(t, err)
			require.NotNil(t, res)

			require.Truef(t, hasOpcode(res.Module.Code, tc.wantOpcode),
				"%s() must lower to opcode 0x%02x", tc.builtin, byte(tc.wantOpcode))
			require.Falsef(t, hasOpcode(res.Module.Code, tc.otherOpcode),
				"%s() must NOT lower to opcode 0x%02x", tc.builtin, byte(tc.otherOpcode))

			verifier, err := avm.NewVerifier(avm.DefaultParams())
			require.NoError(t, err)
			require.NoError(t, verifier.Verify(res.Module))
		})
	}
}

// TestSigMathBuiltinsModuleRoundTrips confirms the generic instruction wire
// format encodes/decodes the new opcodes losslessly and the decoded module still
// verifies.
func TestSigMathBuiltinsModuleRoundTrips(t *testing.T) {
	c, err := New(DefaultOptions())
	require.NoError(t, err)

	res, err := c.Compile([]byte(sigMathBuiltinSource))
	require.NoError(t, err)

	encoded, err := avm.EncodeModule(res.Module)
	require.NoError(t, err)

	decoded, err := avm.DecodeModule(encoded)
	require.NoError(t, err)

	for _, op := range []avm.Opcode{
		avm.OpMulDiv, avm.OpMulDivRoundUp, avm.OpMulDivNearest, avm.OpVerifySecp256k1, avm.OpEcrecover, avm.OpIsqrt,
		avm.OpMulCmp, avm.OpMulDivSigned,
	} {
		require.Truef(t, hasOpcode(decoded.Code, op), "opcode 0x%02x must survive decode", byte(op))
	}

	reencoded, err := avm.EncodeModule(decoded)
	require.NoError(t, err)
	require.Equal(t, encoded, reencoded, "new opcodes must round-trip losslessly through the module wire format")

	verifier, err := avm.NewVerifier(avm.DefaultParams())
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(decoded))
}
