package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"

	"github.com/sovereign-l1/l1/app/addressing"
)

type FraudProofType string

const (
	FraudProofTypeDoubleSign        FraudProofType = "DOUBLE_SIGN"
	FraudProofTypeStaleClose        FraudProofType = "STALE_CLOSE"
	FraudProofTypeInvalidClose      FraudProofType = "INVALID_CLOSE"
	FraudProofTypeInvalidBalance    FraudProofType = "INVALID_BALANCE"
	FraudProofTypeInvalidCondition  FraudProofType = "INVALID_CONDITION"
	FraudProofTypeReplayAttempt     FraudProofType = "REPLAY_ATTEMPT"
	FraudProofTypeAsyncOverexposure FraudProofType = "ASYNC_OVEREXPOSURE"
)

const (
	CloseReasonUnilateral  CloseReason = "UNILATERAL"
	CloseReasonCooperative CloseReason = "COOPERATIVE"
	CloseReasonTimeout     CloseReason = "TIMEOUT"
	CloseReasonFraud       CloseReason = "FRAUD"
)

const (
	SettlementArbitrationOpen                SettlementArbitrationOperation = "OPEN"
	SettlementArbitrationCollateralCustody   SettlementArbitrationOperation = "COLLATERAL_CUSTODY"
	SettlementArbitrationCooperativeClose    SettlementArbitrationOperation = "COOPERATIVE_CLOSE"
	SettlementArbitrationUnilateralClose     SettlementArbitrationOperation = "UNILATERAL_CLOSE"
	SettlementArbitrationDispute             SettlementArbitrationOperation = "DISPUTE"
	SettlementArbitrationFraudProof          SettlementArbitrationOperation = "FRAUD_PROOF"
	SettlementArbitrationConditionResolution SettlementArbitrationOperation = "CONDITION_RESOLUTION"
	SettlementArbitrationPenaltyRouting      SettlementArbitrationOperation = "PENALTY_ROUTING"
	SettlementArbitrationFinalSettlement     SettlementArbitrationOperation = "FINAL_SETTLEMENT"
	SettlementArbitrationReplayProtection    SettlementArbitrationOperation = "REPLAY_PROTECTION"
)

type PenaltyRoute string

const (
	PenaltyRouteReporter        PenaltyRoute = "REPORTER"
	PenaltyRouteCounterparty    PenaltyRoute = "COUNTERPARTY"
	PenaltyRouteBurn            PenaltyRoute = "BURN"
	PenaltyRouteSecurityReserve PenaltyRoute = "SECURITY_RESERVE"
	PenaltyRouteCommunityPool   PenaltyRoute = "COMMUNITY_POOL"
)

type PaymentPenaltyClass string

const (
	PenaltyClassInvalidClose      PaymentPenaltyClass = "INVALID_CLOSE_SUBMISSION"
	PenaltyClassStaleClose        PaymentPenaltyClass = "STALE_CLOSE"
	PenaltyClassDoubleSign        PaymentPenaltyClass = "SAME_NONCE_DOUBLE_SIGN"
	PenaltyClassInvalidCondition  PaymentPenaltyClass = "INVALID_CONDITION_CLAIM"
	PenaltyClassReplayAttempt     PaymentPenaltyClass = "REPLAY_ATTEMPT"
	PenaltyClassAsyncOverexposure PaymentPenaltyClass = "ASYNC_OVEREXPOSURE"
	PenaltyClassInvalidFraudProof PaymentPenaltyClass = "INVALID_FRAUD_PROOF"
)

type FraudProof struct {
	ProofID             string
	ProofType           FraudProofType
	SubmittedBy         string
	OffendingSigner     string
	StateA              ChannelState
	StateB              ChannelState
	AsyncProof          AsyncDeltaDisputeProof
	PenaltyDenom        string
	PenaltyAmount       string
	EvidenceHash        string
	VerificationFeePaid string
}

type Penalty struct {
	Offender  string
	Recipient string
	Denom     string
	Amount    string
}

type PenaltyAllocation struct {
	Offender string
	Route    PenaltyRoute
	Denom    string
	Amount   string
}

type FraudPenaltyPolicy struct {
	ReporterRewardCap       string
	CounterpartyRewardCap   string
	CounterpartyRewardBps   uint32
	BurnShareBps            uint32
	SecurityReserveShareBps uint32
	CommunityPoolShareBps   uint32
	SecurityReserveHook     bool
}

type PenaltySource string

