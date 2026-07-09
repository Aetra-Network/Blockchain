package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type BlockSTMTransactionClass string

const (
	BlockSTMClassOpenChannel       BlockSTMTransactionClass = "OPEN_CHANNEL"
	BlockSTMClassUpdateCheckpoint  BlockSTMTransactionClass = "UPDATE_CHECKPOINT"
	BlockSTMClassCloseChannel      BlockSTMTransactionClass = "CLOSE_CHANNEL"
	BlockSTMClassDisputeChannel    BlockSTMTransactionClass = "DISPUTE_CHANNEL"
	BlockSTMClassSettleChannel     BlockSTMTransactionClass = "SETTLE_CHANNEL"
	BlockSTMClassResolveCondition  BlockSTMTransactionClass = "RESOLVE_CONDITION"
	BlockSTMClassBatchConditions   BlockSTMTransactionClass = "BATCH_CONDITIONS"
	BlockSTMClassPenaltyAccounting BlockSTMTransactionClass = "PENALTY_ACCOUNTING"
)

type BlockSTMAccessPlan struct {
	OperationID        string
	TxClass            BlockSTMTransactionClass
	ChannelID          string
	ConditionIDs       []string
	ReadKeys           []string
	WriteKeys          []string
	AccumulatorKeys    []string
	ConflictDomain     string
	DeterministicGroup string
}

type BlockSTMConflict struct {
	LeftOperationID  string
	RightOperationID string
	Key              string
	Reason           string
}

type BlockSTMConflictProfile struct {
	Plans                    []BlockSTMAccessPlan
	Conflicts                []BlockSTMConflict
	ParallelizableGroups     [][]string
	ConflictFree             bool
	GlobalAccountingDeferred bool
}

type PaymentBlockAccumulator struct {
	BlockHeight    uint64
	FeeAmount      string
	BurnAmount     string
	PenaltyAmount  string
	OperationCount uint64
	AccumulatorKey string
}

func AccessPlanForSettlementOperation(op SettlementOperation, blockHeight uint64) (BlockSTMAccessPlan, error) {
	op = op.Normalize()
	if err := op.Validate(); err != nil {
		return BlockSTMAccessPlan{}, err
	}
	txClass := blockSTMClassForBatchOperation(op.OperationType)
	plan := BlockSTMAccessPlan{
		OperationID:        op.OperationID,
		TxClass:            txClass,
		ChannelID:          op.ChannelID,
		ReadKeys:           []string{PaymentChannelKey(op.ChannelID)},
		WriteKeys:          []string{PaymentChannelKey(op.ChannelID)},
		AccumulatorKeys:    []string{PaymentBlockAccumulatorKey(blockHeight)},
		ConflictDomain:     PaymentChannelKey(op.ChannelID),
		DeterministicGroup: PaymentChannelKey(op.ChannelID),
	}
	switch op.OperationType {
	case BatchOperationOpen:
		plan.WriteKeys = append(plan.WriteKeys, PaymentCustodyKey(op.ChannelID))
	case BatchOperationClose, BatchOperationDispute:
		plan.WriteKeys = append(plan.WriteKeys, PaymentPendingCloseIndexKey(op.ChannelID))
	case BatchOperationSettle:
		plan.WriteKeys = append(plan.WriteKeys, PaymentSettlementKey(op.ChannelID), PaymentSettlementTombstoneKey(op.ChannelID), PaymentCustodyKey(op.ChannelID))
	}
	return plan.Normalize(), nil
}

func AccessPlanForConditionResolution(channelID string, conditionIDs []string, blockHeight uint64) (BlockSTMAccessPlan, error) {
	channelID = normalizeHash(channelID)
	if err := ValidateHash("payments blockstm condition channel id", channelID); err != nil {
		return BlockSTMAccessPlan{}, err
	}
	conditionIDs = normalizeHashSlice(conditionIDs)
	if len(conditionIDs) == 0 {
		return BlockSTMAccessPlan{}, errors.New("payments blockstm condition resolution requires condition ids")
	}
	readKeys := []string{PaymentChannelKey(channelID)}
	writeKeys := []string{PaymentChannelKey(channelID)}
	for _, conditionID := range conditionIDs {
		writeKeys = append(writeKeys, PaymentConditionIndexKey(channelID, conditionID))
	}
	plan := BlockSTMAccessPlan{
		OperationID:        HashParts("condition-resolution", channelID, strings.Join(conditionIDs, "/")),
		TxClass:            BlockSTMClassResolveCondition,
		ChannelID:          channelID,
		ConditionIDs:       conditionIDs,
		ReadKeys:           readKeys,
		WriteKeys:          writeKeys,
		AccumulatorKeys:    []string{PaymentBlockAccumulatorKey(blockHeight)},
		ConflictDomain:     PaymentChannelKey(channelID),
		DeterministicGroup: PaymentChannelKey(channelID),
	}
	return plan.Normalize(), nil
}

