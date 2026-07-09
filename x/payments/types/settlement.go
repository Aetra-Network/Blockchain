package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type BatchOperationType string

type SettlementArbitrationOperation string

type FinalSettlementRequest struct {
	ChannelID           string
	ResolvedConditions  []ConditionResolution
	CurrentHeight       uint64
	FeeAccountingState  string
	RoutingFeeClaimHash string
}

type SettlementArbitrationInput struct {
	Operation         SettlementArbitrationOperation
	ChannelID         string
	SignedState       ChannelState
	Claim             UnidirectionalClaim
	FraudProof        FraudProof
	ConditionProofs   []ConditionResolution
	RouteHints        []ChannelEdge
	GossipStateHash   string
	ExternalLiquidity []Balance
	UnsignedBalances  []Balance
	OffchainIntent    string
	CurrentHeight     uint64
}

type SettlementRecord struct {
	ChainID            string
	ChannelID          string
	StateHash          string
	Nonce              uint64
	FinalBalances      []Balance
	SettlementFeeDenom string
	SettlementFee      string
	Penalties          []Penalty
	PenaltyAllocations []PenaltyAllocation
	SettledHeight      uint64
	SettlementHash     string
}

type CustodyLock struct {
	ChannelID string
	Denom     string
	Amount    string
}

type SettlementOperation struct {
	OperationID   string
	OperationType BatchOperationType
	ChannelID     string
	Nonce         uint64
	StateHash     string
}

type SettlementBatch struct {
	BatchID    string
	Operations []SettlementOperation
	RootHash   string
}

func (r FinalSettlementRequest) Normalize() FinalSettlementRequest {
	r.ChannelID = normalizeHash(r.ChannelID)
	r.ResolvedConditions = normalizeConditionResolutions(r.ResolvedConditions)
	r.FeeAccountingState = strings.TrimSpace(r.FeeAccountingState)
	r.RoutingFeeClaimHash = normalizeOptionalHash(r.RoutingFeeClaimHash)
	return r
}

func (i SettlementArbitrationInput) Normalize() SettlementArbitrationInput {
	i.ChannelID = normalizeHash(i.ChannelID)
	i.SignedState = i.SignedState.Normalize()
	i.Claim = i.Claim.Normalize()
	i.FraudProof = i.FraudProof.Normalize()
	i.ConditionProofs = normalizeConditionResolutions(i.ConditionProofs)
	for index := range i.RouteHints {
		i.RouteHints[index] = i.RouteHints[index].Normalize()
	}
	i.GossipStateHash = normalizeOptionalHash(i.GossipStateHash)
	i.ExternalLiquidity = normalizeBalances(i.ExternalLiquidity)
	i.UnsignedBalances = normalizeBalances(i.UnsignedBalances)
	i.OffchainIntent = strings.TrimSpace(i.OffchainIntent)
	return i
}

func (i SettlementArbitrationInput) ValidateForChannel(channel ChannelRecord) error {
	input := i.Normalize()
	channel = channel.Normalize()
	if err := channel.ValidateCore(); err != nil {
		return err
	}
	if input.ChannelID != channel.ChannelID {
		return errors.New("payments settlement arbitration channel mismatch")
	}
	if !IsSettlementArbitrationOperation(input.Operation) {
		return fmt.Errorf("unknown payments settlement arbitration operation %q", input.Operation)
	}
	if len(input.RouteHints) > 0 {
		return errors.New("payments settlement contract must not select payment routes")
	}
	if input.GossipStateHash != "" {
		return errors.New("payments settlement contract must not trust gossip state")
	}
	if len(input.ExternalLiquidity) > 0 {
		return errors.New("payments settlement contract must not depend on external liquidity reports")
	}
	if len(input.UnsignedBalances) > 0 {
		return errors.New("payments settlement contract must not accept unsigned balance updates")
	}
	if input.OffchainIntent != "" {
		return errors.New("payments settlement contract must not infer participant intent from unsigned off-chain messages")
	}
	if input.CurrentHeight == 0 && operationRequiresHeight(input.Operation) {
		return errors.New("payments settlement arbitration height must be positive")
	}
	if input.Operation == SettlementArbitrationCollateralCustody {
		return validateSettlementCustody(channel)
	}
	if input.Operation == SettlementArbitrationFraudProof {
		return input.FraudProof.ValidateForChannel(channel)
	}
	if input.Operation == SettlementArbitrationReplayProtection {
		return validateSettlementReplayProtection(channel, input.SignedState)
	}
	if input.Operation == SettlementArbitrationPenaltyRouting {
		if input.FraudProof.ProofID == "" {
			return errors.New("payments settlement penalty routing requires accepted fraud proof")
		}
		return input.FraudProof.ValidateForChannel(channel)
	}
	if input.Operation == SettlementArbitrationConditionResolution {
		if err := validateSettlementSignedState(channel, input.SignedState, false); err != nil {
			return err
		}
		return validateConditionResolutionsForState(input.SignedState, channel, input.ConditionProofs, true)
	}
	if input.Operation == SettlementArbitrationOpen {
		if err := validateSettlementCustody(channel); err != nil {
			return err
		}
		return validateSettlementSignedState(channel, channel.LatestState, true)
	}
	if input.Operation == SettlementArbitrationUnilateralClose && !input.Claim.IsZero() {
		if err := input.Claim.ValidateForChannel(channel); err != nil {
			return err
		}
		return validateSettlementClaimReplayProtection(channel, input.Claim)
	}
	requireAll := input.Operation == SettlementArbitrationCooperativeClose
	if err := validateSettlementSignedState(channel, input.SignedState, requireAll); err != nil {
		return err
	}
	return validateSettlementReplayProtection(channel, input.SignedState)
}

