package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	addressing "github.com/sovereign-l1/l1/app/addressing"
	v1 "github.com/sovereign-l1/l1/api/l1/evidence/v1"
	"github.com/sovereign-l1/l1/x/evidence/types"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

func NewMsgServerImpl(k *Keeper) v1.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) SubmitEvidence(ctx context.Context, msg *v1.MsgSubmitEvidence) (*v1.MsgSubmitEvidenceResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgSubmitEvidence{
		Authority:        msg.Authority,
		EvidenceID:       msg.EvidenceId,
		EvidenceType:     msg.EvidenceType,
		AccusedValidator: msg.AccusedValidator,
		Reporter:         msg.Reporter,
		ProofPayloadHash: msg.ProofPayloadHash,
		PayloadSizeBytes: msg.PayloadSizeBytes,
		RequiresReview:   msg.RequiresReview,
		SlashFractionBps: msg.SlashFractionBps,
		RewardNaet:       msg.RewardNaet,
		Height:           msg.Height,
	}
	record, err := m.Keeper.SubmitEvidence(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeSubmitEvidence,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyEvidenceID, record.EvidenceID),
		sdk.NewAttribute(v1.AttributeKeyAccusedValidator, record.AccusedValidator),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgSubmitEvidenceResponse{
		Evidence: evidenceRecordNativeToProto(record),
	}, nil
}

func (m msgServer) VoteEvidence(ctx context.Context, msg *v1.MsgVoteEvidence) (*v1.MsgVoteEvidenceResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgVoteEvidence{
		Authority:       msg.Authority,
		EvidenceID:     msg.EvidenceId,
		Voter:          msg.Voter,
		Accept:         msg.Accept,
		VotingPowerBps: msg.VotingPowerBps,
		Height:         msg.Height,
	}
	record, err := m.Keeper.VoteEvidence(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeVoteEvidence,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyEvidenceID, record.EvidenceID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgVoteEvidenceResponse{
		Evidence: evidenceRecordNativeToProto(record),
	}, nil
}

func (m msgServer) FinalizeEvidence(ctx context.Context, msg *v1.MsgFinalizeEvidence) (*v1.MsgFinalizeEvidenceResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgFinalizeEvidence{
		Authority:   msg.Authority,
		EvidenceID: msg.EvidenceId,
		Height:     msg.Height,
	}
	record, err := m.Keeper.FinalizeEvidence(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeFinalizeEvidence,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyEvidenceID, record.EvidenceID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgFinalizeEvidenceResponse{
		Evidence: evidenceRecordNativeToProto(record),
	}, nil
}

func (m msgServer) CancelExpiredEvidence(ctx context.Context, msg *v1.MsgCancelExpiredEvidence) (*v1.MsgCancelExpiredEvidenceResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgCancelExpiredEvidence{
		Authority:   msg.Authority,
		EvidenceID: msg.EvidenceId,
		Height:     msg.Height,
	}
	record, err := m.Keeper.CancelExpiredEvidence(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeCancelExpiredEvidence,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyEvidenceID, record.EvidenceID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgCancelExpiredEvidenceResponse{
		Evidence: evidenceRecordNativeToProto(record),
	}, nil
}

func (m msgServer) requireAuthority(authority string) error {
	if err := addressing.ValidateAuthorityAddress("authority", authority); err != nil {
		return v1.ErrUnauthorized.Wrap(err.Error())
	}
	if authority != m.Keeper.genesis.Params.Authority {
		return v1.ErrUnauthorized.Wrap("invalid authority")
	}
	return nil
}

func evidenceRecordNativeToProto(n types.EvidenceRecord) v1.EvidenceRecord {
	return v1.EvidenceRecord{
		EvidenceId:       n.EvidenceID,
		Status:           n.Status,
		EvidenceType:     n.EvidenceType,
		AccusedValidator: n.AccusedValidator,
		Reporter:         n.Reporter,
		ProofPayloadHash: n.ProofPayloadHash,
		PayloadSizeBytes: n.PayloadSizeBytes,
		Votes:            evidenceVoteSliceNativeToProto(n.Votes),
		SlashDecision:   slashDecisionNativeToProto(n.SlashDecision),
		RewardDecision:   rewardDecisionNativeToProto(n.RewardDecision),
		SubmittedHeight:  n.SubmittedHeight,
		UpdatedHeight:    n.UpdatedHeight,
		ExpirationHeight: n.ExpirationHeight,
		FinalizedHeight:  n.FinalizedHeight,
		RequiresReview:   n.RequiresReview,
		RejectionReason:  n.RejectionReason,
	}
}

func evidenceVoteNativeToProto(n types.EvidenceVote) v1.EvidenceVote {
	return v1.EvidenceVote{
		Voter:          n.Voter,
		Support:        n.Support,
		VotingPowerBps: n.VotingPowerBps,
		Height:         n.Height,
	}
}

func evidenceVoteSliceNativeToProto(ns []types.EvidenceVote) []v1.EvidenceVote {
	out := make([]v1.EvidenceVote, len(ns))
	for i, n := range ns {
		out[i] = evidenceVoteNativeToProto(n)
	}
	return out
}

func slashDecisionNativeToProto(n types.SlashDecision) v1.SlashDecision {
	return v1.SlashDecision{
		FractionBps: n.FractionBps,
		Tombstone:    n.Tombstone,
		Applied:      n.Applied,
	}
}

func rewardDecisionNativeToProto(n types.RewardDecision) v1.RewardDecision {
	return v1.RewardDecision{
		Reporter:   n.Reporter,
		AmountNaet: n.AmountNaet,
		Paid:       n.Paid,
	}
}

func evidenceRecordSliceNativeToProto(ns []types.EvidenceRecord) []v1.EvidenceRecord {
	out := make([]v1.EvidenceRecord, len(ns))
	for i, n := range ns {
		out[i] = evidenceRecordNativeToProto(n)
	}
	return out
}