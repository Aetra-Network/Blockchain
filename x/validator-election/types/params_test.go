package types

import (
	"strings"
	"testing"

	validatorregistrytypes "github.com/sovereign-l1/l1/x/validator-registry/types"
)

// TestParamsValidateBoundsTotalVotingPowerToCometBFTCeiling covers SA2-S04:
// governance must not be able to set a voting-power ceiling above CometBFT's
// MaxTotalVotingPower (math.MaxInt64/8). Without the bound, once enough
// validators are elected to sum past that limit, FinalizeBlock returns the
// override error fatally and the chain halts permanently.
func TestParamsValidateBoundsTotalVotingPowerToCometBFTCeiling(t *testing.T) {
	if err := DefaultParams().Validate(); err != nil {
		t.Fatalf("default params must validate, got %v", err)
	}

	// One above the ceiling is rejected.
	p := DefaultParams()
	p.MaxTotalVotingPower = MaxAllowedTotalVotingPower + 1
	if err := p.Validate(); err == nil {
		t.Fatalf("expected Validate to reject MaxTotalVotingPower above the CometBFT ceiling %d", MaxAllowedTotalVotingPower)
	}

	// Exactly at the ceiling is allowed.
	p = DefaultParams()
	p.MaxTotalVotingPower = MaxAllowedTotalVotingPower
	if err := p.Validate(); err != nil {
		t.Fatalf("expected Validate to accept MaxTotalVotingPower at the ceiling, got %v", err)
	}

	// MaxValidatorPower is transitively bounded: a per-validator ceiling above
	// the total (which itself is above the CometBFT limit) is rejected too.
	p = DefaultParams()
	p.MaxValidatorPower = MaxAllowedTotalVotingPower + 1
	p.MaxTotalVotingPower = MaxAllowedTotalVotingPower + 1
	if err := p.Validate(); err == nil {
		t.Fatalf("expected Validate to reject MaxValidatorPower above the CometBFT ceiling")
	}
}

// TestValidateValidatorSetRejectsDuplicateConsensusKey covers SA2-S05: two
// distinct operators must not carry the same consensus pubkey, or the CometBFT
// override (keyed by pubkey) collapses them and desyncs the recorded set from
// the live set.
func TestValidateValidatorSetRejectsDuplicateConsensusKey(t *testing.T) {
	const (
		opA = "ae1vpckp78tsyp2w5u6za0lazsqljsyx83cc0ha7n"
		opB = "ae1vw59zucmqxwa6lcf2td6jl3jxnpkw3h3m8vcdp"
	)
	sharedKey := "ed25519:" + strings.Repeat("ab", 32) // 32-byte hex key
	otherKey := "ed25519:" + strings.Repeat("cd", 32)
	params := DefaultParams()

	dup := SortValidatorSet([]ValidatorPower{
		{OperatorAddress: opA, ConsensusPublicKey: sharedKey, VotingPower: 7, ValidatorStatus: validatorregistrytypes.StatusActive},
		{OperatorAddress: opB, ConsensusPublicKey: sharedKey, VotingPower: 3, ValidatorStatus: validatorregistrytypes.StatusActive},
	})
	if err := validateValidatorSet("current", dup, params, false); err == nil || !strings.Contains(err.Error(), "duplicate consensus key") {
		t.Fatalf("expected a duplicate-consensus-key rejection, got %v", err)
	}

	// Same operators with distinct consensus keys validate cleanly.
	distinct := SortValidatorSet([]ValidatorPower{
		{OperatorAddress: opA, ConsensusPublicKey: sharedKey, VotingPower: 7, ValidatorStatus: validatorregistrytypes.StatusActive},
		{OperatorAddress: opB, ConsensusPublicKey: otherKey, VotingPower: 3, ValidatorStatus: validatorregistrytypes.StatusActive},
	})
	if err := validateValidatorSet("current", distinct, params, false); err != nil {
		t.Fatalf("distinct valid entries must validate, got %v", err)
	}
}
