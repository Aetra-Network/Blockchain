# AVM Phase D ZK/pairing primitives — design v1 (REJECTED)

Status: **First adversarial review round found 3 blockers (1 crypto-soundness, 1 crypto-determinism,
1 test-coverage/soundness) plus several majors. Do not implement below as-is.** This design track runs
independently of Phase E/F's call-mechanism work and of the financial-arithmetic pipeline — it does not touch
`avm.go`/`compile.go`/`ir.go` at this design stage, so it can proceed in parallel once those land.

## What v1 got right (kept forward)

- **Dependency choice: `github.com/consensys/gnark-crypto` only, not the full `gnark` module** — gnark-crypto's
  `PairingCheck`/G1/G2 arithmetic/`Bytes()`/`SetBytes()` is sufficient to hand-write Groth16 verification; the
  full `gnark` module's value (R1CS/PLONK frontend, prover) is off-chain tooling never needed inside the VM.
  This part of the reasoning survived adversarial review unchallenged.
- **Opcode surface**: 6 required + 1 optional opcode (`OpBn254G1Add`, `OpBn254G1ScalarMul`, `OpBn254G2Add`,
  `OpBn254G2ScalarMul`, `OpBn254PairingCheck`, `OpPoseidon2Bn254`, optional `OpBn254G1IsOnCurve`) — one opcode
  per *primitive*, not per SNARK scheme; the higher-level Groth16 verify equation lives in a `.atlx` stdlib
  namespace function over these opcodes, mirroring Phase B's "stdlib = compiler newtypes + safe intrinsics
  over primitive opcodes, not new opcodes per feature" pattern. Unchallenged by review.
- **Scope call for v1: BN254 only** (verifier + Poseidon2), deferring BLS12-381 until a concrete consumer
  needs it — reduces opcode/test surface for a curve nothing currently requires. Unchallenged.
- **Byte-string ABI**: packed fixed-width records in a `bytes` blob, sliced via the existing `subBytes`/
  `byteAt`/`concat` primitives — reuses the pattern already proven in `bridge_verify.atlx`, no new ABI concept.
  Unchallenged in shape (though see blocker/minor findings on canonicalization and bounds below).
- **Reference contract + 3-tier test plan shape** (library-level known-answer vectors → real-proof-system
  differential golden vector → adversarial soft-fail matrix) — the right shape, mirroring how Phase C's bridge
  primitives were proven; review found the *depth* of tier 2 insufficient (see blocker below), not the shape.

## Why v1 is rejected — 3 blockers found by adversarial review

1. **G1 point validation is only implied, not specified as a hard requirement — an invalid-curve-attack gap.**
   v1's determinism section argues G1's subgroup equals BN254's full prime-order group (true) and therefore
   spends its explicit "mandatory check" language only on G2's `IsInSubGroup()`. An implementer reading only
   the opcode spec (not the aside in the determinism appendix) could reasonably conclude G1 points need no
   validation at all. Fix direction: the opcode spec itself (not a buried aside) must state, with no exception,
   that every decoded G1 point MUST pass `IsOnCurve()` and every decoded G2 point MUST pass `IsOnCurve()` AND
   `IsInSubGroup()`, before either enters a Miller loop. Skipping G1's on-curve check lets a crafted `proof.A`/
   `proof.C`/`vk.alpha` that isn't a curve point at all reach the pairing — a classic invalid-curve forgery,
   i.e. verifier soundness (not just liveness) is at stake.
2. **Cross-architecture determinism hazard from gnark-crypto's hand-written assembly is unaddressed — a real
   consensus-fork risk class, not hypothetical.** Unlike this codebase's existing pure-Go crypto deps (decred
   secp256k1, hdevalence ed25519consensus — no per-arch assembly), gnark-crypto ships ADX/BMI2-conditional
   amd64 assembly for field arithmetic alongside a generic Go fallback for other architectures. If validators
   run mixed hardware (amd64-with-ADX vs amd64-without vs arm64) and the assembly and generic-Go paths have
   EVER disagreed on any input for this specific version (a documented bug class in other optimized
   pairing/BLS libraries historically), that's a direct chain-halting/fork risk. Fix direction: either (a) pin
   a gnark-crypto version specifically audited for ASM/generic-Go output equivalence and document the audit,
   or (b) force the pure-Go build path (build-tag override) across ALL validator builds so every node takes
   the identical arithmetic code path regardless of host CPU, eliminating the divergence class outright by
   construction rather than by trust.
