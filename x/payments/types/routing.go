package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

type RoutePolicy struct {
	MaxHops                 int
	RequiredTimeoutMargin   uint64
	StaleLiquidityAfter     uint64
	HopPenalty              string
	CongestionPenalty       string
	StaleLiquidityPenalty   string
	FailurePenalty          string
	TimeoutPenalty          string
	SuccessPenalty          string
	AvailabilityPenalty     string
	ReservePressurePenalty  string
	QueueDelayPenalty       string
	PendingConditionPenalty string
	LatencyPenalty          string
	ProportionalFeeBps      uint32
	DecayHalfLife           uint64
	MaxCongestedPaymentBps  uint32
	MaxFeeAmount            string
	EnableMultiPath         bool
	MaxSplits               int
	ExcludedNodes           []string
	ExcludedChannels        []string
	EdgeStats               []EdgeRoutingStats
}

type RoutingFeePolicyUpdate struct {
	PolicyID                string
	ChainID                 string
	ChannelID               string
	From                    string
	To                      string
	FeeDenom                string
	BaseHopFee              string
	ProportionalFeeBps      uint32
	LiquidityReservationFee string
	VirtualChannelSetupFee  string
	CongestionSurcharge     string
	FailurePenalty          string
	MaxHopFee               string
	ValidAfterHeight        uint64
	ValidUntilHeight        uint64
	Sequence                uint64
	PolicyHash              string
	Signature               RoutingFeePolicySignature
}

type RoutingFeePolicySignature struct {
	Signer           string
	ChainID          string
	ChannelID        string
	ObjectType       string
	Version          uint32
	Sequence         uint64
	ObjectID         string
	ExpirationHeight uint64
	CommitmentHash   string
	SignatureHash    string
}

type HopFeeCalculationRequest struct {
	Amount                  string
	Policy                  RoutingFeePolicyUpdate
	CurrentHeight           uint64
	IncludeVirtualSetup     bool
	RepeatedInvalidAttempts uint32
}

type RoutingHopFee struct {
	Denom                   string
	BaseHopFee              string
	ProportionalFee         string
	LiquidityReservationFee string
	VirtualChannelSetupFee  string
	CongestionSurcharge     string
	FailurePenalty          string
	RepeatedInvalidAttempts uint32
	TotalFee                string
	PolicyHash              string
}

type RouteFailureClass string

type RouteFailureReport struct {
	ChannelID      string
	From           string
	To             string
	FailureClass   RouteFailureClass
	Retryable      bool
	ObservedHeight uint64
}

type CongestionSnapshot struct {
	ChannelID                   string
	From                        string
	To                          string
	ChannelUpdateFailureRateBps uint32
	PendingConditionCount       uint32
	AvgResolutionLatency        uint64
	RouteRetryCount             uint32
	ReservePressureBps          uint32
	NodeQueueDelay              uint64
	LiquidityUpdatedHeight      uint64
	ObservedHeight              uint64
}

type RouteSelectionRequest struct {
	From          string
	To            string
	Amount        string
	CurrentHeight uint64
	Policy        RoutePolicy
}

type RouteRetryPolicy struct {
	MaxAttempts          uint32
	AlternateRouteLimit  uint32
	ExcludeFailedEdges   bool
	CongestionRetryDelay uint64
}

type RouteRetryRequest struct {
	Selection RouteSelectionRequest
	Failures  []RouteFailureReport
	Policy    RouteRetryPolicy
}

type RouteRetryResult struct {
	Route      ScoredRoute
	Attempts   uint32
	Retryable  bool
	Reason     string
	PolicyHash string
}

type ScoredRoute struct {
	Edges       []ChannelEdge
	Amount      string
	TotalFee    string
	TotalCost   string
	MinCapacity string
	ScoreHash   string
}

type RouteSimulationResult struct {
	Route       ScoredRoute
	Attemptable bool
	Reason      string
	TotalFee    string
}

type MultiPathRoute struct {
	Parts       []ScoredRoute
	TotalAmount string
	TotalFee    string
	ScoreHash   string
}