func IsSettlementArbitrationOperation(operation SettlementArbitrationOperation) bool {
	switch operation {
	case SettlementArbitrationOpen,
		SettlementArbitrationCollateralCustody,
		SettlementArbitrationCooperativeClose,
		SettlementArbitrationUnilateralClose,
		SettlementArbitrationDispute,
		SettlementArbitrationFraudProof,
		SettlementArbitrationConditionResolution,
		SettlementArbitrationPenaltyRouting,
		SettlementArbitrationFinalSettlement,
		SettlementArbitrationReplayProtection:
		return true
	default:
		return false
	}
}

func operationRequiresHeight(operation SettlementArbitrationOperation) bool {
	switch operation {
	case SettlementArbitrationUnilateralClose,
		SettlementArbitrationDispute,
		SettlementArbitrationFraudProof,
		SettlementArbitrationFinalSettlement,
		SettlementArbitrationReplayProtection:
		return true
	default:
		return false
	}
}

func validateSettlementCustody(channel ChannelRecord) error {
	if channel.Denom != NativeDenom || channel.CustodyDenom != NativeDenom {
		return fmt.Errorf("payments settlement custody must use %s", NativeDenom)
	}
	if channel.Collateral == "" || channel.CustodyAmount == "" {
		return errors.New("payments settlement custody amount is required")
	}
	if channel.CustodyAmount != channel.Collateral {
		return errors.New("payments settlement custody must equal locked collateral")
	}
	return validatePositiveInt("payments settlement custody amount", channel.CustodyAmount)
}

func validateSettlementSignedState(channel ChannelRecord, signedState ChannelState, requireAllParticipants bool) error {
	state := signedState.Normalize()
	if state.StateHash == "" {
		return errors.New("payments settlement arbitration signed state is required")
	}
	return state.ValidateForChannel(channel, requireAllParticipants)
}

func validateSettlementReplayProtection(channel ChannelRecord, signedState ChannelState) error {
	state := signedState.Normalize()
	if state.StateHash == "" {
		return errors.New("payments settlement replay protection signed state is required")
	}
	if state.Nonce < channel.FinalizedNonce {
		return errors.New("payments settlement replay state nonce is below finalized nonce")
	}
	if channel.Status == ChannelStatusSettled && state.Nonce <= channel.FinalizedNonce {
		return errors.New("payments settlement replay state targets closed channel")
	}
	return state.ValidateForChannel(channel, false)
}

func validateSettlementClaimReplayProtection(channel ChannelRecord, claim UnidirectionalClaim) error {
	claim = claim.Normalize()
	if claim.StateHash == "" {
		return errors.New("payments settlement replay protection signed claim is required")
	}
	if claim.Nonce < channel.FinalizedNonce {
		return errors.New("payments settlement replay claim nonce is below finalized nonce")
	}
	if channel.Status == ChannelStatusSettled && claim.Nonce <= channel.FinalizedNonce {
		return errors.New("payments settlement replay claim targets closed channel")
	}
	return claim.ValidateForChannel(channel)
}

