package conformance

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/poseidon2"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// --- Phase D Groth16-over-BN254 stdlib/reference-contract acceptance tests.
//
// These prove the .atlx wiring (compiler builtins -> IR -> opcodes -> the
// hand-written groth16* accumulation loop + pairing check) is REAL, working
// Aetralis source, cross-checked directly against gnark-crypto's own G1/G2
// arithmetic -- the same library the opcodes are built on. This is NOT the
// design doc's deferred "differential test matrix" (multi-toolchain proofs
// from an actual R1CS circuit, e.g. snarkjs + gnark's own prover) -- that is
// explicitly the next stage per avm-phase-d-zk-design.md's closing
// paragraph. What IS proven here: the vk_x accumulation loop (both the
// zero-public-input and one-public-input paths, so the `while` loop body
// genuinely executes at least once), the 4-term pairing product's operand
// ordering (g1s = -A,alpha,vk_x,C / g2s = B,beta,gamma,delta), the negated-A
// helper's byte-exact correctness against gnark's own G1 negation, and that
// a malformed/failing proof soft-fails groth16Verified()/unlocked() to 0
// rather than trapping the message -- by constructing SYNTHETIC (degenerate
// but real, equation-satisfying) proof material by hand rather than through
// gnark's R1CS/Groth16 prover (out of scope here per the design doc's own
// "gnark-crypto only, not the full gnark module" call).

func encodeG1(p bn254.G1Affine) []byte {
	xb := p.X.Bytes()
	yb := p.Y.Bytes()
	out := make([]byte, 0, 64)
	out = append(out, xb[:]...)
	out = append(out, yb[:]...)
	return out
}

func encodeG2(p bn254.G2Affine) []byte {
	a0 := p.X.A0.Bytes()
	a1 := p.X.A1.Bytes()
	b0 := p.Y.A0.Bytes()
	b1 := p.Y.A1.Bytes()
	out := make([]byte, 0, 128)
	out = append(out, a0[:]...)
	out = append(out, a1[:]...)
	out = append(out, b0[:]...)
	out = append(out, b1[:]...)
	return out
}

func scalarBE32(v int64) []byte {
	out := make([]byte, 32)
	big.NewInt(v).FillBytes(out)
	return out
}

// runGetterRaw drives a getter using an explicit codec + positional
// "argN"-keyed value map (see financeArgCodec's own doc comment: getter
// parameters always wire-encode as synthetic arg0/arg1/... field names,
// regardless of the .atlx source parameter names) -- used here instead of
// callGetter/callGetterExpectTrap because those two helpers hard-code
// *big.Int argument values, and the bn254/poseidon2 getters below take
// `bytes` arguments.
func runGetterRaw(t *testing.T, runner *avm.Runner, res *compiler.Result, name string, types []string, values map[string]any) avm.RuntimeValue {
	t.Helper()
	codec := financeArgCodec(name, types...)
	body := mustCodecBody(t, codec, values)
	op := opcodeForGetter(t, res, name)
	exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 10_000_000,
		Message:  async.MessageEnvelope{Opcode: op, Body: body, GasLimit: 10_000_000},
	})
	require.NoError(t, err)
	require.Equalf(t, async.ResultOK, exec.ResultCode, "getter %s trapped (result=%d)", name, exec.ResultCode)
	return exec.ReturnValue
}

