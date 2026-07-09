package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

func RoutePayment(state PaymentsState, from, to, amountText string, currentHeight uint64, maxHops int) ([]ChannelEdge, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return nil, err
	}
	amount, err := parsePositiveInt("payments route amount", amountText)
	if err != nil {
		return nil, err
	}
	if maxHops <= 0 || maxHops > MaxRoutingHops {
		maxHops = MaxRoutingHops
	}
	candidates := activeEdgesForAmount(state.Edges, amount, currentHeight)
	sortEdges(candidates)
	type path struct {
		node  string
		edges []ChannelEdge
	}
	queue := []path{{node: from}}
	visitedDepth := map[string]int{from: 0}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if len(current.edges) >= maxHops {
			continue
		}
		for _, edge := range candidates {
			if edge.From != current.node {
				continue
			}
			nextEdges := append([]ChannelEdge(nil), current.edges...)
			nextEdges = append(nextEdges, edge)
			if edge.To == to {
				return nextEdges, nil
			}
			if depth, seen := visitedDepth[edge.To]; seen && depth <= len(nextEdges) {
				continue
			}
			visitedDepth[edge.To] = len(nextEdges)
			queue = append(queue, path{node: edge.To, edges: nextEdges})
		}
	}
	return nil, errors.New("payments route not found")
}

func SelectPaymentRoute(state PaymentsState, store TopologyStore, req RouteSelectionRequest) (ScoredRoute, error) {
	state = state.Export()
	if err := state.Validate(); err != nil {
		return ScoredRoute{}, err
	}
	store = store.Normalize()
	if err := store.Validate(); err != nil {
		return ScoredRoute{}, err
	}
	req = req.Normalize()
	if err := req.Validate(); err != nil {
		return ScoredRoute{}, err
	}
	amount, err := parsePositiveInt("payments scored route amount", req.Amount)
	if err != nil {
		return ScoredRoute{}, err
	}
	route, err := selectPaymentRouteWithPolicy(state, store, req, amount)
	if err != nil {
		return ScoredRoute{}, err
	}
	sim, err := SimulateRoute(route, req)
	if err != nil {
		return ScoredRoute{}, err
	}
	if !sim.Attemptable {
		return ScoredRoute{}, errors.New(sim.Reason)
	}
	return route, nil
}

func ApplyCongestionSnapshot(policy RoutePolicy, snapshot CongestionSnapshot) (RoutePolicy, error) {
	policy = policy.Normalize()
	snapshot = snapshot.Normalize()
	if err := snapshot.Validate(); err != nil {
		return RoutePolicy{}, err
	}
	stats := EdgeRoutingStats{
		ChannelID:              snapshot.ChannelID,
		From:                   snapshot.From,
		To:                     snapshot.To,
		SuccessRateBps:         10_000 - snapshot.ChannelUpdateFailureRateBps,
		LiquidityUpdatedHeight: snapshot.LiquidityUpdatedHeight,
		CongestionBps:          snapshot.ChannelUpdateFailureRateBps,
		NodeAvailabilityBps:    10_000,
		FailureCount:           uint32(snapshot.ChannelUpdateFailureRateBps / 1_000),
		PendingConditionCount:  snapshot.PendingConditionCount,
		AvgResolutionLatency:   snapshot.AvgResolutionLatency,
		RetryCount:             snapshot.RouteRetryCount,
		ReservePressureBps:     snapshot.ReservePressureBps,
		NodeQueueDelay:         snapshot.NodeQueueDelay,
		LastUpdatedHeight:      snapshot.ObservedHeight,
	}
	if snapshot.NodeQueueDelay > 0 {
		stats.NodeAvailabilityBps = 10_000 - uint32Min(10_000, uint32(snapshot.NodeQueueDelay))
	}
	policy.EdgeStats = upsertRouteStats(policy.EdgeStats, stats)
	return policy.Normalize(), policy.Validate()
}

func ApplyRouteFailureReport(policy RoutePolicy, report RouteFailureReport) (RoutePolicy, error) {
	policy = policy.Normalize()
	report = report.Normalize()
	if err := report.Validate(); err != nil {
		return RoutePolicy{}, err
	}
	stats, found := routeStatsForEdge(policy, ChannelEdge{ChannelID: report.ChannelID, From: report.From, To: report.To})
	if !found {
		stats = EdgeRoutingStats{
			ChannelID:              report.ChannelID,
			From:                   report.From,
			To:                     report.To,
			SuccessRateBps:         10_000,
			NodeAvailabilityBps:    10_000,
			LiquidityUpdatedHeight: report.ObservedHeight,
		}
	}
	stats.FailureCount++
	stats.RetryCount++
	stats.LastFailureHeight = report.ObservedHeight
	stats.LastUpdatedHeight = report.ObservedHeight
	switch report.FailureClass {
	case RouteFailureCapacity:
		stats.ReservePressureBps = uint32Max(stats.ReservePressureBps, 8_000)
	case RouteFailureTimeout:
		stats.TimeoutMargin = 1
	case RouteFailureCongestion:
		stats.CongestionBps = uint32Max(stats.CongestionBps, 8_000)
		stats.PendingConditionCount++
	case RouteFailureLiquidityStale:
		stats.LiquidityUpdatedHeight = 1
	case RouteFailureNodeUnavailable:
		stats.NodeAvailabilityBps = 1_000
	case RouteFailurePolicyRejected:
		stats.CongestionBps = uint32Max(stats.CongestionBps, 5_000)
	default:
		stats.CongestionBps = uint32Max(stats.CongestionBps, 4_000)
	}
	policy.EdgeStats = upsertRouteStats(policy.EdgeStats, stats)
	return policy.Normalize(), policy.Validate()
}