const (
	PenaltySourceChannelBalance              PenaltySource = "CHANNEL_BALANCE"
	PenaltySourceParticipantBond             PenaltySource = "PARTICIPANT_BOND"
	PenaltySourceRoutingAdvertisementDeposit PenaltySource = "ROUTING_ADVERTISEMENT_DEPOSIT"
	PenaltySourceFraudProofDeposit           PenaltySource = "FRAUD_PROOF_DEPOSIT"
)

type PenaltyMatrixEntry struct {
	Class                    PaymentPenaltyClass
	ProofType                FraudProofType
	Source                   PenaltySource
	BasePenalty              string
	ReporterRewardCap        string
	CounterpartyCompensation string
	BurnShareBps             uint32
	SecurityReserveShareBps  uint32
	CommunityPoolShareBps    uint32
	InvalidProofVerifierCost string
	Bounded                  bool
}

type PenaltyRouteAccounting struct {
	Class            PaymentPenaltyClass
	Source           PenaltySource
	TotalPenalty     string
	ReporterReward   string
	CounterpartyComp string
	Allocations      []PenaltyAllocation
	Penalties        []Penalty
}

type InvalidFraudProofSubmissionPenalty struct {
	Submitter        string
	Denom            string
	DepositAmount    string
	VerificationCost string
	ForfeitedAmount  string
	RefundAmount     string
}

func (f FraudProof) Normalize() FraudProof {
	f.ProofID = normalizeHash(f.ProofID)
	f.SubmittedBy = strings.TrimSpace(f.SubmittedBy)
	f.OffendingSigner = strings.TrimSpace(f.OffendingSigner)
	f.PenaltyDenom = normalizeAssetDenom(f.PenaltyDenom)
	f.PenaltyAmount = strings.TrimSpace(f.PenaltyAmount)
	f.EvidenceHash = normalizeHash(f.EvidenceHash)
	f.VerificationFeePaid = strings.TrimSpace(f.VerificationFeePaid)
	if f.VerificationFeePaid == "" {
		f.VerificationFeePaid = "0"
	}
	f.StateA = f.StateA.Normalize()
	f.StateB = f.StateB.Normalize()
	f.AsyncProof = f.AsyncProof.Normalize()
	return f
}

func (f FraudProof) ChannelID() string {
	proof := f.Normalize()
	if proof.StateA.ChannelID != "" {
		return proof.StateA.ChannelID
	}
	return proof.StateB.ChannelID
}

func (f FraudProof) ValidateForChannel(channel ChannelRecord) error {
	proof := f.Normalize()
	if err := ValidateHash("payments fraud proof id", proof.ProofID); err != nil {
		return err
	}
	if !IsFraudProofType(proof.ProofType) {
		return fmt.Errorf("unknown payments fraud proof type %q", proof.ProofType)
	}
	if err := addressing.ValidateUserAddress("payments fraud submitter", proof.SubmittedBy); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments fraud offender", proof.OffendingSigner); err != nil {
		return err
	}
	if !containsString(channel.Participants, proof.SubmittedBy) || !containsString(channel.Participants, proof.OffendingSigner) {
		return errors.New("payments fraud parties must be channel participants")
	}
	if proof.PenaltyDenom != NativeDenom {
		return fmt.Errorf("payments fraud penalty denom must be %s", NativeDenom)
	}
	if err := validatePositiveInt("payments fraud penalty", proof.PenaltyAmount); err != nil {
		return err
	}
	if err := validateNonNegativeInt("payments fraud verification fee", proof.VerificationFeePaid); err != nil {
		return err
	}
	if err := ValidateHash("payments fraud evidence hash", proof.EvidenceHash); err != nil {
		return err
	}
	switch proof.ProofType {
	case FraudProofTypeDoubleSign:
		if err := proof.StateA.ValidateForChannel(channel, false); err != nil {
			return err
		}
		if err := proof.StateB.ValidateForChannel(channel, false); err != nil {
			return err
		}
		if proof.StateA.ChannelID != proof.StateB.ChannelID || proof.StateA.Epoch != proof.StateB.Epoch || proof.StateA.Nonce != proof.StateB.Nonce {
			return errors.New("payments double-sign proof states must share channel, epoch, and nonce")
		}
		if proof.StateA.StateHash == proof.StateB.StateHash {
			return errors.New("payments double-sign proof requires conflicting state hashes")
		}
		if !stateSignedBy(proof.StateA, proof.OffendingSigner) || !stateSignedBy(proof.StateB, proof.OffendingSigner) {
			return errors.New("payments double-sign proof requires offender signature on both states")
		}
	case FraudProofTypeStaleClose:
		if err := proof.StateA.ValidateForChannel(channel, false); err != nil {
			return err
		}
		if err := proof.StateB.ValidateForChannel(channel, false); err != nil {
			return err
		}
		if proof.StateB.Nonce <= proof.StateA.Nonce {
			return errors.New("payments stale-close proof requires newer state")
		}
	case FraudProofTypeInvalidClose:
		if err := proof.StateA.ValidateForChannel(channel, false); err != nil {
			return err
		}
		if proof.StateA.StateHash == channel.LatestState.StateHash {
			return errors.New("payments invalid-close proof requires non-latest close state")
		}
	case FraudProofTypeInvalidBalance:
		if err := validateSignedStateDomainForFraud(channel, proof.StateA); err != nil {
			return err
		}
		if err := validateCollateralConservation(proof.StateA, channel); err == nil {
			return errors.New("payments invalid-balance proof requires collateral conservation failure")
		}
	case FraudProofTypeInvalidCondition:
		if err := proof.StateA.ValidateForChannel(channel, false); err != nil {
			return err
		}
		if len(proof.StateA.Conditions) == 0 {
			return errors.New("payments invalid-condition proof requires conditions")
		}
	case FraudProofTypeReplayAttempt:
		if proof.StateA.ChainID != channel.ChainID || proof.StateA.ChannelID != channel.ChannelID {
			if err := validateSignedStateShapeForFraud(channel, proof.StateA); err != nil {
				return err
			}
			return nil
		}
		if err := proof.StateA.ValidateForChannel(channel, false); err != nil {
			return err
		}
		if proof.StateA.Nonce > channel.FinalizedNonce {
			return errors.New("payments replay proof requires finalized or older nonce")
		}
	case FraudProofTypeAsyncOverexposure:
		if err := validateAsyncOverexposureProof(channel, proof.AsyncProof); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown payments fraud proof type %q", proof.ProofType)
	}
	return nil
}