type ForwardingPacket struct {
	PacketID       string
	RouteID        string
	HopPaymentID   string
	ChannelID      string
	ForwardingNode string
	NextNode       string
	Amount         string
	FeeAmount      string
	TimeoutHeight  uint64
	NextPacketHash string
	PacketHash     string
}

type ForwardingPacketReplayRecord struct {
	PacketID       string
	RouteID        string
	HopPaymentID   string
	RecordedHeight uint64
	ExpiresHeight  uint64
}

type ForwardingLogRecord struct {
	PacketID       string
	RouteID        string
	HopPaymentID   string
	ChannelID      string
	ForwardingNode string
	NextNodeHash   string
	AmountHash     string
	RecordedHeight uint64
}

type RouteFeeClaim struct {
	ChannelID    string
	PromiseID    string
	Recipient    string
	Amount       string
	EvidenceHash string
}

func (r RouteFailureReport) Normalize() RouteFailureReport {
	r.ChannelID = normalizeHash(r.ChannelID)
	r.From = strings.TrimSpace(r.From)
	r.To = strings.TrimSpace(r.To)
	return r
}

func (r RouteFailureReport) Validate() error {
	report := r.Normalize()
	if err := ValidateHash("payments route failure channel id", report.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments route failure from", report.From); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments route failure to", report.To); err != nil {
		return err
	}
	if !IsRouteFailureClass(report.FailureClass) {
		return errors.New("payments route failure class is invalid")
	}
	if report.ObservedHeight == 0 {
		return errors.New("payments route failure observed height must be positive")
	}
	return nil
}

func (s CongestionSnapshot) Normalize() CongestionSnapshot {
	s.ChannelID = normalizeHash(s.ChannelID)
	s.From = strings.TrimSpace(s.From)
	s.To = strings.TrimSpace(s.To)
	return s
}

func (s CongestionSnapshot) Validate() error {
	snapshot := s.Normalize()
	if err := ValidateHash("payments congestion channel id", snapshot.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments congestion from", snapshot.From); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments congestion to", snapshot.To); err != nil {
		return err
	}
	if snapshot.ChannelUpdateFailureRateBps > 10_000 || snapshot.ReservePressureBps > 10_000 {
		return errors.New("payments congestion bps must be <= 10000")
	}
	if snapshot.ObservedHeight == 0 {
		return errors.New("payments congestion observed height must be positive")
	}
	return nil
}

func DefaultRoutePolicy() RoutePolicy {
	return RoutePolicy{
		MaxHops:                 MaxRoutingHops,
		RequiredTimeoutMargin:   DefaultTimeoutMargin,
		StaleLiquidityAfter:     DefaultGossipTTL,
		HopPenalty:              "1",
		CongestionPenalty:       "10",
		StaleLiquidityPenalty:   "25",
		FailurePenalty:          "25",
		TimeoutPenalty:          "50",
		SuccessPenalty:          "25",
		AvailabilityPenalty:     "25",
		ReservePressurePenalty:  "25",
		QueueDelayPenalty:       "10",
		PendingConditionPenalty: "5",
		LatencyPenalty:          "10",
		DecayHalfLife:           DefaultGossipTTL,
		MaxCongestedPaymentBps:  5_000,
		MaxSplits:               1,
	}
}