func DecayRoutePolicyPenalties(policy RoutePolicy, currentHeight uint64) RoutePolicy {
	policy = policy.Normalize()
	if currentHeight == 0 {
		return policy
	}
	for i, stats := range policy.EdgeStats {
		policy.EdgeStats[i] = decayEdgeRoutingStats(stats, currentHeight, policy.DecayHalfLife)
	}
	return policy.Normalize()
}

func RetryPaymentRoute(state PaymentsState, store TopologyStore, req RouteRetryRequest) (RouteRetryResult, error) {
	req = req.Normalize()
	if err := req.Validate(); err != nil {
		return RouteRetryResult{}, err
	}
	selection := req.Selection
	selection.Policy = DecayRoutePolicyPenalties(selection.Policy, selection.CurrentHeight)
	for _, failure := range req.Failures {
		var err error
		selection.Policy, err = ApplyRouteFailureReport(selection.Policy, failure)
		if err != nil {
			return RouteRetryResult{}, err
		}
		if req.Policy.ExcludeFailedEdges {
			selection.Policy.ExcludedChannels = append(selection.Policy.ExcludedChannels, failure.ChannelID)
		}
	}
	if uint32(len(req.Failures)) >= req.Policy.MaxAttempts {
		return RouteRetryResult{Attempts: uint32(len(req.Failures)), Retryable: false, Reason: "payments route retry attempts exhausted"}, nil
	}
	route, err := SelectPaymentRoute(state, store, selection)
	if err != nil {
		return RouteRetryResult{Attempts: uint32(len(req.Failures)) + 1, Retryable: false, Reason: err.Error()}, err
	}
	return RouteRetryResult{
		Route:      route,
		Attempts:   uint32(len(req.Failures)) + 1,
		Retryable:  true,
		PolicyHash: routePolicyHash(selection.Policy),
	}, nil
}

func ClassifyRouteFailure(reason string) RouteFailureClass {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(reason, "capacity") || strings.Contains(reason, "reserve"):
		return RouteFailureCapacity
	case strings.Contains(reason, "timeout") || strings.Contains(reason, "expired"):
		return RouteFailureTimeout
	case strings.Contains(reason, "congestion") || strings.Contains(reason, "queue"):
		return RouteFailureCongestion
	case strings.Contains(reason, "stale") || strings.Contains(reason, "fresh"):
		return RouteFailureLiquidityStale
	case strings.Contains(reason, "availability") || strings.Contains(reason, "unavailable"):
		return RouteFailureNodeUnavailable
	case strings.Contains(reason, "policy") || strings.Contains(reason, "fee"):
		return RouteFailurePolicyRejected
	default:
		return RouteFailureUnknown
	}
}

func CalculateHopRoutingFee(req HopFeeCalculationRequest) (RoutingHopFee, error) {
	policy := req.Policy.Normalize()
	if err := policy.ValidateAtHeight(req.CurrentHeight); err != nil {
		return RoutingHopFee{}, err
	}
	amount, err := parsePositiveInt("payments routing hop fee amount", req.Amount)
	if err != nil {
		return RoutingHopFee{}, err
	}
	base, err := parseNonNegativeInt("payments routing base hop fee", policy.BaseHopFee)
	if err != nil {
		return RoutingHopFee{}, err
	}
	proportional := sdkmath.ZeroInt()
	if policy.ProportionalFeeBps > 0 {
		proportional = amount.Mul(sdkmath.NewInt(int64(policy.ProportionalFeeBps)))
		denom := sdkmath.NewInt(10_000)
		proportional = proportional.Add(denom.Sub(sdkmath.OneInt())).Quo(denom)
	}
	reservation, err := parseNonNegativeInt("payments routing liquidity reservation fee", policy.LiquidityReservationFee)
	if err != nil {
		return RoutingHopFee{}, err
	}
	virtualSetup := sdkmath.ZeroInt()
	if req.IncludeVirtualSetup {
		virtualSetup, err = parseNonNegativeInt("payments routing virtual setup fee", policy.VirtualChannelSetupFee)
		if err != nil {
			return RoutingHopFee{}, err
		}
	}
	congestion, err := parseNonNegativeInt("payments routing congestion surcharge", policy.CongestionSurcharge)
	if err != nil {
		return RoutingHopFee{}, err
	}
	failurePenaltyUnit, err := parseNonNegativeInt("payments routing failure penalty", policy.FailurePenalty)
	if err != nil {
		return RoutingHopFee{}, err
	}
	failurePenalty := failurePenaltyUnit.Mul(sdkmath.NewInt(int64(req.RepeatedInvalidAttempts)))
	total := base.Add(proportional).Add(reservation).Add(virtualSetup).Add(congestion).Add(failurePenalty)
	maxHopFee, err := parseNonNegativeInt("payments routing max hop fee", policy.MaxHopFee)
	if err != nil {
		return RoutingHopFee{}, err
	}
	if !maxHopFee.IsZero() && total.GT(maxHopFee) {
		return RoutingHopFee{}, errors.New("payments routing hop fee exceeds policy maximum")
	}
	return RoutingHopFee{
		Denom:                   NativeDenom,
		BaseHopFee:              base.String(),
		ProportionalFee:         proportional.String(),
		LiquidityReservationFee: reservation.String(),
		VirtualChannelSetupFee:  virtualSetup.String(),
		CongestionSurcharge:     congestion.String(),
		FailurePenalty:          failurePenalty.String(),
		RepeatedInvalidAttempts: req.RepeatedInvalidAttempts,
		TotalFee:                total.String(),
		PolicyHash:              policy.PolicyHash,
	}, nil
}

