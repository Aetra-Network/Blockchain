package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityAttestationValidateAndIDStableAcrossRevocationFields(t *testing.T) {
	contract := contractAPIAddress(0x31)
	raw, err := RawAddressForUserAddress(contract)
	require.NoError(t, err)

	attestation := ContractSecurityAttestation{
		ContractAddressUser: contract,
		ContractAddressRaw:  raw,
		Source:              "ci-scan",
		SourceURL:           "https://ci.example/security/scan/123",
		CommitHash:          "abc123",
		CodeHash:            "def456",
		EvidenceHash:        "789abc",
		CheckedHeight:       42,
		UpdatedHeight:       43,
		RiskScoreBps:        1500,
		Categories:          []string{SecurityAttestationCategoryOpenSourceVerified},
		Flags:               []string{"signed-build"},
		RelatedAddresses:    []string{contractAPIAddress(0x32)},
		GraphEdges: []SecurityGraphEdge{
			{From: contract, To: contractAPIAddress(0x33), Relation: "phishing-linked"},
		},
		SignedBy: "AEsigner",
	}
	attestation.AttestationID = ComputeSecurityAttestationID(attestation)
	require.NoError(t, attestation.Validate())

	revoked := attestation
	revoked.Status = SecurityAttestationStatusRevoked
	revoked.RevokedReason = "replaced by newer scan"
	revoked.UpdatedHeight = 44
	require.Equal(t, attestation.AttestationID, ComputeSecurityAttestationID(revoked))
	require.NoError(t, revoked.Validate())
}

func TestSecurityBadgeAggregatesRiskAndVerification(t *testing.T) {
	contract := contractAPIAddress(0x41)
	activeVerified := ContractSecurityAttestation{
		AttestationID:       "att-1",
		ContractAddressUser: contract,
		ContractAddressRaw:  mustRawAddress(t, contract),
		Source:              "ci-scan",
		SourceURL:           "https://ci.example/security/scan/1",
		CheckedHeight:       10,
		UpdatedHeight:       11,
		RiskScoreBps:        500,
		Categories:          []string{SecurityAttestationCategoryOpenSourceVerified},
	}
	activeVerified.AttestationID = ComputeSecurityAttestationID(activeVerified)

	activeRisky := ContractSecurityAttestation{
		AttestationID:       "att-2",
		ContractAddressUser: contract,
		ContractAddressRaw:  mustRawAddress(t, contract),
		Source:              "scan",
		SourceURL:           "https://ci.example/security/scan/2",
		CheckedHeight:       12,
		UpdatedHeight:       13,
		RiskScoreBps:        9000,
		Categories:          []string{SecurityAttestationCategoryPhishingLinked},
		Flags:               []string{"linked-to-phishing-graph"},
		RelatedAddresses:    []string{contractAPIAddress(0x42)},
		GraphEdges: []SecurityGraphEdge{
			{From: contract, To: contractAPIAddress(0x42), Relation: "phishing-linked"},
		},
	}
	activeRisky.AttestationID = ComputeSecurityAttestationID(activeRisky)

	revoked := activeRisky
	revoked.AttestationID = "att-3"
	revoked.Status = SecurityAttestationStatusRevoked
	revoked.RevokedReason = "false positive"
	revoked.UpdatedHeight = 14

	badge := ComputeSecurityBadge([]ContractSecurityAttestation{activeVerified, activeRisky, revoked}, contract)
	require.Equal(t, contract, badge.ContractAddress)
	require.Equal(t, SecurityBadgeCritical, badge.Badge)
	require.True(t, badge.Verified)
	require.Equal(t, uint32(3), badge.AttestationCount)
	require.Equal(t, uint32(2), badge.ActiveAttestationCount)
	require.Equal(t, uint32(1), badge.RevokedAttestationCount)
	require.Contains(t, badge.Categories, SecurityAttestationCategoryOpenSourceVerified)
	require.Contains(t, badge.Categories, SecurityAttestationCategoryPhishingLinked)
	require.Contains(t, badge.Flags, "linked-to-phishing-graph")
	require.True(t, badge.RiskScoreBps >= 9000)
	require.NotEmpty(t, badge.GraphEdges)
}

func mustRawAddress(t *testing.T, user string) string {
	t.Helper()
	raw, err := RawAddressForUserAddress(user)
	require.NoError(t, err)
	return raw
}