func (p Penalty) Normalize() Penalty {
	p.Offender = strings.TrimSpace(p.Offender)
	p.Recipient = strings.TrimSpace(p.Recipient)
	p.Denom = normalizeAssetDenom(p.Denom)
	p.Amount = strings.TrimSpace(p.Amount)
	return p
}

func (p Penalty) ValidateForChannel(channel ChannelRecord) error {
	p = p.Normalize()
	if err := addressing.ValidateUserAddress("payments penalty offender", p.Offender); err != nil {
		return err
	}
	if err := addressing.ValidateUserAddress("payments penalty recipient", p.Recipient); err != nil {
		return err
	}
	if !containsString(channel.Participants, p.Offender) || !containsString(channel.Participants, p.Recipient) {
		return errors.New("payments penalty parties must be channel participants")
	}
	if p.Offender == p.Recipient {
		return errors.New("payments penalty parties must differ")
	}
	if p.Denom != NativeDenom {
		return fmt.Errorf("payments penalty denom must be %s", NativeDenom)
	}
	return validatePositiveInt("payments penalty amount", p.Amount)
}

func (p PenaltyAllocation) Normalize() PenaltyAllocation {
	p.Offender = strings.TrimSpace(p.Offender)
	p.Denom = normalizeAssetDenom(p.Denom)
	p.Amount = strings.TrimSpace(p.Amount)
	return p
}

func (p PenaltyAllocation) ValidateForChannel(channel ChannelRecord) error {
	allocation := p.Normalize()
	if err := addressing.ValidateUserAddress("payments penalty allocation offender", allocation.Offender); err != nil {
		return err
	}
	if !containsString(channel.Participants, allocation.Offender) {
		return errors.New("payments penalty allocation offender must be channel participant")
	}
	if !IsPenaltyRoute(allocation.Route) || allocation.Route == PenaltyRouteReporter || allocation.Route == PenaltyRouteCounterparty {
		return errors.New("payments penalty allocation route must be burn, security reserve, or community pool")
	}
	if allocation.Denom != NativeDenom {
		return fmt.Errorf("payments penalty allocation denom must be %s", NativeDenom)
	}
	return validatePositiveInt("payments penalty allocation amount", allocation.Amount)
}

func (p FraudPenaltyPolicy) Normalize() FraudPenaltyPolicy {
	p.ReporterRewardCap = strings.TrimSpace(p.ReporterRewardCap)
	p.CounterpartyRewardCap = strings.TrimSpace(p.CounterpartyRewardCap)
	return p
}