func ValidateRouteFeeCeiling(route ScoredRoute, policy RoutePolicy) error {
	route = route.Normalize()
	policy = policy.Normalize()
	if strings.TrimSpace(policy.MaxFeeAmount) == "" {
		return nil
	}
	totalFee, err := parseNonNegativeInt("payments route total fee", route.TotalFee)
	if err != nil {
		return err
	}
	maxFee, err := parseNonNegativeInt("payments route policy max fee", policy.MaxFeeAmount)
	if err != nil {
		return err
	}
	if totalFee.GT(maxFee) {
		return errors.New("payments route fee exceeds policy ceiling")
	}
	return nil
}

func ValidateConditionLinkageFeeCeiling(proof ConditionLinkageProof, policy RoutePolicy) error {
	proof = proof.Normalize()
	policy = policy.Normalize()
	if err := policy.Validate(); err != nil {
		return err
	}
	totalFees, err := parseNonNegativeInt("payments linked route declared fees", proof.TotalFees)
	if err != nil {
		return err
	}
	promiseFees := sdkmath.ZeroInt()
	for i := 1; i < len(proof.Promises); i++ {
		fee, err := parseNonNegativeInt("payments linked route promise fee", proof.Promises[i].Fee)
		if err != nil {
			return err
		}
		promiseFees = promiseFees.Add(fee)
	}
	if promiseFees.GT(totalFees) {
		return errors.New("payments linked route promise fee overcharge")
	}
	if strings.TrimSpace(policy.MaxFeeAmount) == "" {
		return nil
	}
	maxFee, err := parseNonNegativeInt("payments linked route policy max fee", policy.MaxFeeAmount)
	if err != nil {
		return err
	}
	if totalFees.GT(maxFee) || promiseFees.GT(maxFee) {
		return errors.New("payments linked route fee exceeds policy ceiling")
	}
	return nil
}

func SimulateRoute(route ScoredRoute, req RouteSelectionRequest) (RouteSimulationResult, error) {
	route = route.Normalize()
	req = req.Normalize()
	if err := route.Validate(); err != nil {
		return RouteSimulationResult{}, err
	}
	if err := req.Validate(); err != nil {
		return RouteSimulationResult{}, err
	}
	amount, err := parsePositiveInt("payments route simulation amount", route.Amount)
	if err != nil {
		return RouteSimulationResult{}, err
	}
	if route.Edges[0].From != req.From {
		return RouteSimulationResult{Route: route, Attemptable: false, Reason: "payments route simulation source mismatch", TotalFee: route.TotalFee}, nil
	}
	if route.Edges[len(route.Edges)-1].To != req.To {
		return RouteSimulationResult{Route: route, Attemptable: false, Reason: "payments route simulation destination mismatch", TotalFee: route.TotalFee}, nil
	}
	for i, edge := range route.Edges {
		edge = edge.Normalize()
		if i > 0 && route.Edges[i-1].To != edge.From {
			return RouteSimulationResult{Route: route, Attemptable: false, Reason: "payments route simulation discontinuity", TotalFee: route.TotalFee}, nil
		}
		capacity, err := parsePositiveInt("payments route simulation capacity", edge.Capacity)
		if err != nil {
			return RouteSimulationResult{}, err
		}
		if !edge.Active || capacity.LT(amount) {
			return RouteSimulationResult{Route: route, Attemptable: false, Reason: "payments route simulation capacity below amount", TotalFee: route.TotalFee}, nil
		}
		if edge.ExpiresHeight > 0 && req.CurrentHeight > edge.ExpiresHeight {
			return RouteSimulationResult{Route: route, Attemptable: false, Reason: "payments route simulation edge expired", TotalFee: route.TotalFee}, nil
		}
	}
	if err := ValidateRouteFeeCeiling(route, req.Policy); err != nil {
		if !strings.Contains(err.Error(), "policy ceiling") {
			return RouteSimulationResult{}, err
		}
		return RouteSimulationResult{Route: route, Attemptable: false, Reason: "payments route simulation fee exceeds policy", TotalFee: route.TotalFee}, nil
	}
	return RouteSimulationResult{Route: route, Attemptable: true, TotalFee: route.TotalFee}, nil
}

