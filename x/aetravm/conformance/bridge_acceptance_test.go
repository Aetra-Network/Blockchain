package conformance

import (
	"crypto/sha256"
	"path/filepath"
	"testing"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/sha3"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// ---------------------------------------------------------------------------
// Independent Go oracle. These recompute the Merkle roots and signatures with
// the standard library (crypto/sha256, x/crypto/sha3, decred secp256k1) so the
// expected answers do NOT come from the in-contract routine under test.
// ---------------------------------------------------------------------------

// RFC-6962 DOMAIN-SEPARATED hashes. Leaves are tagged with a 0x00 preimage
// prefix and internal nodes with 0x01, so an internal digest can never be
// re-presented as a leaf (the second-preimage forgery the audit reproduced).
// These MUST match the on-chain leafHash*/hashNode* helpers byte-for-byte.
func shaLeaf(data []byte) []byte {
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write(data)
	return h.Sum(nil)
}

func shaNode(left, right []byte) []byte {
	h := sha256.New()
	h.Write([]byte{0x01})
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

func keccakLeaf(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte{0x00})
	h.Write(data)
	return h.Sum(nil)
}

func keccakNode(left, right []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte{0x01})
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

// buildMerkleProof builds a full binary Merkle tree over `rawLeaves` (a
// power-of-two count of raw 32-byte leaves) using RFC-6962 domain separation:
// the leaf level is leafHash(raw) = H(0x00||raw) and every internal level is
// nodeHash(l,r) = H(0x01||l||r). It then extracts the audit path for leaf `idx`
// and returns the RAW leaf (the contract recomputes leafHash on-chain), the flat
// sibling blob (bottom-up), the direction bytes (0 = the running node is the
// LEFT child at that level, 1 = it is the RIGHT child), and the tree root — the
// exact byte layout the contract slices.
func buildMerkleProof(t *testing.T, rawLeaves [][]byte, idx int, leafHash func([]byte) []byte, nodeHash func(a, b []byte) []byte) (leaf, proof, directions, root []byte) {
	t.Helper()
	require.Greater(t, len(rawLeaves), 0)
	// power of two
	require.Equal(t, 0, len(rawLeaves)&(len(rawLeaves)-1), "leaf count must be a power of two")
	leaf = append([]byte(nil), rawLeaves[idx]...) // raw; the contract prepends 0x00

	// Level 0 is the domain-separated leaf hashes H(0x00||raw).
	level := make([][]byte, len(rawLeaves))
	for i, rl := range rawLeaves {
		level[i] = leafHash(rl)
	}
	cur := idx
	for len(level) > 1 {
		sibIdx := cur ^ 1
		proof = append(proof, level[sibIdx]...)
		if cur&1 == 0 {
			directions = append(directions, 0) // running node is the left child
		} else {
			directions = append(directions, 1) // running node is the right child
		}
		next := make([][]byte, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			next[i/2] = nodeHash(level[i], level[i+1])
		}
		level = next
		cur /= 2
	}
	root = append([]byte(nil), level[0]...)
	return leaf, proof, directions, root
}

func makeLeaves(n int) [][]byte {
	leaves := make([][]byte, n)
	for i := range leaves {
		l := make([]byte, 32)
		for j := range l {
			l[j] = byte((i*31 + j*7 + 1) & 0xff)
		}
		leaves[i] = l
	}
	return leaves
}

// signCompactRS returns the 64-byte compact R‖S (canonical low-S) signature of
// `digest` under `priv`, the exact form verifySecp256k1 accepts.
func signCompactRS(t *testing.T, priv *secp256k1.PrivateKey, digest []byte) []byte {
	t.Helper()
	// decred SignCompact yields [headerByte | R(32) | S(32)], RFC6979 + low-S.
	compact := ecdsa.SignCompact(priv, digest, false)
	require.Len(t, compact, 65)
	return append([]byte(nil), compact[1:65]...)
}

func makeValidators(n int) []*secp256k1.PrivateKey {
	out := make([]*secp256k1.PrivateKey, n)
	for i := range out {
		b := make([]byte, 32)
		for j := range b {
			b[j] = byte((i*13 + j*5 + 3) & 0xff)
		}
		b[31] |= 1 // ensure non-zero scalar
		out[i] = secp256k1.PrivKeyFromBytes(b)
	}
	return out
}

func concatBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestAcceptanceBridgeVerify compiles the bridge trust-primitive contract and
// EXECUTES it end to end against a fully independent Go oracle:
//
//   - Merkle inclusion (sha256 AND keccak256): a valid audit path ACCEPTS; a
//     tampered leaf, a tampered sibling, a tampered root, and a flipped
//     direction each REJECT.
//   - Batched validator-signature verify: a full valid set meets the strict
//     > 2/3 quorum; a set with only half the signatures valid does NOT.
//   - Light-client accept: accepts iff BOTH the quorum AND the Merkle inclusion
//     hold; a below-threshold signature set OR a broken proof rejects.
//
// The contract's iterative verification runs in the @internal handler (ATLX
// getters are pure and cannot carry an accumulator loop), so each case submits
// a proof as a message and reads the committed verdict back through a getter.
func TestAcceptanceBridgeVerify(t *testing.T) {
	deployer := testAddress(0xbb)
	res := compileExampleFile(t, filepath.Join("bridge", "bridge_verify.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	initialState := avm.Storage{
		"merkleShaResult":    avm.EncodeU64(0),
		"merkleKeccakResult": avm.EncodeU64(0),
		"sigCount":           avm.EncodeU64(0),
		"thresholdResult":    avm.EncodeU64(0),
		"accepted":           avm.EncodeU64(0),
	}

	// submit runs one proof message through the @internal handler and returns
	// the committed state.
	submit := func(msgName string, fields map[string]any) avm.Storage {
		body := mustCodecBody(t, res.MessageBodies[msgName], fields)
		exec, err := runner.Run(res.Module, initialState, avm.RuntimeContext{
			Entry:           avm.EntryReceiveInternal,
			ContractAddress: testAddress(0xbb),
			GasLimit:        80_000_000,
			Message: async.MessageEnvelope{
				Opcode:   res.MessageBodyOpcodes[msgName],
				QueryID:  uint64(res.MessageBodyOpcodes[msgName]),
				Body:     body,
				GasLimit: 80_000_000,
			},
		})
		require.NoErrorf(t, err, "submit %s", msgName)
		require.Equalf(t, async.ResultOK, exec.ResultCode, "submit %s result", msgName)
		return exec.State
	}
	getU64 := func(state avm.Storage, getter string) uint64 {
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			GasLimit: 5_000_000,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, getter), GasLimit: 5_000_000},
		})
		require.NoError(t, err)
		require.Equalf(t, async.ResultOK, exec.ResultCode, "getter %s result", getter)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}

	// ---------------- 1. MERKLE INCLUSION (sha256) ----------------
	leaves := makeLeaves(8)
	leaf, proof, directions, root := buildMerkleProof(t, leaves, 5, shaLeaf, shaNode)

	// (1a) valid proof ACCEPTS.
	st := submit("VerifyMerkleSha", map[string]any{
		"leaf": leaf, "proof": proof, "directions": directions, "root": root,
	})
	require.Equal(t, uint64(1), getU64(st, "merkleShaResult"), "a valid sha256 Merkle proof must be accepted")

	// (1b) tampered LEAF REJECTS.
	badLeaf := append([]byte(nil), leaf...)
	badLeaf[0] ^= 0xff
	st = submit("VerifyMerkleSha", map[string]any{
		"leaf": badLeaf, "proof": proof, "directions": directions, "root": root,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleShaResult"), "a tampered leaf must be rejected")

	// (1c) tampered SIBLING REJECTS (flip one byte of the first sibling).
	badProof := append([]byte(nil), proof...)
	badProof[0] ^= 0x01
	st = submit("VerifyMerkleSha", map[string]any{
		"leaf": leaf, "proof": badProof, "directions": directions, "root": root,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleShaResult"), "a tampered sibling must be rejected")

	// (1d) tampered ROOT REJECTS.
	badRoot := append([]byte(nil), root...)
	badRoot[31] ^= 0x80
	st = submit("VerifyMerkleSha", map[string]any{
		"leaf": leaf, "proof": proof, "directions": directions, "root": badRoot,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleShaResult"), "a tampered root must be rejected")

	// (1e) WRONG DIRECTION REJECTS (flip the orientation at the first level, so
	// the sibling is concatenated on the wrong side). This is the security-
	// critical concat-order check: the digest silently differs.
	badDirs := append([]byte(nil), directions...)
	badDirs[0] ^= 0x01
	st = submit("VerifyMerkleSha", map[string]any{
		"leaf": leaf, "proof": proof, "directions": badDirs, "root": root,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleShaResult"), "a wrong-direction proof must be rejected (concat order is security-critical)")

	// Sanity: swapping the concat order at the first level yields a different
	// node digest, which is exactly why the direction byte is security-critical.
	firstSib := proof[:32]
	onchainLeafHash := shaLeaf(leaf) // H(0x00||leaf), the value the contract starts from
	require.NotEqual(t, shaNode(firstSib, onchainLeafHash), shaNode(onchainLeafHash, firstSib),
		"swapping the first concat order must change the level hash")

	// (1f) SECOND-PREIMAGE FORGERY (the exact defect the audit reproduced) now
	// REJECTS. Reconstruct the 8-leaf sha256 tree explicitly so we can grab an
	// INTERNAL node. Without leaf/node domain separation an internal digest
	// H(L0||L1) has the same byte shape as a leaf, so it could be submitted AS a
	// leaf with the truncated audit path that already sits above it and would
	// verify against the root — forging inclusion of a value that was never a
	// leaf (on a bridge: an unbacked mint).
	lh := make([][]byte, 8)
	for i := range leaves {
		lh[i] = shaLeaf(leaves[i])
	}
	n01 := shaNode(lh[0], lh[1])
	n23 := shaNode(lh[2], lh[3])
	n45 := shaNode(lh[4], lh[5])
	n67 := shaNode(lh[6], lh[7])
	n0123 := shaNode(n01, n23)
	n4567 := shaNode(n45, n67)
	forgeRoot := shaNode(n0123, n4567)
	require.Equal(t, root, forgeRoot, "reconstructed root must match the tree under test")

	// The truncated path [n23, n4567] with directions [0,0] IS a genuine internal
	// path from n01 up to the root (n01 really is a subtree root under it), so the
	// ONLY thing that can stop the forgery is leaf/node domain separation.
	require.Equal(t, forgeRoot, shaNode(shaNode(n01, n23), n4567),
		"the truncated path must be a valid INTERNAL path to the root (only domain separation stops the forgery)")

	st = submit("VerifyMerkleSha", map[string]any{
		"leaf": n01, "proof": concatBytes(n23, n4567), "directions": []byte{0, 0}, "root": forgeRoot,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleShaResult"),
		"an internal node presented as a leaf must be REJECTED (RFC-6962 domain separation closes the second-preimage forgery)")

	// (1g) A ragged proof whose last sibling is only 31 bytes REJECTS: the blob is
	// not a whole number of 32-byte siblings, so length discipline rejects it
	// before any hashing (rather than silently reinterpreting the misaligned bytes).
	shortProof := append([]byte(nil), proof[:len(proof)-1]...) // last sibling now 31 bytes
	st = submit("VerifyMerkleSha", map[string]any{
		"leaf": leaf, "proof": shortProof, "directions": directions, "root": root,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleShaResult"),
		"a sibling that is not exactly 32 bytes must be rejected (length discipline)")

	// (1h) A direction vector whose length disagrees with the number of proof
	// levels REJECTS (one direction per 32-byte sibling is required).
	shortDirs := append([]byte(nil), directions[:len(directions)-1]...)
	st = submit("VerifyMerkleSha", map[string]any{
		"leaf": leaf, "proof": proof, "directions": shortDirs, "root": root,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleShaResult"),
		"a direction count that disagrees with the proof-level count must be rejected")

	// (1i) A root that is not exactly 32 bytes REJECTS (and must NOT trap the VM):
	// the length guard rejects it before fromBytesBE is ever applied to it.
	longRoot := append(append([]byte(nil), root...), 0x00)
	st = submit("VerifyMerkleSha", map[string]any{
		"leaf": leaf, "proof": proof, "directions": directions, "root": longRoot,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleShaResult"),
		"a root that is not exactly 32 bytes must be rejected (length discipline)")

	// ---------------- 2. MERKLE INCLUSION (keccak256 / Ethereum) ----------------
	kLeaves := makeLeaves(4)
	kLeaf, kProof, kDirs, kRoot := buildMerkleProof(t, kLeaves, 2, keccakLeaf, keccakNode)

	st = submit("VerifyMerkleKeccak", map[string]any{
		"leaf": kLeaf, "proof": kProof, "directions": kDirs, "root": kRoot,
	})
	require.Equal(t, uint64(1), getU64(st, "merkleKeccakResult"), "a valid keccak256 Merkle proof must be accepted")

	// A keccak proof checked against the SHA root of the same leaves must reject
	// — the hash choice is security-critical.
	_, _, _, shaRootOfKLeaves := buildMerkleProof(t, kLeaves, 2, shaLeaf, shaNode)
	st = submit("VerifyMerkleKeccak", map[string]any{
		"leaf": kLeaf, "proof": kProof, "directions": kDirs, "root": shaRootOfKLeaves,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleKeccakResult"), "keccak path against a sha256 root must be rejected (wrong hash)")

	// tampered keccak leaf rejects.
	badKLeaf := append([]byte(nil), kLeaf...)
	badKLeaf[5] ^= 0x22
	st = submit("VerifyMerkleKeccak", map[string]any{
		"leaf": badKLeaf, "proof": kProof, "directions": kDirs, "root": kRoot,
	})
	require.Equal(t, uint64(0), getU64(st, "merkleKeccakResult"), "a tampered keccak leaf must be rejected")

	// ---------------- 3. BATCHED VALIDATOR SIGNATURES ----------------
	header := make([]byte, 32)
	for i := range header {
		header[i] = byte(0xC0 + i)
	}
	headerDigest := sha256.Sum256(header)
	headerHash := headerDigest[:] // the 32-byte value signed and submitted

	validators := makeValidators(4)
	var pubBlob []byte
	for _, v := range validators {
		pubBlob = append(pubBlob, v.PubKey().SerializeCompressed()...)
	}

	// Full valid set: all four sign the header hash.
	var fullSigs []byte
	for _, v := range validators {
		fullSigs = append(fullSigs, signCompactRS(t, v, headerHash)...)
	}
	st = submit("VerifySigs", map[string]any{
		"headerHash": headerHash, "pubkeys": pubBlob, "sigs": fullSigs,
	})
	require.Equal(t, uint64(4), getU64(st, "sigCount"), "all four validator signatures must verify")
	require.Equal(t, uint64(1), getU64(st, "thresholdResult"), "4/4 clears the strict > 2/3 quorum")

	// Exactly 3/4 valid still clears > 2/3 (3*3=9 > 4*2=8).
	otherDigest := sha256.Sum256([]byte("a different header"))
	threeGood := concatBytes(
		signCompactRS(t, validators[0], headerHash),
		signCompactRS(t, validators[1], headerHash),
		signCompactRS(t, validators[2], headerHash),
		signCompactRS(t, validators[3], otherDigest[:]), // invalid over headerHash
	)
	st = submit("VerifySigs", map[string]any{
		"headerHash": headerHash, "pubkeys": pubBlob, "sigs": threeGood,
	})
	require.Equal(t, uint64(3), getU64(st, "sigCount"), "three of four signatures verify")
	require.Equal(t, uint64(1), getU64(st, "thresholdResult"), "3/4 clears the strict > 2/3 quorum")

	// Only 2/4 valid: below the quorum (2*3=6 is NOT > 8).
	twoGood := concatBytes(
		signCompactRS(t, validators[0], headerHash),
		signCompactRS(t, validators[1], headerHash),
		signCompactRS(t, validators[2], otherDigest[:]), // invalid over headerHash
		signCompactRS(t, validators[3], otherDigest[:]), // invalid over headerHash
	)
	st = submit("VerifySigs", map[string]any{
		"headerHash": headerHash, "pubkeys": pubBlob, "sigs": twoGood,
	})
	require.Equal(t, uint64(2), getU64(st, "sigCount"), "only two of four signatures verify")
	require.Equal(t, uint64(0), getU64(st, "thresholdResult"), "2/4 does NOT clear the strict > 2/3 quorum")

	// DUPLICATE-SIGNER batch does NOT meet quorum after dedup. The submitted set
	// is [v0, v0, v0, v3] with valid signatures [s0, s0, s0, s3]: every signature
	// verifies on its own, but v0 is a single validator replayed three times.
	// Without distinct-signer dedup that would tally 4/4 and forge a quorum;
	// after dedup only the DISTINCT keys {v0, v3} count -> 2 valid, and 2*3 = 6
	// is NOT > 4*2 = 8, so the forged super-majority is rejected.
	dupPubs := concatBytes(
		validators[0].PubKey().SerializeCompressed(),
		validators[0].PubKey().SerializeCompressed(),
		validators[0].PubKey().SerializeCompressed(),
		validators[3].PubKey().SerializeCompressed(),
	)
	dupSigs := concatBytes(
		signCompactRS(t, validators[0], headerHash),
		signCompactRS(t, validators[0], headerHash),
		signCompactRS(t, validators[0], headerHash),
		signCompactRS(t, validators[3], headerHash),
	)
	st = submit("VerifySigs", map[string]any{
		"headerHash": headerHash, "pubkeys": dupPubs, "sigs": dupSigs,
	})
	require.Equal(t, uint64(2), getU64(st, "sigCount"),
		"a duplicated validator key must be counted only once (distinct-signer dedup)")
	require.Equal(t, uint64(0), getU64(st, "thresholdResult"),
		"one replayed validator plus one real one must NOT meet the strict > 2/3 quorum")

	// ---------------- 4. LIGHT-CLIENT ACCEPT (both proofs) ----------------
	// The Merkle proof is rooted at the header's stateRoot. Use a fresh tree and
	// treat its root as the stateRoot the validators (implicitly) committed.
	lcLeaves := makeLeaves(8)
	lcLeaf, lcProof, lcDirs, stateRoot := buildMerkleProof(t, lcLeaves, 3, shaLeaf, shaNode)

	// (4a) valid quorum + valid inclusion => ACCEPT.
	st = submit("LightClientVerify", map[string]any{
		"headerHash": headerHash, "pubkeys": pubBlob, "sigs": fullSigs,
		"leaf": lcLeaf, "proof": lcProof, "directions": lcDirs, "stateRoot": stateRoot,
	})
	require.Equal(t, uint64(1), getU64(st, "accepted"), "quorum AND inclusion both hold => accept")
	require.Equal(t, uint64(4), getU64(st, "sigCount"))
	require.Equal(t, uint64(1), getU64(st, "thresholdResult"))

	// (4b) below-threshold signatures => REJECT even though the proof is valid.
	st = submit("LightClientVerify", map[string]any{
		"headerHash": headerHash, "pubkeys": pubBlob, "sigs": twoGood,
		"leaf": lcLeaf, "proof": lcProof, "directions": lcDirs, "stateRoot": stateRoot,
	})
	require.Equal(t, uint64(0), getU64(st, "accepted"), "below-threshold signature set must reject the whole proof")
	require.Equal(t, uint64(0), getU64(st, "thresholdResult"))

	// (4c) good quorum but tampered inclusion (flipped direction) => REJECT.
	lcBadDirs := append([]byte(nil), lcDirs...)
	lcBadDirs[0] ^= 0x01
	st = submit("LightClientVerify", map[string]any{
		"headerHash": headerHash, "pubkeys": pubBlob, "sigs": fullSigs,
		"leaf": lcLeaf, "proof": lcProof, "directions": lcBadDirs, "stateRoot": stateRoot,
	})
	require.Equal(t, uint64(0), getU64(st, "accepted"), "a broken Merkle proof must reject even with a full quorum")
	require.Equal(t, uint64(1), getU64(st, "thresholdResult"), "the quorum still held; only the inclusion failed")

	// (4d) good quorum but wrong stateRoot => REJECT.
	badStateRoot := append([]byte(nil), stateRoot...)
	badStateRoot[0] ^= 0x0f
	st = submit("LightClientVerify", map[string]any{
		"headerHash": headerHash, "pubkeys": pubBlob, "sigs": fullSigs,
		"leaf": lcLeaf, "proof": lcProof, "directions": lcDirs, "stateRoot": badStateRoot,
	})
	require.Equal(t, uint64(0), getU64(st, "accepted"), "a wrong stateRoot must reject even with a full quorum")
}