func (s SettlementRecord) Normalize() SettlementRecord {
	s.ChainID = strings.TrimSpace(s.ChainID)
	s.ChannelID = normalizeHash(s.ChannelID)
	s.StateHash = normalizeHash(s.StateHash)
	s.SettlementFeeDenom = normalizeAssetDenom(s.SettlementFeeDenom)
	s.SettlementFee = strings.TrimSpace(s.SettlementFee)
	s.SettlementHash = normalizeOptionalHash(s.SettlementHash)
	s.FinalBalances = normalizeBalances(s.FinalBalances)
	s.Penalties = normalizePenalties(s.Penalties)
	s.PenaltyAllocations = normalizePenaltyAllocations(s.PenaltyAllocations)
	return s
}

func (s SettlementRecord) ValidateForChannel(channel ChannelRecord) error {
	settlement := s.Normalize()
	channel = channel.Normalize()
	if settlement.ChainID != channel.ChainID {
		return errors.New("payments settlement chain id mismatch")
	}
	if settlement.ChannelID != channel.ChannelID {
		return errors.New("payments settlement channel mismatch")
	}
	if err := ValidateHash("payments settlement state hash", settlement.StateHash); err != nil {
		return err
	}
	if settlement.Nonce == 0 {
		return errors.New("payments settlement nonce must be positive")
	}
	if settlement.SettledHeight == 0 {
		return errors.New("payments settlement height must be positive")
	}
	if settlement.SettlementFeeDenom != NativeDenom {
		return fmt.Errorf("payments settlement fee denom must be %s", NativeDenom)
	}
	if err := validateNonNegativeInt("payments settlement fee", settlement.SettlementFee); err != nil {
		return err
	}
	for _, balance := range settlement.FinalBalances {
		if !containsString(channel.Participants, balance.Participant) {
			return errors.New("payments settlement balance participant must be in channel")
		}
	}
	for _, penalty := range settlement.Penalties {
		if err := penalty.ValidateForChannel(channel); err != nil {
			return err
		}
	}
	for _, allocation := range settlement.PenaltyAllocations {
		if err := allocation.ValidateForChannel(channel); err != nil {
			return err
		}
	}
	finalTotal, err := sumBalances(settlement.FinalBalances)
	if err != nil {
		return err
	}
	fee, err := parseNonNegativeInt("payments settlement fee", settlement.SettlementFee)
	if err != nil {
		return err
	}
	collateral, err := parsePositiveInt("payments channel collateral", channel.Collateral)
	if err != nil {
		return err
	}
	allocationTotal, err := sumPenaltyAllocations(settlement.PenaltyAllocations)
	if err != nil {
		return err
	}
	if !finalTotal.Add(fee).Add(allocationTotal).Equal(collateral) {
		return errors.New("payments settlement must conserve collateral minus fee and routed penalties")
	}
	if settlement.SettlementHash == "" {
		return errors.New("payments settlement hash is required")
	}
	if expected := ComputeSettlementHash(settlement); settlement.SettlementHash != expected {
		return errors.New("payments settlement hash mismatch")
	}
	return nil
}

func (c CustodyLock) Normalize() CustodyLock {
	c.ChannelID = normalizeHash(c.ChannelID)
	c.Denom = normalizeAssetDenom(c.Denom)
	c.Amount = strings.TrimSpace(c.Amount)
	return c
}

func (c CustodyLock) ValidateForChannel(channel ChannelRecord) error {
	lock := c.Normalize()
	if lock.ChannelID != channel.Normalize().ChannelID {
		return errors.New("payments custody channel mismatch")
	}
	if lock.Denom != NativeDenom {
		return fmt.Errorf("payments custody denom must be %s", NativeDenom)
	}
	locked, err := parsePositiveInt("payments custody amount", lock.Amount)
	if err != nil {
		return err
	}
	collateral, err := parsePositiveInt("payments channel collateral", channel.Collateral)
	if err != nil {
		return err
	}
	if !locked.Equal(collateral) {
		return errors.New("payments custody amount must match channel collateral")
	}
	return nil
}

func (op SettlementOperation) Normalize() SettlementOperation {
	op.OperationID = normalizeHash(op.OperationID)
	op.ChannelID = normalizeHash(op.ChannelID)
	op.StateHash = normalizeHash(op.StateHash)
	return op
}

func (op SettlementOperation) Validate() error {
	op = op.Normalize()
	if err := ValidateHash("payments settlement operation id", op.OperationID); err != nil {
		return err
	}
	if !IsBatchOperationType(op.OperationType) {
		return fmt.Errorf("unknown payments batch operation type %q", op.OperationType)
	}
	if err := ValidateHash("payments settlement operation channel id", op.ChannelID); err != nil {
		return err
	}
	if op.Nonce == 0 {
		return errors.New("payments settlement operation nonce must be positive")
	}
	return ValidateHash("payments settlement operation state hash", op.StateHash)
}