func (p FraudPenaltyPolicy) Validate() error {
	p = p.Normalize()
	if p.ReporterRewardCap != "" {
		if err := validateNonNegativeInt("payments reporter reward cap", p.ReporterRewardCap); err != nil {
			return err
		}
	}
	if p.CounterpartyRewardCap != "" {
		if err := validateNonNegativeInt("payments counterparty reward cap", p.CounterpartyRewardCap); err != nil {
			return err
		}
	}
	if p.CounterpartyRewardBps > MaxPenaltyRouteBps {
		return errors.New("payments counterparty reward bps exceeds 10000")
	}
	total := p.BurnShareBps + p.SecurityReserveShareBps + p.CommunityPoolShareBps
	if total > MaxPenaltyRouteBps {
		return errors.New("payments penalty route shares exceed 10000 bps")
	}
	return nil
}

func (e PenaltyMatrixEntry) Normalize() PenaltyMatrixEntry {
	e.BasePenalty = strings.TrimSpace(e.BasePenalty)
	e.ReporterRewardCap = strings.TrimSpace(e.ReporterRewardCap)
	e.CounterpartyCompensation = strings.TrimSpace(e.CounterpartyCompensation)
	e.InvalidProofVerifierCost = strings.TrimSpace(e.InvalidProofVerifierCost)
	if e.BasePenalty == "" {
		e.BasePenalty = "0"
	}
	if e.ReporterRewardCap == "" {
		e.ReporterRewardCap = "0"
	}
	if e.CounterpartyCompensation == "" {
		e.CounterpartyCompensation = "0"
	}
	if e.InvalidProofVerifierCost == "" {
		e.InvalidProofVerifierCost = "0"
	}
	return e
}

func (e PenaltyMatrixEntry) Validate() error {
	entry := e.Normalize()
	if !IsPaymentPenaltyClass(entry.Class) {
		return fmt.Errorf("unknown payments penalty class %q", entry.Class)
	}
	if entry.ProofType != "" && !IsFraudProofType(entry.ProofType) {
		return fmt.Errorf("unknown payments penalty matrix proof type %q", entry.ProofType)
	}
	if !IsPenaltySource(entry.Source) {
		return fmt.Errorf("unknown payments penalty source %q", entry.Source)
	}
	for _, item := range []struct {
		name   string
		amount string
	}{
		{"payments penalty matrix base", entry.BasePenalty},
		{"payments penalty reporter cap", entry.ReporterRewardCap},
		{"payments penalty counterparty compensation", entry.CounterpartyCompensation},
		{"payments invalid proof verifier cost", entry.InvalidProofVerifierCost},
	} {
		if err := validateNonNegativeInt(item.name, item.amount); err != nil {
			return err
		}
	}
	if entry.BurnShareBps+entry.SecurityReserveShareBps+entry.CommunityPoolShareBps > MaxPenaltyRouteBps {
		return errors.New("payments penalty matrix route shares exceed 10000 bps")
	}
	return nil
}

func BuildFraudPenaltyRouting(channel ChannelRecord, proof FraudProof, policy FraudPenaltyPolicy) ([]Penalty, []PenaltyAllocation, error) {
	channel = channel.Normalize()
	proof = proof.Normalize()
	policy = policy.Normalize()
	if err := proof.ValidateForChannel(channel); err != nil {
		return nil, nil, err
	}
	if err := policy.Validate(); err != nil {
		return nil, nil, err
	}
	penaltyAmount, err := parsePositiveInt("payments fraud penalty", proof.PenaltyAmount)
	if err != nil {
		return nil, nil, err
	}
	reporterReward := penaltyAmount
	if policy.ReporterRewardCap != "" {
		capAmount, err := parseNonNegativeInt("payments reporter reward cap", policy.ReporterRewardCap)
		if err != nil {
			return nil, nil, err
		}
		if reporterReward.GT(capAmount) {
			reporterReward = capAmount
		}
	}
	penalties := []Penalty{}
	if reporterReward.IsPositive() {
		penalty := Penalty{Offender: proof.OffendingSigner, Recipient: proof.SubmittedBy, Denom: NativeDenom, Amount: reporterReward.String()}.Normalize()
		if err := penalty.ValidateForChannel(channel); err != nil {
			return nil, nil, err
		}
		penalties = append(penalties, penalty)
	}
	remaining := penaltyAmount.Sub(reporterReward)
	allocations, err := splitPenaltyRemainder(proof.OffendingSigner, remaining, policy)
	if err != nil {
		return nil, nil, err
	}
	for _, allocation := range allocations {
		if err := allocation.ValidateForChannel(channel); err != nil {
			return nil, nil, err
		}
	}
	return normalizePenalties(penalties), normalizePenaltyAllocations(allocations), nil
}

