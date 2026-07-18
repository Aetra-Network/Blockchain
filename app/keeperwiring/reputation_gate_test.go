package keeperwiring

import (
	"bytes"
	"context"
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// White-box tests for the ANS Phase B reputation fee gate. The adapter must
// return found=false (neutral -- no fee scaling) for a plain wallet regardless
// of its score, and found=true (engage the multiplicative scaling) only for a
// wallet that currently holds a domain OR is a validator. A reputation read
// error must degrade to neutral, never block.

type fakeScorer struct {
	score	uint32
	found	bool
	err	error
}

func (f fakeScorer) GetIdentityReputationScore(context.Context, sdk.AccAddress) (uint32, bool, error) {
	return f.score, f.found, f.err
}

type fakeDomainReader struct {
	holds	bool
	err	error
}

func (f fakeDomainReader) AccountHoldsDomain(context.Context, sdk.AccAddress) (bool, error) {
	return f.holds, f.err
}

type fakeValidatorReader struct{ isValidator bool }

func (f fakeValidatorReader) IsValidator(context.Context, sdk.AccAddress) bool {
	return f.isValidator
}

var gateAddr = sdk.AccAddress(bytes.Repeat([]byte{0x42}, 20))

func TestReputationGateNeutralForPlainWallet(t *testing.T) {
	a := reputationReaderAdapter{
		scorer:		fakeScorer{score: 700, found: true},
		domainReader:	fakeDomainReader{holds: false},
		validatorReader:	fakeValidatorReader{isValidator: false},
	}
	score, found, err := a.GetIdentityReputationScore(context.Background(), gateAddr)
	require.NoError(t, err)
	require.False(t, found, "a wallet with neither a domain nor validator status must be un-gated")
	require.Equal(t, uint32(700), score)
}

func TestReputationGateEngagesForDomainHolder(t *testing.T) {
	a := reputationReaderAdapter{
		scorer:		fakeScorer{score: 700, found: true},
		domainReader:	fakeDomainReader{holds: true},
		validatorReader:	fakeValidatorReader{isValidator: false},
	}
	score, found, err := a.GetIdentityReputationScore(context.Background(), gateAddr)
	require.NoError(t, err)
	require.True(t, found, "a domain holder must be gated (reputation scaling engaged)")
	require.Equal(t, uint32(700), score)
}

func TestReputationGateEngagesForValidator(t *testing.T) {
	a := reputationReaderAdapter{
		scorer:		fakeScorer{score: 500, found: true},
		domainReader:	fakeDomainReader{holds: false},
		validatorReader:	fakeValidatorReader{isValidator: true},
	}
	_, found, err := a.GetIdentityReputationScore(context.Background(), gateAddr)
	require.NoError(t, err)
	require.True(t, found, "a validator must be gated even without a domain")
}

func TestReputationGateEngagesForFreshDomainHolderWithNoRecord(t *testing.T) {
	// A freshly-attached wallet has no reputation record yet: the scorer reports
	// found=false with the module default score. The gate must still engage so
	// attaching a domain grants the multiplier immediately.
	a := reputationReaderAdapter{
		scorer:		fakeScorer{score: 100, found: false},
		domainReader:	fakeDomainReader{holds: true},
		validatorReader:	fakeValidatorReader{isValidator: false},
	}
	score, found, err := a.GetIdentityReputationScore(context.Background(), gateAddr)
	require.NoError(t, err)
	require.True(t, found, "a domain holder must be gated even with no reputation record yet")
	require.Equal(t, uint32(100), score)
}

func TestReputationGateDegradesOnScorerError(t *testing.T) {
	a := reputationReaderAdapter{
		scorer:		fakeScorer{score: 100, found: false, err: errors.New("store boom")},
		domainReader:	fakeDomainReader{holds: true},
		validatorReader:	fakeValidatorReader{isValidator: true},
	}
	_, found, err := a.GetIdentityReputationScore(context.Background(), gateAddr)
	require.NoError(t, err, "a reputation read error must never propagate to block the tx")
	require.False(t, found, "on a scorer error the sender is treated as un-gated (neutral)")
}

func TestReputationGateDomainReaderErrorFallsThroughToValidator(t *testing.T) {
	// A domain-read error must not gate on its own, but a validator can still
	// gate the sender.
	a := reputationReaderAdapter{
		scorer:		fakeScorer{score: 300, found: true},
		domainReader:	fakeDomainReader{err: errors.New("domain read boom")},
		validatorReader:	fakeValidatorReader{isValidator: true},
	}
	_, found, err := a.GetIdentityReputationScore(context.Background(), gateAddr)
	require.NoError(t, err)
	require.True(t, found, "a validator is gated even when the domain read errors")

	a.validatorReader = fakeValidatorReader{isValidator: false}
	_, found, err = a.GetIdentityReputationScore(context.Background(), gateAddr)
	require.NoError(t, err)
	require.False(t, found, "a domain read error alone must not gate the sender")
}

func TestReputationGateNilReadersAreNeutral(t *testing.T) {
	a := reputationReaderAdapter{scorer: fakeScorer{score: 900, found: true}}
	_, found, err := a.GetIdentityReputationScore(context.Background(), gateAddr)
	require.NoError(t, err)
	require.False(t, found, "with no domain/validator readers wired the sender is un-gated")
}