func (p RoutePolicy) Normalize() RoutePolicy {
	defaults := DefaultRoutePolicy()
	if p.MaxHops <= 0 || p.MaxHops > MaxRoutingHops {
		p.MaxHops = defaults.MaxHops
	}
	if p.RequiredTimeoutMargin == 0 {
		p.RequiredTimeoutMargin = defaults.RequiredTimeoutMargin
	}
	if p.StaleLiquidityAfter == 0 {
		p.StaleLiquidityAfter = defaults.StaleLiquidityAfter
	}
	if strings.TrimSpace(p.HopPenalty) == "" {
		p.HopPenalty = defaults.HopPenalty
	}
	if strings.TrimSpace(p.CongestionPenalty) == "" {
		p.CongestionPenalty = defaults.CongestionPenalty
	}
	if strings.TrimSpace(p.StaleLiquidityPenalty) == "" {
		p.StaleLiquidityPenalty = defaults.StaleLiquidityPenalty
	}
	if strings.TrimSpace(p.FailurePenalty) == "" {
		p.FailurePenalty = defaults.FailurePenalty
	}
	if strings.TrimSpace(p.TimeoutPenalty) == "" {
		p.TimeoutPenalty = defaults.TimeoutPenalty
	}
	if strings.TrimSpace(p.SuccessPenalty) == "" {
		p.SuccessPenalty = defaults.SuccessPenalty
	}
	if strings.TrimSpace(p.AvailabilityPenalty) == "" {
		p.AvailabilityPenalty = defaults.AvailabilityPenalty
	}
	if strings.TrimSpace(p.ReservePressurePenalty) == "" {
		p.ReservePressurePenalty = defaults.ReservePressurePenalty
	}
	if strings.TrimSpace(p.QueueDelayPenalty) == "" {
		p.QueueDelayPenalty = defaults.QueueDelayPenalty
	}
	if strings.TrimSpace(p.PendingConditionPenalty) == "" {
		p.PendingConditionPenalty = defaults.PendingConditionPenalty
	}
	if strings.TrimSpace(p.LatencyPenalty) == "" {
		p.LatencyPenalty = defaults.LatencyPenalty
	}
	if p.DecayHalfLife == 0 {
		p.DecayHalfLife = defaults.DecayHalfLife
	}
	if p.MaxCongestedPaymentBps == 0 {
		p.MaxCongestedPaymentBps = defaults.MaxCongestedPaymentBps
	}
	if strings.TrimSpace(p.MaxFeeAmount) != "" {
		p.MaxFeeAmount = strings.TrimSpace(p.MaxFeeAmount)
	}
	if p.MaxSplits <= 0 {
		p.MaxSplits = defaults.MaxSplits
	}
	if p.MaxSplits > MaxRoutingHops {
		p.MaxSplits = MaxRoutingHops
	}
	for i := range p.ExcludedNodes {
		p.ExcludedNodes[i] = strings.TrimSpace(p.ExcludedNodes[i])
	}
	sort.Strings(p.ExcludedNodes)
	for i := range p.ExcludedChannels {
		p.ExcludedChannels[i] = normalizeHash(p.ExcludedChannels[i])
	}
	sort.Strings(p.ExcludedChannels)
	for i := range p.EdgeStats {
		p.EdgeStats[i] = p.EdgeStats[i].Normalize()
	}
	sort.SliceStable(p.EdgeStats, func(i, j int) bool {
		return routeStatsKey(p.EdgeStats[i]) < routeStatsKey(p.EdgeStats[j])
	})
	return p
}

func (p RoutePolicy) Validate() error {
	policy := p.Normalize()
	for _, value := range []struct {
		field string
		text  string
	}{
		{"payments route hop penalty", policy.HopPenalty},
		{"payments route congestion penalty", policy.CongestionPenalty},
		{"payments route stale liquidity penalty", policy.StaleLiquidityPenalty},
		{"payments route failure penalty", policy.FailurePenalty},
		{"payments route timeout penalty", policy.TimeoutPenalty},
		{"payments route success penalty", policy.SuccessPenalty},
		{"payments route availability penalty", policy.AvailabilityPenalty},
		{"payments route reserve pressure penalty", policy.ReservePressurePenalty},
		{"payments route queue delay penalty", policy.QueueDelayPenalty},
		{"payments route pending condition penalty", policy.PendingConditionPenalty},
		{"payments route latency penalty", policy.LatencyPenalty},
	} {
		if err := validateNonNegativeInt(value.field, value.text); err != nil {
			return err
		}
	}
	if policy.MaxCongestedPaymentBps > 10_000 {
		return errors.New("payments max congested payment bps must be <= 10000")
	}
	if policy.MaxFeeAmount != "" {
		if err := validateNonNegativeInt("payments route max fee", policy.MaxFeeAmount); err != nil {
			return err
		}
	}
	if policy.ProportionalFeeBps > 100_000 {
		return errors.New("payments route proportional fee bps is too high")
	}
	for _, node := range policy.ExcludedNodes {
		if err := addressing.ValidateUserAddress("payments route excluded node", node); err != nil {
			return err
		}
	}
	for _, channelID := range policy.ExcludedChannels {
		if err := ValidateHash("payments route excluded channel", channelID); err != nil {
			return err
		}
	}
	seenStats := make(map[string]struct{}, len(policy.EdgeStats))
	for _, stats := range policy.EdgeStats {
		if err := stats.Validate(); err != nil {
			return err
		}
		key := routeStatsKey(stats)
		if _, found := seenStats[key]; found {
			return errors.New("payments duplicate route stats")
		}
		seenStats[key] = struct{}{}
	}
	return nil
}