3. **Hand-rolling the Groth16 verification equation from scratch, validated against only ONE golden vector
   from one trivial single-public-input circuit, under-tests the exact bug class most likely to hide there.**
   A wrong term order in the multi-pairing product, a sign error on the negated-A term, or an off-by-one in
   the IC-array indexing for MORE than one public input would not necessarily fail the stated test plan (a
   single-input toy circuit doesn't exercise the accumulation loop meaningfully) — such a bug can silently
   break soundness (accept invalid proofs) while still passing every listed test. Fix direction: either (a)
   vendor or line-by-line diff the hand-rolled equation against gnark's own `backend/groth16/bn254` verify
   implementation as an explicit acceptance gate (not just "inspired by"), or (b) materially strengthen the
   differential test matrix — multi-public-input circuits, proofs generated from >=2 independent toolchains
   (e.g. one from snarkjs, one from gnark's own prover, for the same circuit) — before this is
   implementation-ready.

Also found (majors, must fix before implementation, not blocking the overall direction):
- **The Ethereum gas-anchor citation is factually backwards.** v1 asserts Ethereum's EIP-1108 `ECPAIRING` is
  "34,000-flat + 45,000/pair"; the real, post-Istanbul figure is **45,000 flat + 34,000/pair** — the design
  reproduces a reversed number from its own task framing without independently verifying it, directly
  contradicting its own stated "do not invent numbers from nothing" bar. The proposed formula
  (45,000 + 40,000×(k-1)) happens to land in a similar ballpark by coincidence, not because the citation was
  actually checked. Must be corrected and the formula re-derived against the CORRECT anchor.
- **Field-element canonicalization is only half-solved.** v1 chose uncompressed X‖Y encoding specifically to
  avoid the compressed-encoding Y-sign ambiguity, but never states whether a 32-byte coordinate >= the field
  modulus `p` is rejected or silently reduced mod `p` by the underlying decode call — if silently reduced,
  multiple distinct byte strings map to the same point, contradicting the design's own "canonical encoding"
  claim. Must explicitly require rejecting (soft-fail) any coordinate >= `p`, not silently reducing it.
- **Gas pricing for G2 ops doesn't state whether it already includes the mandatory subgroup-check cost** from
  blocker 1 — a real gap between the security requirement and the itemized cost model that an implementer
  could miss (e.g. adding a "cheap" G2Add that skips the check because the price was set without it in mind).
- **A cited reference file doesn't exist**: v1 cites `sig_wallet.atlx` twice as an existing convention-setting
  example; no such file exists under `examples/avm/` — the real secp256k1-adjacent example is
  `examples/avm/multisig/dual_sig_multisig.atlx`. Fabricated citation in a section explicitly presented as
  "already verified in this codebase" — must be corrected, not just noted.

Minor, worth addressing but not blocking: Poseidon2 (gnark-crypto's default) vs. original Poseidon (what
circom/circomlib-based off-chain mixer tooling conventionally uses) is a real interoperability question left
as a silent default rather than a deliberate call; whether `subBytes` traps or silently short-reads on an
out-of-bounds public-inputs blob is asserted-but-unverified and could either violate the "never trap, soft-fail
to false" convention or silently alter the effective public inputs used in the pairing check.

## Status

**Not started. Do not implement.** v2 must specifically: (a) make G1 `IsOnCurve()` + G2
`IsOnCurve()`+`IsInSubGroup()` an explicit, no-exception opcode-spec requirement, (b) resolve the
cross-architecture ASM/generic-Go determinism question with a concrete pin-and-audit or force-pure-Go
decision, (c) either vendor/diff against gnark's own verifier or substantially deepen the differential test
matrix (multi-input circuits, >=2 independent proof toolchains), (d) correct the Ethereum gas anchor and
re-derive the pairing-check formula from the correct figures, (e) state explicit coordinate-range rejection
for field elements, (f) fix the fabricated file citation, (g) make an explicit Poseidon-vs-Poseidon2 call and
verify `subBytes` out-of-bounds behavior against the intended soft-fail convention.

This is the FIRST round for Phase D (unlike Phase E/F, which is now 3 rounds deep) — a single round finding
real blockers on a first pass is normal and not yet a signal to escalate to human review; proceed with a v2
design-fix pass.