func ProfileBlockSTMConflicts(plans []BlockSTMAccessPlan) BlockSTMConflictProfile {
	normalized := make([]BlockSTMAccessPlan, len(plans))
	for i, plan := range plans {
		normalized[i] = plan.Normalize()
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].OperationID < normalized[j].OperationID
	})
	conflicts := []BlockSTMConflict{}
	for i := range normalized {
		for j := i + 1; j < len(normalized); j++ {
			for _, conflict := range blockSTMPlanConflicts(normalized[i], normalized[j]) {
				conflicts = append(conflicts, conflict)
			}
		}
	}
	return BlockSTMConflictProfile{
		Plans:                    normalized,
		Conflicts:                conflicts,
		ParallelizableGroups:     blockSTMParallelizableGroups(normalized),
		ConflictFree:             len(conflicts) == 0,
		GlobalAccountingDeferred: blockSTMAccountingDeferred(normalized),
	}
}

func AccumulatePaymentBlockAccounting(acc PaymentBlockAccumulator, settlement SettlementRecord) (PaymentBlockAccumulator, error) {
	acc = acc.Normalize()
	settlement = settlement.Normalize()
	fee, err := parseNonNegativeInt("payments block accumulator fee", acc.FeeAmount)
	if err != nil {
		return PaymentBlockAccumulator{}, err
	}
	burn, err := parseNonNegativeInt("payments block accumulator burn", acc.BurnAmount)
	if err != nil {
		return PaymentBlockAccumulator{}, err
	}
	penalty, err := parseNonNegativeInt("payments block accumulator penalty", acc.PenaltyAmount)
	if err != nil {
		return PaymentBlockAccumulator{}, err
	}
	settlementFee, err := parseNonNegativeInt("payments settlement fee", settlement.SettlementFee)
	if err != nil {
		return PaymentBlockAccumulator{}, err
	}
	penaltyTotal, err := sumPenaltyAllocations(settlement.PenaltyAllocations)
	if err != nil {
		return PaymentBlockAccumulator{}, err
	}
	acc.FeeAmount = fee.Add(settlementFee).String()
	acc.PenaltyAmount = penalty.Add(penaltyTotal).String()
	acc.BurnAmount = burn.String()
	acc.OperationCount++
	return acc.Normalize(), nil
}

func (p BlockSTMAccessPlan) Normalize() BlockSTMAccessPlan {
	p.OperationID = normalizeOptionalHash(p.OperationID)
	p.ChannelID = normalizeOptionalHash(p.ChannelID)
	p.ConditionIDs = normalizeHashSlice(p.ConditionIDs)
	p.ReadKeys = normalizeStoreKeySlice(p.ReadKeys)
	p.WriteKeys = normalizeStoreKeySlice(p.WriteKeys)
	p.AccumulatorKeys = normalizeStoreKeySlice(p.AccumulatorKeys)
	p.ConflictDomain = strings.TrimSpace(p.ConflictDomain)
	p.DeterministicGroup = strings.TrimSpace(p.DeterministicGroup)
	if p.ConflictDomain == "" && p.ChannelID != "" {
		p.ConflictDomain = PaymentChannelKey(p.ChannelID)
	}
	if p.DeterministicGroup == "" {
		p.DeterministicGroup = p.ConflictDomain
	}
	return p
}

func (p BlockSTMAccessPlan) Validate() error {
	p = p.Normalize()
	if err := ValidateHash("payments blockstm operation id", p.OperationID); err != nil {
		return err
	}
	if !IsBlockSTMTransactionClass(p.TxClass) {
		return fmt.Errorf("unknown payments blockstm transaction class %q", p.TxClass)
	}
	if p.ChannelID != "" {
		if err := ValidateHash("payments blockstm channel id", p.ChannelID); err != nil {
			return err
		}
	}
	if len(p.WriteKeys) == 0 {
		return errors.New("payments blockstm access plan requires write keys")
	}
	if p.ConflictDomain == "" {
		return errors.New("payments blockstm access plan requires conflict domain")
	}
	return nil
}

func (c BlockSTMConflict) Normalize() BlockSTMConflict {
	c.LeftOperationID = normalizeOptionalHash(c.LeftOperationID)
	c.RightOperationID = normalizeOptionalHash(c.RightOperationID)
	c.Key = strings.TrimSpace(c.Key)
	c.Reason = strings.TrimSpace(c.Reason)
	return c
}