func SplitPaymentRoute(state PaymentsState, store TopologyStore, req RouteSelectionRequest) (MultiPathRoute, error) {
	req = req.Normalize()
	if err := req.Validate(); err != nil {
		return MultiPathRoute{}, err
	}
	if !req.Policy.EnableMultiPath {
		route, err := SelectPaymentRoute(state, store, req)
		if err != nil {
			return MultiPathRoute{}, err
		}
		return buildMultiPathRoute([]ScoredRoute{route})
	}
	amount, err := parsePositiveInt("payments multipath amount", req.Amount)
	if err != nil {
		return MultiPathRoute{}, err
	}
	maxSplits := req.Policy.Normalize().MaxSplits
	remaining := amount
	parts := make([]ScoredRoute, 0, maxSplits)
	excludedChannels := append([]string(nil), req.Policy.ExcludedChannels...)
	for split := 0; split < maxSplits && remaining.IsPositive(); split++ {
		splitsLeft := maxSplits - split
		chunk := remaining.Quo(sdkmath.NewInt(int64(splitsLeft)))
		if chunk.IsZero() {
			chunk = remaining
		}
		if remaining.Mod(sdkmath.NewInt(int64(splitsLeft))).IsPositive() {
			chunk = chunk.Add(sdkmath.NewInt(1))
		}
		partReq := req
		partReq.Amount = chunk.String()
		partReq.Policy.ExcludedChannels = append([]string(nil), excludedChannels...)
		route, err := SelectPaymentRoute(state, store, partReq)
		if err != nil {
			if len(parts) == 0 {
				return MultiPathRoute{}, err
			}
			break
		}
		parts = append(parts, route)
		remaining = remaining.Sub(chunk)
		for _, edge := range route.Edges {
			excludedChannels = append(excludedChannels, edge.ChannelID)
		}
	}
	if remaining.IsPositive() {
		return MultiPathRoute{}, errors.New("payments multipath route capacity insufficient")
	}
	return buildMultiPathRoute(parts)
}

func BuildForwardingPackets(route ScoredRoute, routeSeed string, routeNonce uint64, timeoutHeight uint64) ([]ForwardingPacket, error) {
	route = route.Normalize()
	if err := route.Validate(); err != nil {
		return nil, err
	}
	if timeoutHeight == 0 {
		return nil, errors.New("payments forwarding timeout height must be positive")
	}
	rootRouteID, err := DeriveRouteID(routeSeed, routeNonce)
	if err != nil {
		return nil, err
	}
	packets := make([]ForwardingPacket, len(route.Edges))
	nextHash := ""
	for i := len(route.Edges) - 1; i >= 0; i-- {
		edge := route.Edges[i].Normalize()
		hopRouteID, err := DeriveHopRouteID(rootRouteID, i, edge.ChannelID)
		if err != nil {
			return nil, err
		}
		hopPaymentID, err := DeriveHopPaymentID(rootRouteID, i, edge.ChannelID)
		if err != nil {
			return nil, err
		}
		packet := ForwardingPacket{
			RouteID:        hopRouteID,
			HopPaymentID:   hopPaymentID,
			ChannelID:      edge.ChannelID,
			ForwardingNode: edge.From,
			NextNode:       edge.To,
			Amount:         route.Amount,
			FeeAmount:      edge.FeeAmount,
			TimeoutHeight:  timeoutHeight,
			NextPacketHash: nextHash,
		}.Normalize()
		packet.PacketHash = ComputeForwardingPacketHash(packet)
		packet.PacketID = HashParts("forwarding-packet-id", packet.PacketHash)
		packet = packet.Normalize()
		if err := packet.Validate(); err != nil {
			return nil, err
		}
		packets[i] = packet
		nextHash = packet.PacketHash
	}
	return packets, nil
}

func ValidateForwardingPacket(packet ForwardingPacket, expectedForwarder string, replayRecords []ForwardingPacketReplayRecord, currentHeight uint64) error {
	packet = packet.Normalize()
	if err := packet.Validate(); err != nil {
		return err
	}
	expectedForwarder = strings.TrimSpace(expectedForwarder)
	if expectedForwarder != "" && packet.ForwardingNode != expectedForwarder {
		return errors.New("payments forwarding packet forwarder mismatch")
	}
	if currentHeight == 0 {
		return errors.New("payments forwarding packet validation height must be positive")
	}
	if currentHeight > packet.TimeoutHeight {
		return errors.New("payments forwarding packet is expired")
	}
	for _, record := range normalizeForwardingReplayRecords(replayRecords) {
		if currentHeight > record.ExpiresHeight {
			continue
		}
		if record.PacketID == packet.PacketID {
			return errors.New("payments forwarding packet replay detected")
		}
		if record.RouteID == packet.RouteID {
			return errors.New("payments forwarding route id replay detected")
		}
		if record.HopPaymentID == packet.HopPaymentID {
			return errors.New("payments forwarding payment id replay detected")
		}
	}
	return nil
}

