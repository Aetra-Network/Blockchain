package avm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpcodeRegistrationSetsAgree guards against a future opcode being added
// to one of Params.Validate()'s required-opcode list, DefaultParams()'s
// GasSchedule, or IsAllowedOpcode()'s accepted switch, but forgotten in
// another -- a partial registration that would otherwise only surface as a
// runtime panic/mispricing/rejection surprise instead of a build-time (well,
// test-time) failure. All three must describe exactly the same set of
// opcodes.
//
// Approach: scan the whole byte range for opcodes IsAllowedOpcode accepts
// (that's the ground truth for "this is a real, executable opcode"), then
// cross-check each one is present with a positive cost in DefaultParams()'s
// GasSchedule, AND that Validate() actually enforces its presence (by
// deleting it from a copy of the gas schedule and confirming Validate()
// rejects the result -- this is the only way to probe Validate()'s internal,
// unexported required-opcode list from outside the function).
func TestOpcodeRegistrationSetsAgree(t *testing.T) {
	var allowed []Opcode
	for b := 0; b <= 0xff; b++ {
		op := Opcode(byte(b))
		if IsAllowedOpcode(op) {
			allowed = append(allowed, op)
		}
	}
	require.NotEmpty(t, allowed, "IsAllowedOpcode must accept at least one opcode")

	base := DefaultParams()
	require.NoError(t, base.Validate(), "DefaultParams() must be valid as a baseline")

	gasKeys := make(map[Opcode]bool, len(base.GasSchedule))
	for op := range base.GasSchedule {
		gasKeys[op] = true
	}

	allowedSet := make(map[Opcode]bool, len(allowed))
	for _, op := range allowed {
		allowedSet[op] = true
	}

	// 1) Every opcode IsAllowedOpcode() accepts must have a positive entry in
	// DefaultParams().GasSchedule -- an allowed-but-unpriced opcode would be
	// free to execute (and dispatchable, per IsAllowedOpcode/Dispatch), a gas
	// accounting hole.
	for _, op := range allowed {
		cost, ok := base.GasSchedule[op]
		if !ok || cost == 0 {
			t.Errorf("opcode 0x%02x is accepted by IsAllowedOpcode but has no positive DefaultParams().GasSchedule entry", byte(op))
		}
	}

	// 2) Every opcode with a GasSchedule entry must be accepted by
	// IsAllowedOpcode -- a priced-but-disallowed opcode is dead gas-schedule
	// weight that masks a forgotten IsAllowedOpcode case (or the reverse: an
	// opcode wrongly left dispatchable-but-rejected).
	for op := range gasKeys {
		if !allowedSet[op] {
			t.Errorf("opcode 0x%02x has a DefaultParams().GasSchedule entry but is not accepted by IsAllowedOpcode", byte(op))
		}
	}

	// 3) Every opcode IsAllowedOpcode() accepts must also be enforced as
	// REQUIRED by Params.Validate() -- i.e. removing its GasSchedule entry
	// must make an otherwise-valid Params invalid. This is the guard against
	// an opcode being added to the allowed/priced sets but forgotten in
	// Validate()'s required-opcode list, which would let a governance-
	// supplied Params silently zero its cost.
	for _, op := range allowed {
		mutated := DefaultParams()
		gasSchedule := make(map[Opcode]uint64, len(base.GasSchedule))
		for k, v := range base.GasSchedule {
			gasSchedule[k] = v
		}
		delete(gasSchedule, op)
		mutated.GasSchedule = gasSchedule

		if err := mutated.Validate(); err == nil {
			t.Errorf("opcode 0x%02x is accepted by IsAllowedOpcode but Params.Validate() does not require it to be present in GasSchedule (removing it still validates)", byte(op))
		}
	}
}