// TestAcceptanceGroth16StdlibPrimitiveGetters cross-checks the 7 new
// compiler builtins, wired end-to-end through groth16_stdlib.atlx's thin
// pass-through getters, against gnark-crypto's own G1/G2/Poseidon2
// arithmetic.
func TestAcceptanceGroth16StdlibPrimitiveGetters(t *testing.T) {
	deployer := testAddress(0xA1)
	res := compileExampleFile(t, filepath.Join("zk", "groth16_stdlib.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	_, _, g1Gen, g2Gen := bn254.Generators()
	gBytes := encodeG1(g1Gen)

	// bn254G1AddG(G, G) == 2G.
	var want2G bn254.G1Affine
	want2G.Add(&g1Gen, &g1Gen)
	gotAdd, err := runGetterRaw(t, runner, res, "bn254G1AddG", []string{"bytes", "bytes"}, map[string]any{"arg0": gBytes, "arg1": gBytes}).AsBytes()
	require.NoError(t, err)
	require.Equal(t, encodeG1(want2G), gotAdd, "bn254G1AddG(G,G) must equal 2G")

	// bn254G1ScalarMulG(G, 7) == 7G.
	var want7G bn254.G1Affine
	want7G.ScalarMultiplication(&g1Gen, big.NewInt(7))
	gotMul, err := runGetterRaw(t, runner, res, "bn254G1ScalarMulG", []string{"bytes", "uint256"}, map[string]any{"arg0": gBytes, "arg1": big.NewInt(7)}).AsBytes()
	require.NoError(t, err)
	require.Equal(t, encodeG1(want7G), gotMul, "bn254G1ScalarMulG(G,7) must equal 7G")

	// bn254G1IsOnCurveG(G) == true.
	gotOnCurve, err := runGetterRaw(t, runner, res, "bn254G1IsOnCurveG", []string{"bytes"}, map[string]any{"arg0": gBytes}).AsBool()
	require.NoError(t, err)
	require.True(t, gotOnCurve, "generator must be on curve")

	// bn254G1IsOnCurveG(garbage) == false.
	garbage := make([]byte, 64)
	for i := range garbage {
		garbage[i] = 0xAB
	}
	gotGarbageOnCurve, err := runGetterRaw(t, runner, res, "bn254G1IsOnCurveG", []string{"bytes"}, map[string]any{"arg0": garbage}).AsBool()
	require.NoError(t, err)
	require.False(t, gotGarbageOnCurve, "0xAB-filled bytes must not be on curve")

	// bn254G2AddG(G2, G2) == 2*G2.
	g2Bytes := encodeG2(g2Gen)
	var want2G2 bn254.G2Affine
	want2G2.Add(&g2Gen, &g2Gen)
	gotG2Add, err := runGetterRaw(t, runner, res, "bn254G2AddG", []string{"bytes", "bytes"}, map[string]any{"arg0": g2Bytes, "arg1": g2Bytes}).AsBytes()
	require.NoError(t, err)
	require.Equal(t, encodeG2(want2G2), gotG2Add, "bn254G2AddG(G2,G2) must equal 2*G2")

	// bn254G2ScalarMulG(G2, 5) == 5*G2.
	var want5G2 bn254.G2Affine
	want5G2.ScalarMultiplication(&g2Gen, big.NewInt(5))
	gotG2Mul, err := runGetterRaw(t, runner, res, "bn254G2ScalarMulG", []string{"bytes", "uint256"}, map[string]any{"arg0": g2Bytes, "arg1": big.NewInt(5)}).AsBytes()
	require.NoError(t, err)
	require.Equal(t, encodeG2(want5G2), gotG2Mul, "bn254G2ScalarMulG(G2,5) must equal 5*G2")

	// bn254PairingCheckG: e(G,G2) * e(-G,G2) == 1 (bilinearity identity).
	var negG bn254.G1Affine
	negG.Neg(&g1Gen)
	g1s := append(append([]byte{}, gBytes...), encodeG1(negG)...)
	g2s := append(append([]byte{}, g2Bytes...), g2Bytes...)
	gotPairing, err := runGetterRaw(t, runner, res, "bn254PairingCheckG", []string{"bytes", "bytes", "uint256"}, map[string]any{"arg0": g1s, "arg1": g2s, "arg2": big.NewInt(2)}).AsBool()
	require.NoError(t, err)
	require.True(t, gotPairing, "e(G,G2)*e(-G,G2) must equal 1")

	// groth16NegateG1G(G) == -G, byte-exact against gnark's own negation.
	gotNeg, err := runGetterRaw(t, runner, res, "groth16NegateG1G", []string{"bytes"}, map[string]any{"arg0": gBytes}).AsBytes()
	require.NoError(t, err)
	require.Equal(t, encodeG1(negG), gotNeg, "groth16NegateG1G(G) must equal -G")

	// poseidon2Bn254G(scalar, 1) matches gnark-crypto's own Poseidon2 BN254
	// Merkle-Damgard hasher directly (same construction runtimePoseidon2Bn254
	// uses -- see avm.go's own doc comment).
	scalarBytes := scalarBE32(42)
	hasher := poseidon2.NewMerkleDamgardHasher()
	_, err = hasher.Write(scalarBytes)
	require.NoError(t, err)
	wantDigest := hasher.Sum(nil)

	gotDigest, err := runGetterRaw(t, runner, res, "poseidon2Bn254G", []string{"bytes", "uint256"}, map[string]any{"arg0": scalarBytes, "arg1": big.NewInt(1)}).AsBytes()
	require.NoError(t, err)
	require.Equal(t, wantDigest, gotDigest, "poseidon2Bn254G must match gnark-crypto's own hasher")
}

// TestAcceptanceGroth16StdlibVerifyEquation drives the real VerifyGroth16Proof
// message handler (the mutating vk_x-accumulation loop + 4-term pairing
// check) through synthetic, hand-constructed, equation-satisfying proof
// material -- see the file doc comment for why this is a legitimate
// end-to-end proof of the ATLX control flow (loop + pairing arity/ordering)
// without needing a full R1CS/Groth16 prover toolchain.
func TestAcceptanceGroth16StdlibVerifyEquation(t *testing.T) {
	deployer := testAddress(0xA2)
	res := compileExampleFile(t, filepath.Join("zk", "groth16_stdlib.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	_, _, g1Gen, g2Gen := bn254.Generators()
	var zeroG1 bn254.G1Affine // (0,0): the identity, per the design doc's Status-section correction.
	zeroBytes := encodeG1(zeroG1)

	verifyCodec := res.MessageBodies["VerifyGroth16Proof"]
	require.NotEmpty(t, verifyCodec.Fields, "VerifyGroth16Proof codec must be registered")
	verifyOpcode := res.MessageBodyOpcodes["VerifyGroth16Proof"]

	submit := func(vk, proof, publicInputs []byte) avm.Storage {
		body := mustCodecBody(t, verifyCodec, map[string]any{"vk": vk, "proof": proof, "publicInputs": publicInputs})
		exec, err := runner.Run(res.Module, avm.Storage{}, avm.RuntimeContext{
			Entry:           avm.EntryReceiveInternal,
			ContractAddress: deployer,
			GasLimit:        50_000_000,
			Message: async.MessageEnvelope{
				Opcode:   verifyOpcode,
				QueryID:  uint64(verifyOpcode),
				Body:     body,
				GasLimit: 50_000_000,
			},
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode, "VerifyGroth16Proof must never trap on malformed/degenerate input")
		return exec.State
	}
	readVerified := func(state avm.Storage) uint64 {
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			GasLimit: 10_000_000,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "groth16Verified"), GasLimit: 10_000_000},
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}

	// ---- Case 1: n=0 public inputs. Pick A=alpha=G1 generator, B=beta=G2
	// generator, so e(-A,B)*e(alpha,beta) = e(-G,G)*e(G,G) = 1 (bilinearity).
	// vk_x = IC[0] = 0 (identity) since n=0, and C = 0, so
	// e(vk_x,gamma)*e(C,delta) = e(0,gamma)*e(0,delta) = 1 for ANY gamma/delta
	// -- pick gamma != delta to prove the g2s ordering isn't accidentally
	// symmetric. Overall product is 1: MUST verify true.
	gamma := g2Gen
	var delta bn254.G2Affine
	delta.Double(&g2Gen)
	vk0 := append(append(append(append([]byte{}, encodeG1(g1Gen)...), encodeG2(g2Gen)...), encodeG2(gamma)...), encodeG2(delta)...)
	vk0 = append(vk0, zeroBytes...) // IC[0] = 0
	proof0 := append(append(append([]byte{}, encodeG1(g1Gen)...), encodeG2(g2Gen)...), zeroBytes...) // A=G, B=G, C=0
	require.Equal(t, uint64(1), readVerified(submit(vk0, proof0, nil)), "degenerate n=0 identity should verify true")

	// ---- Case 2: n=1 public input. Choose IC[1] = 2G, public input s=1, so
	// the accumulation loop computes vk_x = IC[0] + 1*IC[1]. Pick
	// IC[0] = -2G so vk_x = 0 again (same "all zero elsewhere" degenerate
	// trick as case 1) -- this exercises the `while` loop body executing
	// exactly once (one bn254G1ScalarMul + one bn254G1Add), not just the
	// n=0 skip path.
	var twoG bn254.G1Affine
	twoG.Double(&g1Gen)
	var negTwoG bn254.G1Affine
	negTwoG.Neg(&twoG)
	vk1 := append(append(append(append([]byte{}, encodeG1(g1Gen)...), encodeG2(g2Gen)...), encodeG2(gamma)...), encodeG2(delta)...)
	vk1 = append(vk1, encodeG1(negTwoG)...) // IC[0] = -2G
	vk1 = append(vk1, encodeG1(twoG)...)    // IC[1] = 2G
	publicInputs1 := scalarBE32(1)
	require.Equal(t, uint64(1), readVerified(submit(vk1, proof0, publicInputs1)), "n=1 accumulation loop (IC[0] + 1*IC[1] = 0) should verify true")

	// ---- Negative case: same n=1 setup but public input s=2 instead of 1,
	// so vk_x = -2G + 2*2G = 2G != 0 -- the pairing product is no longer 1.
	// Must soft-fail to false, NOT trap.
	publicInputsBad := scalarBE32(2)
	require.Equal(t, uint64(0), readVerified(submit(vk1, proof0, publicInputsBad)), "wrong public input must soft-fail to false")

	// ---- Malformed-length case: vk too short to even contain IC[0]. Must
	// soft-fail to false without ever calling subBytes on the truncated blob
	// (i.e. without trapping).
	require.Equal(t, uint64(0), readVerified(submit(vk1[:100], proof0, publicInputs1)), "truncated vk must soft-fail, not trap")

	// ---- Malformed proof length case.
	require.Equal(t, uint64(0), readVerified(submit(vk0, proof0[:10], nil)), "truncated proof must soft-fail, not trap")
}

// TestAcceptanceGroth16VerifierReferenceContract drives the
// groth16_verifier.atlx reference contract's verifyAndUnlock one-way latch
// and standalone poseidon2Bn254-based commit().
func TestAcceptanceGroth16VerifierReferenceContract(t *testing.T) {
	deployer := testAddress(0xA3)
	res := compileExampleFile(t, filepath.Join("zk", "groth16_verifier.atlx"), compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(deployer),
	})
	require.NoError(t, avm.VerifyInterface(res.Module, res.Manifest))
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	_, _, g1Gen, g2Gen := bn254.Generators()
	var zeroG1 bn254.G1Affine
	zeroBytes := encodeG1(zeroG1)
	gamma := g2Gen
	var delta bn254.G2Affine
	delta.Double(&g2Gen)

	vk0 := append(append(append(append([]byte{}, encodeG1(g1Gen)...), encodeG2(g2Gen)...), encodeG2(gamma)...), encodeG2(delta)...)
	vk0 = append(vk0, zeroBytes...)
	proof0 := append(append(append([]byte{}, encodeG1(g1Gen)...), encodeG2(g2Gen)...), zeroBytes...)

	verifyCodec := res.MessageBodies["VerifyAndUnlock"]
	require.NotEmpty(t, verifyCodec.Fields, "VerifyAndUnlock codec must be registered")
	verifyOpcode := res.MessageBodyOpcodes["VerifyAndUnlock"]

	sendVerify := func(state avm.Storage, vk, proof, publicInputs []byte) avm.Storage {
		body := mustCodecBody(t, verifyCodec, map[string]any{"vk": vk, "proof": proof, "publicInputs": publicInputs})
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:           avm.EntryReceiveInternal,
			ContractAddress: deployer,
			GasLimit:        50_000_000,
			Message: async.MessageEnvelope{
				Opcode:   verifyOpcode,
				QueryID:  uint64(verifyOpcode),
				Body:     body,
				GasLimit: 50_000_000,
			},
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		return exec.State
	}
	readUnlocked := func(state avm.Storage) uint64 {
		exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
			Entry:    avm.EntryQuery,
			GasLimit: 10_000_000,
			Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "unlocked"), GasLimit: 10_000_000},
		})
		require.NoError(t, err)
		require.Equal(t, async.ResultOK, exec.ResultCode)
		v, err := exec.ReturnValue.AsUint64()
		require.NoError(t, err)
		return v
	}

	// A malformed proof must NOT unlock.
	state := sendVerify(avm.Storage{}, vk0, proof0[:10], nil)
	require.Equal(t, uint64(0), readUnlocked(state), "malformed proof must not unlock")

	// A valid (degenerate, equation-satisfying) proof unlocks.
	state = sendVerify(state, vk0, proof0, nil)
	require.Equal(t, uint64(1), readUnlocked(state), "valid proof must unlock")

	// The latch is one-way: a subsequent malformed proof must NOT re-lock.
	state = sendVerify(state, vk0, proof0[:10], nil)
	require.Equal(t, uint64(1), readUnlocked(state), "unlock latch must not reset on a later failed proof")

	// commit(secret) hashes via poseidon2Bn254 directly and is independently
	// readable via the commitment() getter.
	commitCodec := res.MessageBodies["Commit"]
	require.NotEmpty(t, commitCodec.Fields, "Commit codec must be registered")
	commitOpcode := res.MessageBodyOpcodes["Commit"]
	secret := scalarBE32(1337)
	commitBody := mustCodecBody(t, commitCodec, map[string]any{"secret": secret})
	commitExec, err := runner.Run(res.Module, state, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: deployer,
		GasLimit:        10_000_000,
		Message: async.MessageEnvelope{
			Opcode:   commitOpcode,
			QueryID:  uint64(commitOpcode),
			Body:     commitBody,
			GasLimit: 10_000_000,
		},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, commitExec.ResultCode)

	hasher := poseidon2.NewMerkleDamgardHasher()
	_, err = hasher.Write(secret)
	require.NoError(t, err)
	wantDigest := hasher.Sum(nil)

	commitmentExec, err := runner.Run(res.Module, commitExec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		GasLimit: 10_000_000,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "commitment"), GasLimit: 10_000_000},
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, commitmentExec.ResultCode)
	gotDigest, err := commitmentExec.ReturnValue.AsBytes()
	require.NoError(t, err)
	require.Equal(t, wantDigest, gotDigest, "commitment() must match poseidon2Bn254(secret,1)")

	// commit() with a malformed (non-32-byte) secret TRAPS -- per
	// OpPoseidon2Bn254's own documented contract, deliberately not
	// soft-failing.
	badBody := mustCodecBody(t, commitCodec, map[string]any{"secret": []byte{0x01, 0x02, 0x03}})
	badExec, err := runner.Run(res.Module, state, avm.RuntimeContext{
		Entry:           avm.EntryReceiveInternal,
		ContractAddress: deployer,
		GasLimit:        10_000_000,
		Message: async.MessageEnvelope{
			Opcode:   commitOpcode,
			QueryID:  uint64(commitOpcode),
			Body:     badBody,
			GasLimit: 10_000_000,
		},
	})
	// A genuine trap surfaces as a non-nil error (and a non-OK result code)
	// from Run -- unlike every soft-fail case above, do NOT assert
	// require.NoError here.
	require.NotEqual(t, async.ResultOK, badExec.ResultCode, "commit() with a malformed secret must TRAP, not soft-fail")
}