func RecordForwardingPacket(replayRecords []ForwardingPacketReplayRecord, packet ForwardingPacket, currentHeight uint64) ([]ForwardingPacketReplayRecord, error) {
	if err := ValidateForwardingPacket(packet, packet.ForwardingNode, replayRecords, currentHeight); err != nil {
		return nil, err
	}
	records := append(normalizeForwardingReplayRecords(replayRecords), ForwardingPacketReplayRecord{
		PacketID:       packet.PacketID,
		RouteID:        packet.RouteID,
		HopPaymentID:   packet.HopPaymentID,
		RecordedHeight: currentHeight,
		ExpiresHeight:  currentHeight + DefaultReplayHorizon,
	}.Normalize())
	sortForwardingReplayRecords(records)
	return records, nil
}

func PruneForwardingReplayRecords(replayRecords []ForwardingPacketReplayRecord, currentHeight uint64) []ForwardingPacketReplayRecord {
	out := make([]ForwardingPacketReplayRecord, 0, len(replayRecords))
	for _, record := range normalizeForwardingReplayRecords(replayRecords) {
		if currentHeight <= record.ExpiresHeight {
			out = append(out, record)
		}
	}
	sortForwardingReplayRecords(out)
	return out
}

func PrivacySafeForwardingLog(packet ForwardingPacket, currentHeight uint64) (ForwardingLogRecord, error) {
	packet = packet.Normalize()
	if err := packet.Validate(); err != nil {
		return ForwardingLogRecord{}, err
	}
	if currentHeight == 0 {
		return ForwardingLogRecord{}, errors.New("payments forwarding log height must be positive")
	}
	record := ForwardingLogRecord{
		PacketID:       packet.PacketID,
		RouteID:        packet.RouteID,
		HopPaymentID:   packet.HopPaymentID,
		ChannelID:      packet.ChannelID,
		ForwardingNode: packet.ForwardingNode,
		NextNodeHash:   HashParts("forwarding-next-node", packet.NextNode),
		AmountHash:     HashParts("forwarding-amount", packet.Amount, packet.FeeAmount),
		RecordedHeight: currentHeight,
	}.Normalize()
	return record, record.Validate()
}

func activeEdgesForAmount(edges []ChannelEdge, amount sdkmath.Int, currentHeight uint64) []ChannelEdge {
	out := make([]ChannelEdge, 0, len(edges))
	for _, edge := range edges {
		edge = edge.Normalize()
		capacity, err := parsePositiveInt("payments routing capacity", edge.Capacity)
		if err != nil {
			continue
		}
		if !edge.Active || capacity.LT(amount) {
			continue
		}
		if edge.ExpiresHeight > 0 && currentHeight > edge.ExpiresHeight {
			continue
		}
		out = append(out, edge)
	}
	return out
}

type routeSearchPath struct {
	node  string
	edges []ChannelEdge
	cost  sdkmath.Int
	fee   sdkmath.Int
}

func selectPaymentRouteWithPolicy(state PaymentsState, store TopologyStore, req RouteSelectionRequest, amount sdkmath.Int) (ScoredRoute, error) {
	policy := req.Policy.Normalize()
	candidates := candidateRoutingEdges(state, store, amount, req.CurrentHeight, policy)
	if len(candidates) == 0 {
		return ScoredRoute{}, errors.New("payments scored route has no eligible edges")
	}
	sortEdges(candidates)
	queue := []routeSearchPath{{node: req.From, cost: sdkmath.ZeroInt(), fee: sdkmath.ZeroInt()}}
	bestByNode := map[string]sdkmath.Int{req.From: sdkmath.ZeroInt()}
	for len(queue) > 0 {
		sortRouteQueue(queue)
		current := queue[0]
		queue = queue[1:]
		if current.node == req.To && len(current.edges) > 0 {
			return buildScoredRoute(current.edges, amount, current.fee, current.cost)
		}
		if len(current.edges) >= policy.MaxHops {
			continue
		}
		for _, edge := range candidates {
			edge = edge.Normalize()
			if edge.From != current.node || routeContainsNode(current.edges, edge.To) {
				continue
			}
			weight, fee, err := routeEdgeWeight(store, edge, amount, req.CurrentHeight, policy)
			if err != nil {
				return ScoredRoute{}, err
			}
			nextCost := current.cost.Add(weight)
			nextFee := current.fee.Add(fee)
			if policy.MaxFeeAmount != "" {
				maxFee, err := parseNonNegativeInt("payments route policy max fee", policy.MaxFeeAmount)
				if err != nil {
					return ScoredRoute{}, err
				}
				if nextFee.GT(maxFee) {
					continue
				}
			}
			nextEdges := append([]ChannelEdge(nil), current.edges...)
			nextEdges = append(nextEdges, edge)
			if best, found := bestByNode[edge.To]; found && !nextCost.LT(best) {
				continue
			}
			bestByNode[edge.To] = nextCost
			queue = append(queue, routeSearchPath{node: edge.To, edges: nextEdges, cost: nextCost, fee: nextFee})
		}
	}
	return ScoredRoute{}, errors.New("payments scored route not found")
}