func BuildRoutingFeePolicyUpdate(update RoutingFeePolicyUpdate, signer string) (RoutingFeePolicyUpdate, error) {
	update = update.Normalize()
	if update.PolicyID == "" {
		update.PolicyID = HashParts("routing-fee-policy-id", update.ChainID, update.ChannelID, update.From, update.To, fmt.Sprintf("%020d", update.Sequence))
	}
	update.PolicyHash = ""
	update.Signature = RoutingFeePolicySignature{}
	update.PolicyHash = ComputeRoutingFeePolicyHash(update)
	signature, err := SignatureForRoutingFeePolicy(update, signer)
	if err != nil {
		return RoutingFeePolicyUpdate{}, err
	}
	update.Signature = signature
	if err := update.ValidateAtHeight(update.ValidAfterHeight); err != nil {
		return RoutingFeePolicyUpdate{}, err
	}
	return update.Normalize(), nil
}

func ComputeRoutingFeePolicyHash(update RoutingFeePolicyUpdate) string {
	update = update.Normalize()
	return HashParts(
		"routing-fee-policy",
		update.PolicyID,
		update.ChainID,
		update.ChannelID,
		update.From,
		update.To,
		update.FeeDenom,
		update.BaseHopFee,
		fmt.Sprintf("%010d", update.ProportionalFeeBps),
		update.LiquidityReservationFee,
		update.VirtualChannelSetupFee,
		update.CongestionSurcharge,
		update.FailurePenalty,
		update.MaxHopFee,
		fmt.Sprintf("%020d", update.ValidAfterHeight),
		fmt.Sprintf("%020d", update.ValidUntilHeight),
		fmt.Sprintf("%020d", update.Sequence),
	)
}

func SignatureForRoutingFeePolicy(update RoutingFeePolicyUpdate, signer string) (RoutingFeePolicySignature, error) {
	update = update.Normalize()
	signer = strings.TrimSpace(signer)
	if err := addressing.ValidateUserAddress("payments routing fee policy signer", signer); err != nil {
		return RoutingFeePolicySignature{}, err
	}
	if update.PolicyHash == "" {
		update.PolicyHash = ComputeRoutingFeePolicyHash(update)
	}
	return RoutingFeePolicySignature{
		Signer:           signer,
		ChainID:          update.ChainID,
		ChannelID:        update.ChannelID,
		ObjectType:       SignatureObjectRoutingFee,
		Version:          CurrentStateVersion,
		Sequence:         update.Sequence,
		ObjectID:         update.PolicyHash,
		ExpirationHeight: update.ValidUntilHeight,
		CommitmentHash:   update.PolicyHash,
		SignatureHash: ComputeSignatureEnvelopeHash(
			signer,
			update.ChainID,
			update.ChannelID,
			SignatureObjectRoutingFee,
			CurrentStateVersion,
			update.Sequence,
			update.PolicyHash,
			update.ValidUntilHeight,
			update.PolicyHash,
		),
	}, nil
}