func DefaultPenaltyMatrix() []PenaltyMatrixEntry {
	return []PenaltyMatrixEntry{
		{Class: PenaltyClassInvalidClose, ProofType: FraudProofTypeInvalidClose, Source: PenaltySourceChannelBalance, BasePenalty: "10", ReporterRewardCap: "5", CounterpartyCompensation: "5", BurnShareBps: 2_500, SecurityReserveShareBps: 2_500, CommunityPoolShareBps: 5_000, Bounded: true},
		{Class: PenaltyClassInvalidClose, ProofType: FraudProofTypeInvalidBalance, Source: PenaltySourceChannelBalance, BasePenalty: "10", ReporterRewardCap: "5", CounterpartyCompensation: "5", BurnShareBps: 2_500, SecurityReserveShareBps: 2_500, CommunityPoolShareBps: 5_000, Bounded: true},
		{Class: PenaltyClassStaleClose, ProofType: FraudProofTypeStaleClose, Source: PenaltySourceChannelBalance, BasePenalty: "10", ReporterRewardCap: "5", CounterpartyCompensation: "5", BurnShareBps: 2_500, SecurityReserveShareBps: 2_500, CommunityPoolShareBps: 5_000, Bounded: true},
		{Class: PenaltyClassDoubleSign, ProofType: FraudProofTypeDoubleSign, Source: PenaltySourceParticipantBond, BasePenalty: "20", ReporterRewardCap: "8", CounterpartyCompensation: "6", BurnShareBps: 3_000, SecurityReserveShareBps: 4_000, CommunityPoolShareBps: 3_000, Bounded: true},
		{Class: PenaltyClassInvalidCondition, ProofType: FraudProofTypeInvalidCondition, Source: PenaltySourceChannelBalance, BasePenalty: "8", ReporterRewardCap: "4", CounterpartyCompensation: "4", BurnShareBps: 2_500, SecurityReserveShareBps: 2_500, CommunityPoolShareBps: 5_000, Bounded: true},
		{Class: PenaltyClassReplayAttempt, ProofType: FraudProofTypeReplayAttempt, Source: PenaltySourceChannelBalance, BasePenalty: "8", ReporterRewardCap: "4", CounterpartyCompensation: "4", BurnShareBps: 3_000, SecurityReserveShareBps: 3_000, CommunityPoolShareBps: 4_000, Bounded: true},
		{Class: PenaltyClassAsyncOverexposure, ProofType: FraudProofTypeAsyncOverexposure, Source: PenaltySourceParticipantBond, BasePenalty: "12", ReporterRewardCap: "5", CounterpartyCompensation: "5", BurnShareBps: 3_000, SecurityReserveShareBps: 3_000, CommunityPoolShareBps: 4_000, Bounded: true},
		{Class: PenaltyClassInvalidFraudProof, Source: PenaltySourceFraudProofDeposit, BasePenalty: "0", InvalidProofVerifierCost: "1", Bounded: true},
	}
}

func PenaltyClassForFraudProofType(proofType FraudProofType) (PaymentPenaltyClass, error) {
	switch proofType {
	case FraudProofTypeInvalidClose:
		return PenaltyClassInvalidClose, nil
	case FraudProofTypeStaleClose:
		return PenaltyClassStaleClose, nil
	case FraudProofTypeDoubleSign:
		return PenaltyClassDoubleSign, nil
	case FraudProofTypeInvalidCondition:
		return PenaltyClassInvalidCondition, nil
	case FraudProofTypeReplayAttempt:
		return PenaltyClassReplayAttempt, nil
	case FraudProofTypeAsyncOverexposure:
		return PenaltyClassAsyncOverexposure, nil
	case FraudProofTypeInvalidBalance:
		return PenaltyClassInvalidClose, nil
	default:
		return "", fmt.Errorf("unknown payments fraud proof type %q", proofType)
	}
}

func PenaltyMatrixEntryForProof(proofType FraudProofType, matrix []PenaltyMatrixEntry) (PenaltyMatrixEntry, error) {
	class, err := PenaltyClassForFraudProofType(proofType)
	if err != nil {
		return PenaltyMatrixEntry{}, err
	}
	for _, entry := range normalizePenaltyMatrix(matrix) {
		if entry.Class == class && (entry.ProofType == "" || entry.ProofType == proofType) {
			return entry, nil
		}
	}
	return PenaltyMatrixEntry{}, errors.New("payments penalty matrix entry not found")
}