func (a PaymentBlockAccumulator) Normalize() PaymentBlockAccumulator {
	a.FeeAmount = strings.TrimSpace(a.FeeAmount)
	if a.FeeAmount == "" {
		a.FeeAmount = "0"
	}
	a.BurnAmount = strings.TrimSpace(a.BurnAmount)
	if a.BurnAmount == "" {
		a.BurnAmount = "0"
	}
	a.PenaltyAmount = strings.TrimSpace(a.PenaltyAmount)
	if a.PenaltyAmount == "" {
		a.PenaltyAmount = "0"
	}
	if a.AccumulatorKey == "" && a.BlockHeight > 0 {
		a.AccumulatorKey = PaymentBlockAccumulatorKey(a.BlockHeight)
	}
	a.AccumulatorKey = strings.TrimSpace(a.AccumulatorKey)
	return a
}

func (a PaymentBlockAccumulator) Validate() error {
	a = a.Normalize()
	if a.BlockHeight == 0 {
		return errors.New("payments block accumulator height must be positive")
	}
	if a.AccumulatorKey != PaymentBlockAccumulatorKey(a.BlockHeight) {
		return errors.New("payments block accumulator key mismatch")
	}
	if err := validateNonNegativeInt("payments block accumulator fees", a.FeeAmount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments block accumulator burns", a.BurnAmount); err != nil {
		return err
	}
	return validateNonNegativeInt("payments block accumulator penalties", a.PenaltyAmount)
}

func blockSTMClassForBatchOperation(op BatchOperationType) BlockSTMTransactionClass {
	switch op {
	case BatchOperationOpen:
		return BlockSTMClassOpenChannel
	case BatchOperationClose:
		return BlockSTMClassCloseChannel
	case BatchOperationDispute:
		return BlockSTMClassDisputeChannel
	case BatchOperationSettle:
		return BlockSTMClassSettleChannel
	default:
		return ""
	}
}

func blockSTMPlanConflicts(left, right BlockSTMAccessPlan) []BlockSTMConflict {
	left = left.Normalize()
	right = right.Normalize()
	conflicts := []BlockSTMConflict{}
	leftWrites := setFromStrings(left.WriteKeys)
	rightWrites := setFromStrings(right.WriteKeys)
	for key := range leftWrites {
		if _, found := rightWrites[key]; found {
			conflicts = append(conflicts, BlockSTMConflict{
				LeftOperationID:  left.OperationID,
				RightOperationID: right.OperationID,
				Key:              key,
				Reason:           "write/write",
			}.Normalize())
		}
	}
	for _, key := range left.WriteKeys {
		if containsString(right.ReadKeys, key) && !containsString(left.AccumulatorKeys, key) && !containsString(right.AccumulatorKeys, key) {
			conflicts = append(conflicts, BlockSTMConflict{LeftOperationID: left.OperationID, RightOperationID: right.OperationID, Key: key, Reason: "write/read"}.Normalize())
		}
	}
	for _, key := range right.WriteKeys {
		if containsString(left.ReadKeys, key) && !containsString(left.AccumulatorKeys, key) && !containsString(right.AccumulatorKeys, key) {
			conflicts = append(conflicts, BlockSTMConflict{LeftOperationID: left.OperationID, RightOperationID: right.OperationID, Key: key, Reason: "read/write"}.Normalize())
		}
	}
	return conflicts
}

func blockSTMParallelizableGroups(plans []BlockSTMAccessPlan) [][]string {
	groups := [][]string{}
	groupDomains := []map[string]struct{}{}
	for _, plan := range plans {
		plan = plan.Normalize()
		placed := false
		for i := range groups {
			if _, found := groupDomains[i][plan.ConflictDomain]; found {
				continue
			}
			groups[i] = append(groups[i], plan.OperationID)
			groupDomains[i][plan.ConflictDomain] = struct{}{}
			placed = true
			break
		}
		if !placed {
			groups = append(groups, []string{plan.OperationID})
			groupDomains = append(groupDomains, map[string]struct{}{plan.ConflictDomain: {}})
		}
	}
	return groups
}

func blockSTMAccountingDeferred(plans []BlockSTMAccessPlan) bool {
	for _, plan := range plans {
		plan = plan.Normalize()
		for _, accKey := range plan.AccumulatorKeys {
			if containsString(plan.WriteKeys, accKey) {
				return false
			}
		}
	}
	return true
}

func IsBlockSTMTransactionClass(value BlockSTMTransactionClass) bool {
	switch value {
	case BlockSTMClassOpenChannel,
		BlockSTMClassUpdateCheckpoint,
		BlockSTMClassCloseChannel,
		BlockSTMClassDisputeChannel,
		BlockSTMClassSettleChannel,
		BlockSTMClassResolveCondition,
		BlockSTMClassBatchConditions,
		BlockSTMClassPenaltyAccounting:
		return true
	default:
		return false
	}
}