func candidateRoutingEdges(state PaymentsState, store TopologyStore, amount sdkmath.Int, currentHeight uint64, policy RoutePolicy) []ChannelEdge {
	combined := make([]ChannelEdge, 0, len(state.Edges)+len(store.Edges))
	for _, edge := range state.Edges {
		combined = upsertTopologyEdge(combined, edge)
	}
	for _, edge := range store.Edges {
		combined = upsertTopologyEdge(combined, edge)
	}
	active := activeEdgesForAmount(combined, amount, currentHeight)
	out := make([]ChannelEdge, 0, len(active))
	for _, edge := range active {
		edge = edge.Normalize()
		if routePolicyExcludesEdge(policy, edge) {
			continue
		}
		if !edgeEffectiveCapacityCovers(policy, edge, amount) {
			continue
		}
		channel, found := state.ChannelByID(edge.ChannelID)
		if !found || channel.Status != ChannelStatusOpen {
			continue
		}
		if !containsString(channel.Participants, edge.From) || !containsString(channel.Participants, edge.To) {
			continue
		}
		out = append(out, edge)
	}
	sortEdges(out)
	return out
}

func routeEdgeWeight(store TopologyStore, edge ChannelEdge, amount sdkmath.Int, currentHeight uint64, policy RoutePolicy) (sdkmath.Int, sdkmath.Int, error) {
	fee, err := parseNonNegativeInt("payments route edge fee", edge.FeeAmount)
	if err != nil {
		return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
	}
	if policy.ProportionalFeeBps > 0 {
		fee = fee.Add(amount.Mul(sdkmath.NewInt(int64(policy.ProportionalFeeBps))).Quo(sdkmath.NewInt(10_000)))
	}
	cost := fee
	for _, penaltyText := range []string{policy.HopPenalty} {
		penalty, err := parseNonNegativeInt("payments route fixed penalty", penaltyText)
		if err != nil {
			return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
		}
		cost = cost.Add(penalty)
	}
	stats, hasStats := routeStatsForEdge(policy, edge)
	if hasStats {
		if stats.CongestionBps > 0 {
			cost = cost.Add(routeScaledPenalty(policy.CongestionPenalty, stats.CongestionBps))
		}
		if stats.FailureCount > 0 {
			penalty, err := parseNonNegativeInt("payments route failure penalty", policy.FailurePenalty)
			if err != nil {
				return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
			}
			cost = cost.Add(penalty.Mul(sdkmath.NewInt(int64(stats.FailureCount))))
		}
		if stats.SuccessRateBps > 0 && stats.SuccessRateBps < 10_000 {
			cost = cost.Add(routeScaledPenalty(policy.SuccessPenalty, 10_000-stats.SuccessRateBps))
		}
		if stats.NodeAvailabilityBps > 0 && stats.NodeAvailabilityBps < 10_000 {
			cost = cost.Add(routeScaledPenalty(policy.AvailabilityPenalty, 10_000-stats.NodeAvailabilityBps))
		}
		if stats.LiquidityUpdatedHeight > 0 && currentHeight > stats.LiquidityUpdatedHeight+policy.StaleLiquidityAfter {
			penalty, err := parseNonNegativeInt("payments stale liquidity penalty", policy.StaleLiquidityPenalty)
			if err != nil {
				return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
			}
			cost = cost.Add(penalty)
		}
		if stats.TimeoutMargin > 0 && stats.TimeoutMargin < policy.RequiredTimeoutMargin {
			penalty, err := parseNonNegativeInt("payments timeout margin penalty", policy.TimeoutPenalty)
			if err != nil {
				return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
			}
			cost = cost.Add(penalty)
		}
		if stats.ReservePressureBps > 0 {
			cost = cost.Add(routeScaledPenalty(policy.ReservePressurePenalty, stats.ReservePressureBps))
		}
		if stats.PendingConditionCount > 0 {
			penalty, err := parseNonNegativeInt("payments pending condition penalty", policy.PendingConditionPenalty)
			if err != nil {
				return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
			}
			cost = cost.Add(penalty.Mul(sdkmath.NewInt(int64(stats.PendingConditionCount))))
		}
		if stats.AvgResolutionLatency > 0 {
			penalty, err := parseNonNegativeInt("payments condition latency penalty", policy.LatencyPenalty)
			if err != nil {
				return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
			}
			cost = cost.Add(penalty.Mul(sdkmath.NewInt(int64(stats.AvgResolutionLatency))).Quo(sdkmath.NewInt(100)))
		}
		if stats.RetryCount > 0 {
			penalty, err := parseNonNegativeInt("payments route retry penalty", policy.FailurePenalty)
			if err != nil {
				return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
			}
			cost = cost.Add(penalty.Mul(sdkmath.NewInt(int64(stats.RetryCount))))
		}
		if stats.NodeQueueDelay > 0 {
			penalty, err := parseNonNegativeInt("payments node queue delay penalty", policy.QueueDelayPenalty)
			if err != nil {
				return sdkmath.ZeroInt(), sdkmath.ZeroInt(), err
			}
			cost = cost.Add(penalty.Mul(sdkmath.NewInt(int64(stats.NodeQueueDelay))).Quo(sdkmath.NewInt(100)))
		}
	}
	reputation := RoutingScoreForEdge(store, edge)
	if reputation < 0 {
		cost = cost.Add(sdkmath.NewInt(-reputation))
	}
	return cost, fee, nil
}