func BuildPenaltyRouteAccounting(channel ChannelRecord, proof FraudProof, matrix []PenaltyMatrixEntry, policy FraudPenaltyPolicy) (PenaltyRouteAccounting, error) {
	channel = channel.Normalize()
	proof = proof.Normalize()
	if err := proof.ValidateForChannel(channel); err != nil {
		return PenaltyRouteAccounting{}, err
	}
	entry, err := PenaltyMatrixEntryForProof(proof.ProofType, matrix)
	if err != nil {
		return PenaltyRouteAccounting{}, err
	}
	if err := entry.Validate(); err != nil {
		return PenaltyRouteAccounting{}, err
	}
	policy = policy.Normalize()
	if policy.ReporterRewardCap == "" {
		policy.ReporterRewardCap = entry.ReporterRewardCap
	}
	if policy.CounterpartyRewardCap == "" {
		policy.CounterpartyRewardCap = entry.CounterpartyCompensation
	}
	if policy.BurnShareBps == 0 && policy.SecurityReserveShareBps == 0 && policy.CommunityPoolShareBps == 0 {
		policy.BurnShareBps = entry.BurnShareBps
		policy.SecurityReserveShareBps = entry.SecurityReserveShareBps
		policy.CommunityPoolShareBps = entry.CommunityPoolShareBps
	}
	if err := policy.Validate(); err != nil {
		return PenaltyRouteAccounting{}, err
	}
	penaltyAmount, err := parsePositiveInt("payments penalty accounting amount", proof.PenaltyAmount)
	if err != nil {
		return PenaltyRouteAccounting{}, err
	}
	if entry.BasePenalty != "" {
		basePenalty, err := parseNonNegativeInt("payments penalty matrix base", entry.BasePenalty)
		if err != nil {
			return PenaltyRouteAccounting{}, err
		}
		if penaltyAmount.LT(basePenalty) {
			return PenaltyRouteAccounting{}, errors.New("payments fraud penalty below matrix minimum")
		}
	}
	remaining := penaltyAmount
	penalties := []Penalty{}
	counterpartyComp := sdkmath.ZeroInt()
	if proof.OffendingSigner == channel.PendingClose.Submitter {
		counterparty := channelCounterparty(channel, proof.OffendingSigner)
		if counterparty != "" {
			counterpartyComp, err = cappedPenaltyPortion(remaining, policy.CounterpartyRewardCap, policy.CounterpartyRewardBps)
			if err != nil {
				return PenaltyRouteAccounting{}, err
			}
			if counterpartyComp.IsPositive() {
				penalty := Penalty{Offender: proof.OffendingSigner, Recipient: counterparty, Denom: NativeDenom, Amount: counterpartyComp.String()}.Normalize()
				if err := penalty.ValidateForChannel(channel); err != nil {
					return PenaltyRouteAccounting{}, err
				}
				penalties = append(penalties, penalty)
				remaining = remaining.Sub(counterpartyComp)
			}
		}
	}
	reporterReward := sdkmath.ZeroInt()
	if remaining.IsPositive() {
		reporterReward, err = cappedPenaltyPortion(remaining, policy.ReporterRewardCap, 0)
		if err != nil {
			return PenaltyRouteAccounting{}, err
		}
		if reporterReward.IsPositive() {
			penalty := Penalty{Offender: proof.OffendingSigner, Recipient: proof.SubmittedBy, Denom: NativeDenom, Amount: reporterReward.String()}.Normalize()
			if err := penalty.ValidateForChannel(channel); err != nil {
				return PenaltyRouteAccounting{}, err
			}
			penalties = append(penalties, penalty)
			remaining = remaining.Sub(reporterReward)
		}
	}
	allocations, err := splitPenaltyRemainder(proof.OffendingSigner, remaining, policy)
	if err != nil {
		return PenaltyRouteAccounting{}, err
	}
	return PenaltyRouteAccounting{
		Class:            entry.Class,
		Source:           entry.Source,
		TotalPenalty:     penaltyAmount.String(),
		ReporterReward:   reporterReward.String(),
		CounterpartyComp: counterpartyComp.String(),
		Allocations:      allocations,
		Penalties:        normalizePenalties(penalties),
	}, nil
}

