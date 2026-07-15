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

// TestValidateValidatorSetTotalPowerCheckIsUnderflowSafe covers SA2-I02: under
// misconfigured params where a per-validator power exceeds the total cap, an
// oversized validator must be rejected, not silently admitted via a uint64
// underflow of (MaxTotalVotingPower - VotingPower).
func TestValidateValidatorSetTotalPowerCheckIsUnderflowSafe(t *testing.T) {
	params := DefaultParams()
	params.MaxValidatorPower = 1000 // per-validator cap above the total cap (misconfig)
	params.MaxTotalVotingPower = 100
	set := SortValidatorSet([]ValidatorPower{
		{OperatorAddress: "ae1vpckp78tsyp2w5u6za0lazsqljsyx83cc0ha7n", ConsensusPublicKey: "ed25519:" + strings.Repeat("ab", 32), VotingPower: 500, ValidatorStatus: validatorregistrytypes.StatusActive},
	})
	if err := validateValidatorSet("current", set, params, false); err == nil {
		t.Fatalf("expected the oversized validator to be rejected (underflow-safe), got nil")
	}
}

// TestNormalizeBoundsUnboundedHistorySlices covers SA2-S02: the epoch-keyed
// history slices must be trimmed to a window (they grow +1/epoch forever), and
// released frozen stakes must be dropped, so per-block load+validate cost does
// not creep toward the block timeout on a long-running chain.
func TestNormalizeBoundsUnboundedHistorySlices(t *testing.T) {
	params := DefaultParams()
	var s State
	overfill := int(MaxElectionResultsV1) + 50
	for i := 1; i <= overfill; i++ {
		s.ElectionResults = append(s.ElectionResults, ElectionResult{Epoch: uint64(i), Height: uint64(i), Committed: true})
		s.RewardDistributionSnapshots = append(s.RewardDistributionSnapshots, RewardDistributionSnapshot{Epoch: uint64(i), Height: uint64(i)})
	}
	s.FrozenStakes = []FrozenStake{
		{OperatorAddress: "op", Amount: 1, FrozenAtHeight: 1, UnlockHeight: 2, Released: true},
		{OperatorAddress: "op", Amount: 1, FrozenAtHeight: 1, UnlockHeight: 3, Released: false},
	}

	out := s.Normalize(params)

	if uint32(len(out.ElectionResults)) != MaxElectionResultsV1 {
		t.Fatalf("ElectionResults not bounded: got %d want %d", len(out.ElectionResults), MaxElectionResultsV1)
	}
	if uint32(len(out.RewardDistributionSnapshots)) != MaxRewardSnapshotsV1 {
		t.Fatalf("RewardDistributionSnapshots not bounded: got %d want %d", len(out.RewardDistributionSnapshots), MaxRewardSnapshotsV1)
	}
	// The most recent epoch (the tail) must be retained, not an old one.
	if last := out.ElectionResults[len(out.ElectionResults)-1]; last.Epoch != uint64(overfill) {
		t.Fatalf("expected the most-recent epoch retained, got %d", last.Epoch)
	}
	if len(out.FrozenStakes) != 1 || out.FrozenStakes[0].Released {
		t.Fatalf("released frozen stake should be dropped and the unreleased kept, got %+v", out.FrozenStakes)
	}
}