func (u RoutingFeePolicyUpdate) Normalize() RoutingFeePolicyUpdate {
	u.PolicyID = normalizeOptionalHash(u.PolicyID)
	u.ChainID = strings.TrimSpace(u.ChainID)
	u.ChannelID = normalizeHash(u.ChannelID)
	u.From = strings.TrimSpace(u.From)
	u.To = strings.TrimSpace(u.To)
	u.FeeDenom = normalizeAssetDenom(u.FeeDenom)
	u.BaseHopFee = strings.TrimSpace(u.BaseHopFee)
	u.LiquidityReservationFee = strings.TrimSpace(u.LiquidityReservationFee)
	u.VirtualChannelSetupFee = strings.TrimSpace(u.VirtualChannelSetupFee)
	u.CongestionSurcharge = strings.TrimSpace(u.CongestionSurcharge)
	u.FailurePenalty = strings.TrimSpace(u.FailurePenalty)
	u.MaxHopFee = strings.TrimSpace(u.MaxHopFee)
	for _, field := range []*string{&u.BaseHopFee, &u.LiquidityReservationFee, &u.VirtualChannelSetupFee, &u.CongestionSurcharge, &u.FailurePenalty, &u.MaxHopFee} {
		if *field == "" {
			*field = "0"
		}
	}
	u.PolicyHash = normalizeOptionalHash(u.PolicyHash)
	u.Signature = u.Signature.Normalize()
	return u
}

func (u RoutingFeePolicyUpdate) ValidateAtHeight(currentHeight uint64) error {
	update := u.Normalize()
	if update.PolicyID == "" {
		return errors.New("payments routing fee policy id is required")
	}
	if err := ValidateHash("payments routing fee policy id", update.PolicyID); err != nil {
		return err
	}
	if update.ChainID == "" {
		return errors.New("payments routing fee policy chain id is required")
	}
	if err := ValidateHash("payments routing fee policy channel id", update.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments routing fee policy from", update.From); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments routing fee policy to", update.To); err != nil {
		return err
	}
	if update.From == update.To {
		return errors.New("payments routing fee policy endpoints must differ")
	}
	if update.FeeDenom != NativeDenom {
		return fmt.Errorf("payments routing fee policy denom must be %s", NativeDenom)
	}
	for _, value := range []struct {
		field string
		text  string
	}{
		{"payments routing base hop fee", update.BaseHopFee},
		{"payments routing liquidity reservation fee", update.LiquidityReservationFee},
		{"payments routing virtual setup fee", update.VirtualChannelSetupFee},
		{"payments routing congestion surcharge", update.CongestionSurcharge},
		{"payments routing failure penalty", update.FailurePenalty},
		{"payments routing max hop fee", update.MaxHopFee},
	} {
		if err := validateNonNegativeInt(value.field, value.text); err != nil {
			return err
		}
	}
	if update.ProportionalFeeBps > 100_000 {
		return errors.New("payments routing proportional fee bps is too high")
	}
	if update.ValidAfterHeight == 0 || update.ValidUntilHeight == 0 || update.ValidUntilHeight < update.ValidAfterHeight {
		return errors.New("payments routing fee policy validity window is invalid")
	}
	if currentHeight != 0 && (currentHeight < update.ValidAfterHeight || currentHeight > update.ValidUntilHeight) {
		return errors.New("payments routing fee policy is outside validity window")
	}
	if update.Sequence == 0 {
		return errors.New("payments routing fee policy sequence must be positive")
	}
	expectedHash := ComputeRoutingFeePolicyHash(update)
	if update.PolicyHash != expectedHash {
		return errors.New("payments routing fee policy hash mismatch")
	}
	return update.Signature.Validate(update)
}

func (s RoutingFeePolicySignature) Normalize() RoutingFeePolicySignature {
	s.Signer = strings.TrimSpace(s.Signer)
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.ChannelID = normalizeHash(s.ChannelID)
	s.ObjectType = strings.TrimSpace(s.ObjectType)
	s.ObjectID = normalizeOptionalHash(s.ObjectID)
	s.CommitmentHash = normalizeOptionalHash(s.CommitmentHash)
	s.SignatureHash = normalizeOptionalHash(s.SignatureHash)
	return s
}