func ComputeInvalidFraudProofSubmissionPenalty(submitter, depositAmount, verificationCost string) (InvalidFraudProofSubmissionPenalty, error) {
	submitter = strings.TrimSpace(submitter)
	if err := addressing.ValidateUserAddress("payments invalid fraud proof submitter", submitter); err != nil {
		return InvalidFraudProofSubmissionPenalty{}, err
	}
	deposit, err := parseNonNegativeInt("payments invalid fraud proof deposit", strings.TrimSpace(depositAmount))
	if err != nil {
		return InvalidFraudProofSubmissionPenalty{}, err
	}
	cost, err := parseNonNegativeInt("payments invalid fraud proof verification cost", strings.TrimSpace(verificationCost))
	if err != nil {
		return InvalidFraudProofSubmissionPenalty{}, err
	}
	forfeited := cost
	if forfeited.GT(deposit) {
		forfeited = deposit
	}
	return InvalidFraudProofSubmissionPenalty{
		Submitter:        submitter,
		Denom:            NativeDenom,
		DepositAmount:    deposit.String(),
		VerificationCost: cost.String(),
		ForfeitedAmount:  forfeited.String(),
		RefundAmount:     deposit.Sub(forfeited).String(),
	}, nil
}

func IsFraudProofType(value FraudProofType) bool {
	switch value {
	case FraudProofTypeDoubleSign,
		FraudProofTypeStaleClose,
		FraudProofTypeInvalidClose,
		FraudProofTypeInvalidBalance,
		FraudProofTypeInvalidCondition,
		FraudProofTypeReplayAttempt,
		FraudProofTypeAsyncOverexposure:
		return true
	default:
		return false
	}
}

func IsPenaltyRoute(value PenaltyRoute) bool {
	switch value {
	case PenaltyRouteReporter, PenaltyRouteCounterparty, PenaltyRouteBurn, PenaltyRouteSecurityReserve, PenaltyRouteCommunityPool:
		return true
	default:
		return false
	}
}

func IsPaymentPenaltyClass(value PaymentPenaltyClass) bool {
	switch value {
	case PenaltyClassInvalidClose,
		PenaltyClassStaleClose,
		PenaltyClassDoubleSign,
		PenaltyClassInvalidCondition,
		PenaltyClassReplayAttempt,
		PenaltyClassAsyncOverexposure,
		PenaltyClassInvalidFraudProof:
		return true
	default:
		return false
	}
}

func IsPenaltySource(value PenaltySource) bool {
	switch value {
	case PenaltySourceChannelBalance,
		PenaltySourceParticipantBond,
		PenaltySourceRoutingAdvertisementDeposit,
		PenaltySourceFraudProofDeposit:
		return true
	default:
		return false
	}
}

func validateSignedStateShapeForFraud(channel ChannelRecord, state ChannelState) error {
	channel = channel.Normalize()
	state = state.Normalize()
	if err := validateUnsignedStateShape(state); err != nil {
		return err
	}
	if state.StateHash == "" {
		return errors.New("payments fraud state hash is required")
	}
	if expected := ComputeStateHash(state); state.StateHash != expected {
		return errors.New("payments fraud state hash mismatch")
	}
	return validateSignatureQuorum(state.Signatures, channel.RequiredSigners, state)
}

func validateSignedStateDomainForFraud(channel ChannelRecord, state ChannelState) error {
	channel = channel.Normalize()
	state = state.Normalize()
	if err := validateSignedStateShapeForFraud(channel, state); err != nil {
		return err
	}
	if state.ChainID != channel.ChainID {
		return errors.New("payments fraud state chain id mismatch")
	}
	if state.ChannelID != channel.ChannelID {
		return errors.New("payments fraud state channel mismatch")
	}
	if state.ChannelType != channel.ChannelType {
		return errors.New("payments fraud state type mismatch")
	}
	if expected := ComputeParticipantSetHash(channel.Participants); state.ParticipantSetHash != expected {
		return errors.New("payments fraud state participant set hash mismatch")
	}
	if state.Denom != channel.Denom {
		return errors.New("payments fraud state denom mismatch")
	}
	if state.ChallengePeriod != channel.DisputePeriod {
		return errors.New("payments fraud state challenge period mismatch")
	}
	if expected := ComputeRequiredSignerBitmap(channel.Participants, channel.RequiredSigners); state.RequiredSignerBitmap != expected {
		return errors.New("payments fraud state required signer bitmap mismatch")
	}
	return validateStateParticipants(state, channel)
}

func sumPenaltyAllocations(allocations []PenaltyAllocation) (sdkmath.Int, error) {
	total := sdkmath.ZeroInt()
	for _, allocation := range normalizePenaltyAllocations(allocations) {
		amount, err := parsePositiveInt("payments penalty allocation amount", allocation.Amount)
		if err != nil {
			return sdkmath.Int{}, err
		}
		total = total.Add(amount)
	}
	return total, nil
}

