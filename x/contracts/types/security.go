package types

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	SecurityAttestationStatusActive  = "active"
	SecurityAttestationStatusRevoked = "revoked"

	SecurityAttestationCategoryOpenSourceVerified       = "open_source_verified"
	SecurityAttestationCategorySimulatedExploitBehavior = "simulated_exploit_behavior"
	SecurityAttestationCategoryPermissionAbuse          = "permission_abuse"
	SecurityAttestationCategoryPhishingLinked           = "phishing_linked"
	SecurityAttestationCategoryHoneypotLikeBehavior     = "honeypot_like_behavior"

	SecurityBadgeVerified   = "verified"
	SecurityBadgeReview     = "review"
	SecurityBadgeHighRisk   = "high-risk"
	SecurityBadgeCritical   = "critical"
	SecurityBadgeUnattested = "unattested"
)

type SecurityGraphEdge struct {
	From     string
	To       string
	Relation string
}

type ContractSecurityAttestation struct {
	AttestationID       string
	ContractAddressUser string
	ContractAddressRaw  string
	Source              string
	SourceURL           string
	CommitHash          string
	CodeHash            string
	EvidenceHash        string
	CheckedHeight       uint64
	UpdatedHeight       uint64
	RiskScoreBps        uint32
	Categories          []string
	Flags               []string
	RelatedAddresses    []string
	GraphEdges          []SecurityGraphEdge
	Status              string
	RevokedReason       string
	SignedBy            string
}

type ContractSecurityBadge struct {
	ContractAddress         string
	Badge                   string
	Verified                bool
	RiskScoreBps            uint32
	Categories              []string
	Flags                   []string
	RelatedAddresses        []string
	GraphEdges              []SecurityGraphEdge
	AttestationCount        uint32
	ActiveAttestationCount  uint32
	RevokedAttestationCount uint32
	LatestUpdatedHeight     uint64
	AttestationIDs          []string
}

type MsgSubmitSecurityAttestation struct {
	Authority   string
	Attestation ContractSecurityAttestation
}

type MsgSubmitSecurityAttestationResponse struct {
	Attestation ContractSecurityAttestation
	StateRoot   string
}

type MsgRevokeSecurityAttestation struct {
	Authority     string
	AttestationID string
	RevokedReason string
	Height        uint64
}

type MsgRevokeSecurityAttestationResponse struct {
	Attestation ContractSecurityAttestation
	StateRoot   string
}

type QuerySecurityAttestationsRequest struct {
	ContractAddress string
	IncludeRevoked  bool
	Pagination      PageRequest
}

type QuerySecurityAttestationsResponse struct {
	Attestations []ContractSecurityAttestation
}

type QuerySecurityBadgeRequest struct {
	ContractAddress string
}

type QuerySecurityBadgeResponse struct {
	Badge ContractSecurityBadge
	Found bool
}

func (a ContractSecurityAttestation) Normalize() ContractSecurityAttestation {
	out := a
	out.ContractAddressUser = strings.TrimSpace(out.ContractAddressUser)
	out.ContractAddressRaw = strings.TrimSpace(out.ContractAddressRaw)
	out.Source = strings.TrimSpace(out.Source)
	out.SourceURL = strings.TrimSpace(out.SourceURL)
	out.CommitHash = strings.TrimSpace(strings.ToLower(out.CommitHash))
	out.CodeHash = strings.TrimSpace(strings.ToLower(out.CodeHash))
	out.EvidenceHash = strings.TrimSpace(strings.ToLower(out.EvidenceHash))
	out.RevokedReason = strings.TrimSpace(out.RevokedReason)
	out.SignedBy = strings.TrimSpace(out.SignedBy)
	out.Status = normalizeSecurityAttestationStatus(out.Status)
	out.Categories = normalizeUniqueStrings(out.Categories)
	out.Flags = normalizeUniqueStrings(out.Flags)
	out.RelatedAddresses = normalizeUniqueStrings(out.RelatedAddresses)
	sort.SliceStable(out.GraphEdges, func(i, j int) bool {
		if out.GraphEdges[i].From != out.GraphEdges[j].From {
			return out.GraphEdges[i].From < out.GraphEdges[j].From
		}
		if out.GraphEdges[i].To != out.GraphEdges[j].To {
			return out.GraphEdges[i].To < out.GraphEdges[j].To
		}
		return out.GraphEdges[i].Relation < out.GraphEdges[j].Relation
	})
	for i := range out.GraphEdges {
		out.GraphEdges[i].From = strings.TrimSpace(out.GraphEdges[i].From)
		out.GraphEdges[i].To = strings.TrimSpace(out.GraphEdges[i].To)
		out.GraphEdges[i].Relation = strings.TrimSpace(out.GraphEdges[i].Relation)
	}
	return out
}

