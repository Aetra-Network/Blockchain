package conformance

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/ripemd160"
	"golang.org/x/crypto/sha3"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// TestAcceptancePowMinerExample compiles the proof-of-work reference contract
// and asserts (1) that it VERIFIES — proving the compiled module carries the new
// byte-exact hash / byte opcodes and still passes interface verification — and
// (2) that each new primitive actually EXECUTES end to end through the compiled
// contract, matching a Go known-answer vector. The PoW predicate itself
// (sha256(nonce||challenge) via concat + slice + fromBytesBE below a target) is
// covered by compilation + verification; its inputs are exercised directly by
// the getters below.
func TestAcceptancePowMinerExample(t *testing.T) {
	deployer := testAddress(0x51)
	res := compileExampleFile(t, filepath.Join("pow", "pow_miner.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	challenge, err := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	require.NoError(t, err)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	runGetter := func(name string) avm.RuntimeValue {
		op := opcodeForGetter(t, res, name)
		exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			Message:  async.MessageEnvelope{Opcode: op, GasLimit: 5_000_000},
			GasLimit: 5_000_000,
		})
		require.NoError(t, err)
		require.Equalf(t, async.ResultOK, exec.ResultCode, "getter %q result", name)
		return exec.ReturnValue
	}
	getHash := func(name string) [32]byte {
		h, err := runGetter(name).AsHash()
		require.NoError(t, err)
		return h
	}
	getBytes := func(name string) []byte {
		b, err := runGetter(name).AsBytes()
		require.NoError(t, err)
		return b
	}
	getU64 := func(name string) uint64 {
		u, err := runGetter(name).AsUint64()
		require.NoError(t, err)
		return u
	}

	// sha256 — the byte-exact hash the PoW predicate hashes the preimage with.
	wantSha := sha256.Sum256(challenge)
	require.Equal(t, wantSha, getHash("challengeSha256"), "byte-exact sha256 must differ from hash() and match the standard digest")

	// keccak256 — MUST use legacy (pre-NIST) Keccak padding so a bridge matches
	// Ethereum. sha3.New256 (FIPS) would produce a different digest.
	kh := sha3.NewLegacyKeccak256()
	kh.Write(challenge)
	var wantKeccak [32]byte
	copy(wantKeccak[:], kh.Sum(nil))
	require.Equal(t, wantKeccak, getHash("challengeKeccak256"))

	// blake2b-256.
	wantBlake := blake2b.Sum256(challenge)
	require.Equal(t, wantBlake, getHash("challengeBlake2b"))

	// ripemd160 — 20-byte digest returned as bytes.
	rh := ripemd160.New()
	rh.Write(challenge)
	require.Equal(t, rh.Sum(nil), getBytes("challengeRipemd160"))

	// sha512 — 64-byte digest returned as bytes.
	wantSha512 := sha512.Sum512(challenge)
	require.Equal(t, wantSha512[:], getBytes("challengeSha512"))

	// byteAt(challenge, 0).
	require.Equal(t, uint64(challenge[0]), getU64("challengeByte0"))

	// slice(challenge, 0, 4).
	require.Equal(t, challenge[:4], getBytes("challengePrefix"))

	// toBytesBE(POW_TARGET, 8): 0x0000ffffffffffff big-endian in 8 bytes.
	require.Equal(t, []byte{0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, getBytes("targetBytes"))
}