func (s RoutingFeePolicySignature) Validate(update RoutingFeePolicyUpdate) error {
	sig := s.Normalize()
	update = update.Normalize()
	if err := addressing.ValidateUserAddress("payments routing fee policy signature signer", sig.Signer); err != nil {
		return err
	}
	if sig.Signer != update.From {
		return errors.New("payments routing fee policy signer must be forwarding node")
	}
	if sig.ChainID != update.ChainID || sig.ChannelID != update.ChannelID {
		return errors.New("payments routing fee policy signature domain mismatch")
	}
	if sig.ObjectType != SignatureObjectRoutingFee {
		return errors.New("payments routing fee policy signature object type mismatch")
	}
	if sig.Version != CurrentStateVersion || sig.Sequence != update.Sequence {
		return errors.New("payments routing fee policy signature version or sequence mismatch")
	}
	if sig.ObjectID != update.PolicyHash || sig.CommitmentHash != update.PolicyHash {
		return errors.New("payments routing fee policy signature commitment mismatch")
	}
	if sig.ExpirationHeight != update.ValidUntilHeight {
		return errors.New("payments routing fee policy signature expiration mismatch")
	}
	if err := ValidateHash("payments routing fee policy signature hash", sig.SignatureHash); err != nil {
		return err
	}
	expected := ComputeSignatureEnvelopeHash(sig.Signer, sig.ChainID, sig.ChannelID, sig.ObjectType, sig.Version, sig.Sequence, sig.ObjectID, sig.ExpirationHeight, sig.CommitmentHash)
	if sig.SignatureHash != expected {
		return errors.New("payments routing fee policy signature value mismatch")
	}
	return nil
}

func (r RouteSelectionRequest) Normalize() RouteSelectionRequest {
	r.From = strings.TrimSpace(r.From)
	r.To = strings.TrimSpace(r.To)
	r.Amount = strings.TrimSpace(r.Amount)
	r.Policy = r.Policy.Normalize()
	return r
}

func (r RouteSelectionRequest) Validate() error {
	req := r.Normalize()
	if err := addressing.ValidateUserAddress("payments route request from", req.From); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments route request to", req.To); err != nil {
		return err
	}
	if req.From == req.To {
		return errors.New("payments route endpoints must differ")
	}
	if _, err := parsePositiveInt("payments route request amount", req.Amount); err != nil {
		return err
	}
	if req.CurrentHeight == 0 {
		return errors.New("payments route request height must be positive")
	}
	return req.Policy.Validate()
}

func (p RouteRetryPolicy) Normalize() RouteRetryPolicy {
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 3
	}
	if p.AlternateRouteLimit == 0 {
		p.AlternateRouteLimit = p.MaxAttempts
	}
	return p
}

func (p RouteRetryPolicy) Validate() error {
	policy := p.Normalize()
	if policy.MaxAttempts == 0 || policy.MaxAttempts > 32 {
		return errors.New("payments route retry max attempts must be between 1 and 32")
	}
	if policy.AlternateRouteLimit == 0 || policy.AlternateRouteLimit > policy.MaxAttempts {
		return errors.New("payments route retry alternate limit must be within attempts")
	}
	return nil
}

func (r RouteRetryRequest) Normalize() RouteRetryRequest {
	r.Selection = r.Selection.Normalize()
	for i, failure := range r.Failures {
		r.Failures[i] = failure.Normalize()
	}
	sort.SliceStable(r.Failures, func(i, j int) bool {
		return routeFailureKey(r.Failures[i]) < routeFailureKey(r.Failures[j])
	})
	r.Policy = r.Policy.Normalize()
	return r
}