func routeScaledPenalty(penaltyText string, multiplier uint32) sdkmath.Int {
	penalty, err := parseNonNegativeInt("payments route scaled penalty", penaltyText)
	if err != nil {
		return sdkmath.ZeroInt()
	}
	return penalty.Mul(sdkmath.NewInt(int64(multiplier))).Quo(sdkmath.NewInt(10_000))
}

func routePolicyExcludesEdge(policy RoutePolicy, edge ChannelEdge) bool {
	policy = policy.Normalize()
	edge = edge.Normalize()
	if containsString(policy.ExcludedChannels, edge.ChannelID) {
		return true
	}
	return containsString(policy.ExcludedNodes, edge.From) || containsString(policy.ExcludedNodes, edge.To)
}

func routeStatsForEdge(policy RoutePolicy, edge ChannelEdge) (EdgeRoutingStats, bool) {
	key := routeStatsKey(EdgeRoutingStats{ChannelID: edge.ChannelID, From: edge.From, To: edge.To})
	for _, stats := range policy.Normalize().EdgeStats {
		if routeStatsKey(stats) == key {
			return stats.Normalize(), true
		}
	}
	return EdgeRoutingStats{}, false
}

func edgeEffectiveCapacityCovers(policy RoutePolicy, edge ChannelEdge, amount sdkmath.Int) bool {
	edge = edge.Normalize()
	capacity, err := parsePositiveInt("payments congested edge capacity", edge.Capacity)
	if err != nil {
		return false
	}
	stats, found := routeStatsForEdge(policy, edge)
	if !found {
		return !capacity.LT(amount)
	}
	reductionBps := uint32Max(stats.CongestionBps, stats.ReservePressureBps)
	if stats.PendingConditionCount > 0 {
		reductionBps = uint32Max(reductionBps, uint32Min(10_000, stats.PendingConditionCount*500))
	}
	if reductionBps == 0 {
		return !capacity.LT(amount)
	}
	allowedBps := uint32(10_000 - reductionBps)
	maxCongestedBps := policy.Normalize().MaxCongestedPaymentBps
	if maxCongestedBps > 0 && allowedBps > maxCongestedBps {
		allowedBps = maxCongestedBps
	}
	effective := capacity.Mul(sdkmath.NewInt(int64(allowedBps))).Quo(sdkmath.NewInt(10_000))
	return !effective.LT(amount)
}

func routeStatsKey(stats EdgeRoutingStats) string {
	stats = stats.Normalize()
	return fmt.Sprintf("%s/%s/%s", stats.ChannelID, stats.From, stats.To)
}

func upsertRouteStats(stats []EdgeRoutingStats, next EdgeRoutingStats) []EdgeRoutingStats {
	next = next.Normalize()
	key := routeStatsKey(next)
	out := make([]EdgeRoutingStats, 0, len(stats)+1)
	replaced := false
	for _, existing := range stats {
		existing = existing.Normalize()
		if routeStatsKey(existing) == key {
			out = append(out, next)
			replaced = true
			continue
		}
		out = append(out, existing)
	}
	if !replaced {
		out = append(out, next)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return routeStatsKey(out[i]) < routeStatsKey(out[j])
	})
	return out
}

func decayEdgeRoutingStats(stats EdgeRoutingStats, currentHeight, halfLife uint64) EdgeRoutingStats {
	stats = stats.Normalize()
	if halfLife == 0 || stats.LastUpdatedHeight == 0 || currentHeight <= stats.LastUpdatedHeight {
		return stats
	}
	periods := (currentHeight - stats.LastUpdatedHeight) / halfLife
	if periods == 0 {
		return stats
	}
	for ; periods > 0; periods-- {
		stats.CongestionBps /= 2
		stats.FailureCount /= 2
		stats.PendingConditionCount /= 2
		stats.AvgResolutionLatency /= 2
		stats.RetryCount /= 2
		stats.ReservePressureBps /= 2
		stats.NodeQueueDelay /= 2
		if stats.NodeAvailabilityBps < 10_000 {
			stats.NodeAvailabilityBps += (10_000 - stats.NodeAvailabilityBps) / 2
		}
		if stats.SuccessRateBps < 10_000 {
			stats.SuccessRateBps += (10_000 - stats.SuccessRateBps) / 2
		}
	}
	stats.LastUpdatedHeight = currentHeight
	return stats.Normalize()
}

