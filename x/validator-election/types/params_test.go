package types

import "testing"

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