func (r RouteRetryRequest) Validate() error {
	req := r.Normalize()
	if err := req.Selection.Validate(); err != nil {
		return err
	}
	if err := req.Policy.Validate(); err != nil {
		return err
	}
	for _, failure := range req.Failures {
		if err := failure.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func IsRouteFailureClass(failureClass RouteFailureClass) bool {
	switch failureClass {
	case RouteFailureCapacity,
		RouteFailureTimeout,
		RouteFailureCongestion,
		RouteFailureLiquidityStale,
		RouteFailureNodeUnavailable,
		RouteFailurePolicyRejected,
		RouteFailureUnknown:
		return true
	default:
		return false
	}
}

func (r ScoredRoute) Normalize() ScoredRoute {
	for i, edge := range r.Edges {
		r.Edges[i] = edge.Normalize()
	}
	r.Amount = strings.TrimSpace(r.Amount)
	r.TotalFee = strings.TrimSpace(r.TotalFee)
	r.TotalCost = strings.TrimSpace(r.TotalCost)
	r.MinCapacity = strings.TrimSpace(r.MinCapacity)
	r.ScoreHash = normalizeOptionalHash(r.ScoreHash)
	return r
}

func (r ScoredRoute) Validate() error {
	route := r.Normalize()
	if len(route.Edges) == 0 {
		return errors.New("payments scored route requires edges")
	}
	if _, err := parsePositiveInt("payments scored route amount", route.Amount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments scored route total fee", route.TotalFee); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments scored route total cost", route.TotalCost); err != nil {
		return err
	}
	if _, err := parsePositiveInt("payments scored route min capacity", route.MinCapacity); err != nil {
		return err
	}
	if route.ScoreHash != "" {
		return ValidateHash("payments scored route hash", route.ScoreHash)
	}
	return nil
}

func DeriveRouteID(seed string, nonce uint64) (string, error) {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return "", errors.New("payments route id seed is required")
	}
	if nonce == 0 {
		return "", errors.New("payments route id nonce must be positive")
	}
	return HashParts("route-id", seed, fmt.Sprintf("%020d", nonce)), nil
}

func DeriveHopRouteID(routeID string, hopIndex int, channelID string) (string, error) {
	routeID = normalizeHash(routeID)
	channelID = normalizeHash(channelID)
	if err := ValidateHash("payments root route id", routeID); err != nil {
		return "", err
	}
	if hopIndex < 0 {
		return "", errors.New("payments hop index must be non-negative")
	}
	if err := ValidateHash("payments hop route channel id", channelID); err != nil {
		return "", err
	}
	return HashParts("hop-route-id", routeID, fmt.Sprintf("%020d", uint64(hopIndex)), channelID), nil
}

func DeriveHopPaymentID(routeID string, hopIndex int, channelID string) (string, error) {
	hopRouteID, err := DeriveHopRouteID(routeID, hopIndex, channelID)
	if err != nil {
		return "", err
	}
	return HashParts("hop-payment-id", hopRouteID), nil
}

func ComputeForwardingPacketHash(packet ForwardingPacket) string {
	packet = packet.Normalize()
	return HashParts(
		"forwarding-packet",
		packet.RouteID,
		packet.HopPaymentID,
		packet.ChannelID,
		packet.ForwardingNode,
		packet.NextNode,
		packet.Amount,
		packet.FeeAmount,
		fmt.Sprintf("%020d", packet.TimeoutHeight),
		packet.NextPacketHash,
	)
}

func (p ForwardingPacket) Normalize() ForwardingPacket {
	p.PacketID = normalizeOptionalHash(p.PacketID)
	p.RouteID = normalizeHash(p.RouteID)
	p.HopPaymentID = normalizeHash(p.HopPaymentID)
	p.ChannelID = normalizeHash(p.ChannelID)
	p.ForwardingNode = strings.TrimSpace(p.ForwardingNode)
	p.NextNode = strings.TrimSpace(p.NextNode)
	p.Amount = strings.TrimSpace(p.Amount)
	p.FeeAmount = strings.TrimSpace(p.FeeAmount)
	p.NextPacketHash = normalizeOptionalHash(p.NextPacketHash)
	p.PacketHash = normalizeOptionalHash(p.PacketHash)
	return p
}

func (p ForwardingPacket) Validate() error {
	packet := p.Normalize()
	if err := ValidateHash("payments forwarding packet id", packet.PacketID); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding route id", packet.RouteID); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding payment id", packet.HopPaymentID); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding channel id", packet.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments forwarding node", packet.ForwardingNode); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments forwarding next node", packet.NextNode); err != nil {
		return err
	}
	if packet.ForwardingNode == packet.NextNode {
		return errors.New("payments forwarding packet nodes must differ")
	}
	if _, err := parsePositiveInt("payments forwarding amount", packet.Amount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments forwarding fee", packet.FeeAmount); err != nil {
		return err
	}
	if packet.TimeoutHeight == 0 {
		return errors.New("payments forwarding timeout height must be positive")
	}
	if packet.NextPacketHash != "" {
		if err := ValidateHash("payments forwarding next packet hash", packet.NextPacketHash); err != nil {
			return err
		}
	}
	if packet.PacketHash != ComputeForwardingPacketHash(packet) {
		return errors.New("payments forwarding packet hash mismatch")
	}
	return nil
}