func splitPenaltyRemainder(offender string, remaining sdkmath.Int, policy FraudPenaltyPolicy) ([]PenaltyAllocation, error) {
	if !remaining.IsPositive() {
		return nil, nil
	}
	shares := []struct {
		route PenaltyRoute
		bps   uint32
	}{
		{route: PenaltyRouteBurn, bps: policy.BurnShareBps},
		{route: PenaltyRouteSecurityReserve, bps: policy.SecurityReserveShareBps},
		{route: PenaltyRouteCommunityPool, bps: policy.CommunityPoolShareBps},
	}
	allocated := sdkmath.ZeroInt()
	amountByRoute := map[PenaltyRoute]sdkmath.Int{}
	for _, share := range shares {
		if share.bps == 0 {
			continue
		}
		amount := remaining.MulRaw(int64(share.bps)).QuoRaw(int64(MaxPenaltyRouteBps))
		if !amount.IsPositive() {
			continue
		}
		allocated = allocated.Add(amount)
		current, found := amountByRoute[share.route]
		if !found {
			current = sdkmath.ZeroInt()
		}
		amountByRoute[share.route] = current.Add(amount)
	}
	remainder := remaining.Sub(allocated)
	if remainder.IsPositive() {
		route := PenaltyRouteCommunityPool
		if policy.BurnShareBps == 0 && policy.SecurityReserveShareBps == 0 && policy.CommunityPoolShareBps == 0 {
			route = PenaltyRouteCommunityPool
		}
		current, found := amountByRoute[route]
		if !found {
			current = sdkmath.ZeroInt()
		}
		amountByRoute[route] = current.Add(remainder)
	}
	allocations := make([]PenaltyAllocation, 0, len(amountByRoute))
	for route, amount := range amountByRoute {
		if amount.IsPositive() {
			allocations = append(allocations, PenaltyAllocation{Offender: offender, Route: route, Denom: NativeDenom, Amount: amount.String()})
		}
	}
	return normalizePenaltyAllocations(allocations), nil
}

func normalizeFraudProofs(proofs []FraudProof) []FraudProof {
	out := make([]FraudProof, len(proofs))
	for i, proof := range proofs {
		out[i] = proof.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ProofID < out[j].ProofID
	})
	return out
}

func normalizePenalties(penalties []Penalty) []Penalty {
	out := make([]Penalty, len(penalties))
	for i, penalty := range penalties {
		out[i] = penalty.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Offender != out[j].Offender {
			return out[i].Offender < out[j].Offender
		}
		if out[i].Recipient != out[j].Recipient {
			return out[i].Recipient < out[j].Recipient
		}
		return out[i].Amount < out[j].Amount
	})
	return out
}

func normalizePenaltyAllocations(allocations []PenaltyAllocation) []PenaltyAllocation {
	out := make([]PenaltyAllocation, len(allocations))
	for i, allocation := range allocations {
		out[i] = allocation.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Offender != out[j].Offender {
			return out[i].Offender < out[j].Offender
		}
		if out[i].Route != out[j].Route {
			return out[i].Route < out[j].Route
		}
		return out[i].Amount < out[j].Amount
	})
	return out
}

func normalizePenaltyMatrix(matrix []PenaltyMatrixEntry) []PenaltyMatrixEntry {
	if len(matrix) == 0 {
		matrix = DefaultPenaltyMatrix()
	}
	out := make([]PenaltyMatrixEntry, len(matrix))
	for i, entry := range matrix {
		out[i] = entry.Normalize()
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := string(out[i].Class) + "/" + string(out[i].ProofType)
		right := string(out[j].Class) + "/" + string(out[j].ProofType)
		return left < right
	})
	return out
}

func cappedPenaltyPortion(available sdkmath.Int, capText string, bps uint32) (sdkmath.Int, error) {
	if !available.IsPositive() {
		return sdkmath.ZeroInt(), nil
	}
	portion := available
	if bps > 0 {
		if bps > MaxPenaltyRouteBps {
			return sdkmath.ZeroInt(), errors.New("payments penalty portion bps exceeds 10000")
		}
		portion = available.MulRaw(int64(bps)).QuoRaw(int64(MaxPenaltyRouteBps))
	}
	capText = strings.TrimSpace(capText)
	if capText != "" {
		capAmount, err := parseNonNegativeInt("payments penalty portion cap", capText)
		if err != nil {
			return sdkmath.ZeroInt(), err
		}
		if portion.GT(capAmount) {
			portion = capAmount
		}
	}
	if portion.GT(available) {
		portion = available
	}
	return portion, nil
}