func (a ContractSecurityAttestation) Validate() error {
	if err := ValidateUserFacingAEAddress("security attestation contract address", a.ContractAddressUser); err != nil {
		return err
	}
	if err := ValidateRawAddress("security attestation raw contract address", a.ContractAddressRaw); err != nil {
		return err
	}
	if err := ValidateAddressPair("security attestation address pair", a.ContractAddressUser, a.ContractAddressRaw); err != nil {
		return err
	}
	if strings.TrimSpace(a.Source) == "" {
		return errors.New("security attestation source is required")
	}
	if a.CheckedHeight == 0 {
		return errors.New("security attestation checked height is required")
	}
	if a.UpdatedHeight == 0 {
		return errors.New("security attestation updated height is required")
	}
	if a.RiskScoreBps > 10_000 {
		return errors.New("security attestation risk score is invalid")
	}
	if len(a.Categories) == 0 {
		return errors.New("security attestation categories are required")
	}
	for _, category := range a.Categories {
		if !isSecurityAttestationCategory(category) {
			return fmt.Errorf("unsupported security attestation category %q", category)
		}
	}
	if a.Status == "" {
		a.Status = SecurityAttestationStatusActive
	}
	if a.Status != SecurityAttestationStatusActive && a.Status != SecurityAttestationStatusRevoked {
		return fmt.Errorf("unsupported security attestation status %q", a.Status)
	}
	if a.Status == SecurityAttestationStatusRevoked && strings.TrimSpace(a.RevokedReason) == "" {
		return errors.New("revoked security attestation reason is required")
	}
	for _, edge := range a.GraphEdges {
		if strings.TrimSpace(edge.From) == "" || strings.TrimSpace(edge.To) == "" || strings.TrimSpace(edge.Relation) == "" {
			return errors.New("security attestation scam graph edges require from, to, and relation")
		}
	}
	expected := ComputeSecurityAttestationID(a)
	if strings.TrimSpace(a.AttestationID) == "" {
		return errors.New("security attestation id is required")
	}
	if a.AttestationID != expected {
		return errors.New("security attestation id mismatch")
	}
	return nil
}

func (b ContractSecurityBadge) Validate() error {
	if err := ValidateUserFacingAEAddress("security badge contract address", b.ContractAddress); err != nil {
		return err
	}
	if b.Badge == "" {
		return errors.New("security badge classification is required")
	}
	if b.RiskScoreBps > 10_000 {
		return errors.New("security badge risk score is invalid")
	}
	return nil
}

func ComputeSecurityAttestationID(att ContractSecurityAttestation) string {
	att = att.Normalize()
	att.AttestationID = ""
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"contracts-security-attestation-v1/%s/%s/%s/%s/%s/%s/%s/%020d/%05d/%s/%s/%s/%s/%s",
		att.ContractAddressUser,
		att.ContractAddressRaw,
		att.Source,
		att.SourceURL,
		att.CommitHash,
		att.CodeHash,
		att.EvidenceHash,
		att.CheckedHeight,
		att.RiskScoreBps,
		strings.Join(att.Categories, ","),
		strings.Join(att.Flags, ","),
		strings.Join(att.RelatedAddresses, ","),
		graphEdgeFingerprint(att.GraphEdges),
		att.SignedBy,
	)))
	return hex.EncodeToString(sum[:])
}

