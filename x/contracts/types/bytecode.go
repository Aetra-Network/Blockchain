package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ValidateAVMBytecode performs the cheap, structural checks on a bytecode
// blob: non-empty, within the governance-configured size ceiling, and
// carrying the AVM1 module header. This is intentionally NOT the full
// verification gate — it cannot decode or verify the module here, since the
// real decoder/verifier lives in x/aetravm/avm, which itself imports this
// package (x/contracts/types) for StateInit/address-derivation support used
// by the AVM's counterfactual-address opcodes; importing avm from here would
// be an import cycle. The real accept/reject decision (avm.DecodeModule +
// Verifier.Verify) is enforced once, at StoreCode time, by
// x/contracts/keeper.storeCodeUnchecked — see FINDING-004. This function
// also still runs on every CodeRecord.Validate invariant re-check (via
// State.Validate), so it must stay cheap; it is deliberately NOT where the
// expensive decode/verify pass belongs.
//
// The previous implementation additionally scanned for a fixed list of
// forbidden ASCII substrings ("time.now", "random", ...) as a heuristic
// stand-in for nondeterminism enforcement. That scan both under-rejected
// (real forbidden opcodes are binary, not ASCII text) and over-rejected
// (legitimate compiled modules can contain these byte sequences by
// coincidence). Real determinism enforcement is avm.Verifier.Verify, which
// rejects forbidden opcodes (OpWallClock/OpRandom/OpFileRead/OpFloatAdd/
// OpIterMap) directly against the decoded instruction stream; the substring
// scan added no coverage beyond that and is removed.
func ValidateAVMBytecode(params Params, bytecode []byte) error {
	if len(bytecode) == 0 {
		return errors.New(ErrInvalidBytecode + ": bytecode is required")
	}
	if uint64(len(bytecode)) > params.MaxCodeBytes {
		return errors.New(ErrInvalidBytecode + ": code size out of bounds")
	}
	if !bytes.HasPrefix(bytecode, []byte("AVM1")) {
		return errors.New(ErrInvalidBytecode + ": unsupported AVM bytecode header")
	}
	return nil
}

func CanonicalCodeHash(bytecode []byte) string {
	sum := sha256.Sum256(append([]byte("aetra-avm-code-v1/"), bytecode...))
	return hex.EncodeToString(sum[:])
}