func (r ForwardingPacketReplayRecord) Normalize() ForwardingPacketReplayRecord {
	r.PacketID = normalizeHash(r.PacketID)
	r.RouteID = normalizeHash(r.RouteID)
	r.HopPaymentID = normalizeHash(r.HopPaymentID)
	return r
}

func (r ForwardingPacketReplayRecord) Validate() error {
	record := r.Normalize()
	if err := ValidateHash("payments forwarding replay packet id", record.PacketID); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding replay route id", record.RouteID); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding replay payment id", record.HopPaymentID); err != nil {
		return err
	}
	if record.RecordedHeight == 0 {
		return errors.New("payments forwarding replay recorded height must be positive")
	}
	if record.ExpiresHeight <= record.RecordedHeight {
		return errors.New("payments forwarding replay expiry must exceed recorded height")
	}
	return nil
}

func (r ForwardingLogRecord) Normalize() ForwardingLogRecord {
	r.PacketID = normalizeHash(r.PacketID)
	r.RouteID = normalizeHash(r.RouteID)
	r.HopPaymentID = normalizeHash(r.HopPaymentID)
	r.ChannelID = normalizeHash(r.ChannelID)
	r.ForwardingNode = strings.TrimSpace(r.ForwardingNode)
	r.NextNodeHash = normalizeHash(r.NextNodeHash)
	r.AmountHash = normalizeHash(r.AmountHash)
	return r
}

func (r ForwardingLogRecord) Validate() error {
	record := r.Normalize()
	if err := ValidateHash("payments forwarding log packet id", record.PacketID); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding log route id", record.RouteID); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding log payment id", record.HopPaymentID); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding log channel id", record.ChannelID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments forwarding log node", record.ForwardingNode); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding log next node hash", record.NextNodeHash); err != nil {
		return err
	}
	if err := ValidateHash("payments forwarding log amount hash", record.AmountHash); err != nil {
		return err
	}
	if record.RecordedHeight == 0 {
		return errors.New("payments forwarding log height must be positive")
	}
	return nil
}

func (c RouteFeeClaim) Normalize() RouteFeeClaim {
	c.ChannelID = normalizeHash(c.ChannelID)
	c.PromiseID = normalizeHash(c.PromiseID)
	c.Recipient = strings.TrimSpace(c.Recipient)
	c.Amount = strings.TrimSpace(c.Amount)
	c.EvidenceHash = normalizeHash(c.EvidenceHash)
	return c
}

func (c RouteFeeClaim) Validate() error {
	c = c.Normalize()
	if err := ValidateHash("payments route fee claim channel id", c.ChannelID); err != nil {
		return err
	}
	if err := ValidateHash("payments route fee claim promise id", c.PromiseID); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments route fee recipient", c.Recipient); err != nil {
		return err
	}
	if err := validatePositiveInt("payments route fee amount", c.Amount); err != nil {
		return err
	}
	return ValidateHash("payments route fee evidence hash", c.EvidenceHash)
}