func routeFailureKey(report RouteFailureReport) string {
	report = report.Normalize()
	return fmt.Sprintf("%s/%s/%s/%s/%020d", report.ChannelID, report.From, report.To, report.FailureClass, report.ObservedHeight)
}

func routePolicyHash(policy RoutePolicy) string {
	policy = policy.Normalize()
	parts := []string{"route-policy", fmt.Sprintf("%d", policy.MaxHops), fmt.Sprintf("%020d", policy.RequiredTimeoutMargin), fmt.Sprintf("%020d", policy.StaleLiquidityAfter)}
	for _, channelID := range policy.ExcludedChannels {
		parts = append(parts, "excluded-channel", channelID)
	}
	for _, node := range policy.ExcludedNodes {
		parts = append(parts, "excluded-node", node)
	}
	for _, stats := range policy.EdgeStats {
		stats = stats.Normalize()
		parts = append(parts,
			"stats",
			routeStatsKey(stats),
			fmt.Sprintf("%d", stats.SuccessRateBps),
			fmt.Sprintf("%d", stats.CongestionBps),
			fmt.Sprintf("%d", stats.FailureCount),
			fmt.Sprintf("%d", stats.PendingConditionCount),
			fmt.Sprintf("%020d", stats.LastUpdatedHeight),
		)
	}
	return HashParts(parts...)
}

func routeContainsNode(edges []ChannelEdge, node string) bool {
	node = strings.TrimSpace(node)
	for _, edge := range edges {
		edge = edge.Normalize()
		if edge.From == node || edge.To == node {
			return true
		}
	}
	return false
}

func routePathKey(edges []ChannelEdge) string {
	if len(edges) == 0 {
		return ""
	}
	parts := make([]string, 0, len(edges))
	for _, edge := range edges {
		parts = append(parts, edgeKey(edge.Normalize()))
	}
	return strings.Join(parts, "|")
}

func buildScoredRoute(edges []ChannelEdge, amount, totalFee, totalCost sdkmath.Int) (ScoredRoute, error) {
	if len(edges) == 0 {
		return ScoredRoute{}, errors.New("payments scored route requires edges")
	}
	minCapacity, err := parsePositiveInt("payments scored route capacity", edges[0].Capacity)
	if err != nil {
		return ScoredRoute{}, err
	}
	parts := []string{"scored-route", amount.String(), totalFee.String(), totalCost.String()}
	for _, edge := range edges {
		edge = edge.Normalize()
		capacity, err := parsePositiveInt("payments scored route capacity", edge.Capacity)
		if err != nil {
			return ScoredRoute{}, err
		}
		if capacity.LT(minCapacity) {
			minCapacity = capacity
		}
		parts = append(parts, edgeKey(edge), edge.Capacity, edge.FeeAmount, fmt.Sprintf("%020d", edge.ExpiresHeight))
	}
	route := ScoredRoute{
		Edges:       append([]ChannelEdge(nil), edges...),
		Amount:      amount.String(),
		TotalFee:    totalFee.String(),
		TotalCost:   totalCost.String(),
		MinCapacity: minCapacity.String(),
	}
	route.ScoreHash = HashParts(parts...)
	route = route.Normalize()
	return route, route.Validate()
}

func buildMultiPathRoute(parts []ScoredRoute) (MultiPathRoute, error) {
	if len(parts) == 0 {
		return MultiPathRoute{}, errors.New("payments multipath route requires parts")
	}
	totalAmount := sdkmath.ZeroInt()
	totalFee := sdkmath.ZeroInt()
	hashParts := []string{"multipath-route"}
	for _, part := range parts {
		part = part.Normalize()
		if err := part.Validate(); err != nil {
			return MultiPathRoute{}, err
		}
		amount, err := parsePositiveInt("payments multipath part amount", part.Amount)
		if err != nil {
			return MultiPathRoute{}, err
		}
		fee, err := parseNonNegativeInt("payments multipath part fee", part.TotalFee)
		if err != nil {
			return MultiPathRoute{}, err
		}
		totalAmount = totalAmount.Add(amount)
		totalFee = totalFee.Add(fee)
		hashParts = append(hashParts, part.ScoreHash)
	}
	out := MultiPathRoute{
		Parts:       append([]ScoredRoute(nil), parts...),
		TotalAmount: totalAmount.String(),
		TotalFee:    totalFee.String(),
		ScoreHash:   HashParts(hashParts...),
	}
	return out, nil
}