func ComputeSecurityBadge(attestations []ContractSecurityAttestation, contractAddress string) ContractSecurityBadge {
	contractAddress = strings.TrimSpace(contractAddress)
	badge := ContractSecurityBadge{ContractAddress: contractAddress}
	if contractAddress == "" {
		return badge
	}
	var active []ContractSecurityAttestation
	for _, att := range attestations {
		if att.ContractAddressUser != contractAddress {
			continue
		}
		badge.AttestationCount++
		badge.AttestationIDs = append(badge.AttestationIDs, att.AttestationID)
		if att.Status == SecurityAttestationStatusRevoked {
			badge.RevokedAttestationCount++
			continue
		}
		badge.ActiveAttestationCount++
		active = append(active, att)
		if att.UpdatedHeight > badge.LatestUpdatedHeight {
			badge.LatestUpdatedHeight = att.UpdatedHeight
		}
		if att.RiskScoreBps > badge.RiskScoreBps {
			badge.RiskScoreBps = att.RiskScoreBps
		}
		badge.Categories = append(badge.Categories, att.Categories...)
		badge.Flags = append(badge.Flags, att.Flags...)
		badge.RelatedAddresses = append(badge.RelatedAddresses, att.RelatedAddresses...)
		badge.GraphEdges = append(badge.GraphEdges, att.GraphEdges...)
		if containsSecurityCategory(att.Categories, SecurityAttestationCategoryOpenSourceVerified) && len(att.Flags) == 0 && att.RiskScoreBps <= 1_000 {
			badge.Verified = true
		}
	}
	badge.Categories = normalizeUniqueStrings(badge.Categories)
	badge.Flags = normalizeUniqueStrings(badge.Flags)
	badge.RelatedAddresses = normalizeUniqueStrings(badge.RelatedAddresses)
	badge.GraphEdges = normalizeSecurityGraphEdges(badge.GraphEdges)
	badge.AttestationIDs = normalizeUniqueStrings(badge.AttestationIDs)
	switch {
	case badge.ActiveAttestationCount == 0:
		badge.Badge = SecurityBadgeUnattested
	case badge.RiskScoreBps >= 8_000 || containsSecurityCategory(badge.Categories, SecurityAttestationCategoryPhishingLinked) || containsSecurityCategory(badge.Categories, SecurityAttestationCategoryHoneypotLikeBehavior):
		badge.Badge = SecurityBadgeCritical
	case badge.RiskScoreBps >= 5_000 || containsSecurityCategory(badge.Categories, SecurityAttestationCategorySimulatedExploitBehavior) || containsSecurityCategory(badge.Categories, SecurityAttestationCategoryPermissionAbuse):
		badge.Badge = SecurityBadgeHighRisk
	case badge.Verified:
		badge.Badge = SecurityBadgeVerified
	default:
		badge.Badge = SecurityBadgeReview
	}
	_ = active
	return badge
}

func normalizeSecurityAttestationStatus(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return SecurityAttestationStatusActive
	}
	return status
}

func isSecurityAttestationCategory(category string) bool {
	switch strings.TrimSpace(category) {
	case SecurityAttestationCategoryOpenSourceVerified,
		SecurityAttestationCategorySimulatedExploitBehavior,
		SecurityAttestationCategoryPermissionAbuse,
		SecurityAttestationCategoryPhishingLinked,
		SecurityAttestationCategoryHoneypotLikeBehavior:
		return true
	default:
		return false
	}
}

func normalizeUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeSecurityGraphEdges(edges []SecurityGraphEdge) []SecurityGraphEdge {
	out := append([]SecurityGraphEdge(nil), edges...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].Relation < out[j].Relation
	})
	return out
}

func graphEdgeFingerprint(edges []SecurityGraphEdge) string {
	if len(edges) == 0 {
		return ""
	}
	parts := make([]string, 0, len(edges))
	for _, edge := range normalizeSecurityGraphEdges(edges) {
		parts = append(parts, edge.From+"->"+edge.To+":"+edge.Relation)
	}
	return strings.Join(parts, "|")
}

func containsSecurityCategory(categories []string, category string) bool {
	for _, item := range categories {
		if item == category {
			return true
		}
	}
	return false
}

func (m MsgSubmitSecurityAttestation) ValidateBasic(params Params) error {
	if err := params.Authorize(m.Authority); err != nil {
		return err
	}
	att := m.Attestation.Normalize()
	if att.AttestationID == "" {
		att.AttestationID = ComputeSecurityAttestationID(att)
	}
	return att.Validate()
}

func (m MsgRevokeSecurityAttestation) ValidateBasic(params Params) error {
	if err := params.Authorize(m.Authority); err != nil {
		return err
	}
	if strings.TrimSpace(m.AttestationID) == "" {
		return errors.New("security attestation id is required")
	}
	if m.Height == 0 {
		return errors.New("security attestation revoke height is required")
	}
	return nil
}