func (b SettlementBatch) Normalize() SettlementBatch {
	b.BatchID = normalizeHash(b.BatchID)
	b.RootHash = normalizeOptionalHash(b.RootHash)
	b.Operations = SortSettlementOperations(b.Operations)
	return b
}

func (b SettlementBatch) Validate() error {
	batch := b.Normalize()
	if err := ValidateHash("payments settlement batch id", batch.BatchID); err != nil {
		return err
	}
	if len(batch.Operations) == 0 || len(batch.Operations) > MaxSettlementBatchOps {
		return fmt.Errorf("payments settlement batch operations must be between 1 and %d", MaxSettlementBatchOps)
	}
	seenOps := make(map[string]struct{}, len(batch.Operations))
	seenChannels := make(map[string]struct{}, len(batch.Operations))
	for i, op := range batch.Operations {
		if err := op.Validate(); err != nil {
			return err
		}
		if _, found := seenOps[op.OperationID]; found {
			return errors.New("payments duplicate settlement batch operation")
		}
		seenOps[op.OperationID] = struct{}{}
		if _, found := seenChannels[op.ChannelID]; found {
			return errors.New("payments settlement batch must contain independent channels")
		}
		seenChannels[op.ChannelID] = struct{}{}
		if i > 0 && compareSettlementOperations(batch.Operations[i-1], op) >= 0 {
			return errors.New("payments settlement batch operations must be sorted canonically")
		}
	}
	if batch.RootHash == "" {
		return errors.New("payments settlement batch root is required")
	}
	if expected := ComputeBatchRoot(batch.Operations); batch.RootHash != expected {
		return errors.New("payments settlement batch root mismatch")
	}
	return nil
}

func NewSettlementBatch(batchID string, operations []SettlementOperation) (SettlementBatch, error) {
	batch := SettlementBatch{
		BatchID:    normalizeHash(batchID),
		Operations: SortSettlementOperations(operations),
	}
	batch.RootHash = ComputeBatchRoot(batch.Operations)
	if err := batch.Validate(); err != nil {
		return SettlementBatch{}, err
	}
	return batch, nil
}

func GroupSettlementOperationsByChannelKey(seed string, operations []SettlementOperation) ([]SettlementBatch, error) {
	operations = SortSettlementOperations(operations)
	if len(operations) == 0 {
		return nil, errors.New("payments settlement batch grouping requires operations")
	}
	groups := [][]SettlementOperation{}
	groupChannels := []map[string]struct{}{}
	for _, op := range operations {
		if err := op.Validate(); err != nil {
			return nil, err
		}
		placed := false
		for i := range groups {
			if _, found := groupChannels[i][op.ChannelID]; found {
				continue
			}
			groups[i] = append(groups[i], op)
			groupChannels[i][op.ChannelID] = struct{}{}
			placed = true
			break
		}
		if !placed {
			groups = append(groups, []SettlementOperation{op})
			groupChannels = append(groupChannels, map[string]struct{}{op.ChannelID: {}})
		}
	}
	out := make([]SettlementBatch, 0, len(groups))
	seed = strings.TrimSpace(seed)
	if seed == "" {
		seed = "settlement-batch-group"
	}
	for i, group := range groups {
		batchID := HashParts(seed, fmt.Sprintf("%020d", uint64(i)))
		batch, err := NewSettlementBatch(batchID, group)
		if err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, nil
}

func SortSettlementOperations(operations []SettlementOperation) []SettlementOperation {
	out := make([]SettlementOperation, len(operations))
	for i, op := range operations {
		out[i] = op.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compareSettlementOperations(out[i], out[j]) < 0
	})
	return out
}

func IsBatchOperationType(value BatchOperationType) bool {
	switch value {
	case BatchOperationOpen, BatchOperationClose, BatchOperationDispute, BatchOperationSettle:
		return true
	default:
		return false
	}
}

func compareSettlementOperations(left, right SettlementOperation) int {
	if left.ChannelID != right.ChannelID {
		return compareString(left.ChannelID, right.ChannelID)
	}
	if left.OperationType != right.OperationType {
		return compareString(string(left.OperationType), string(right.OperationType))
	}
	if left.Nonce < right.Nonce {
		return -1
	}
	if left.Nonce > right.Nonce {
		return 1
	}
	return compareString(left.OperationID, right.OperationID)
}
