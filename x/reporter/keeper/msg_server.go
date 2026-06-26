package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	addressing "github.com/sovereign-l1/l1/app/addressing"
	v1 "github.com/sovereign-l1/l1/api/l1/reporter/v1"
	"github.com/sovereign-l1/l1/x/reporter/types"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

func NewMsgServerImpl(k *Keeper) v1.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) RegisterReporter(ctx context.Context, msg *v1.MsgRegisterReporter) (*v1.MsgRegisterReporterResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgRegisterReporter{
		Authority:       msg.Authority,
		ReporterAddress: msg.ReporterAddress,
		Height:          msg.Height,
	}
	reporter, err := m.Keeper.RegisterReporter(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeRegisterReporter,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyReporterAddress, reporter.ReporterAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgRegisterReporterResponse{
		Reporter: reporterRecordNativeToProto(reporter),
	}, nil
}

func (m msgServer) BondReporter(ctx context.Context, msg *v1.MsgBondReporter) (*v1.MsgBondReporterResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgBondReporter{
		Authority:       msg.Authority,
		ReporterAddress: msg.ReporterAddress,
		Amount:          msg.Amount,
		Height:          msg.Height,
	}
	reporter, err := m.Keeper.BondReporter(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeBondReporter,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyReporterAddress, reporter.ReporterAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgBondReporterResponse{
		Reporter: reporterRecordNativeToProto(reporter),
	}, nil
}

func (m msgServer) UnbondReporter(ctx context.Context, msg *v1.MsgUnbondReporter) (*v1.MsgUnbondReporterResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgUnbondReporter{
		Authority:       msg.Authority,
		ReporterAddress: msg.ReporterAddress,
		Height:          msg.Height,
	}
	reporter, err := m.Keeper.UnbondReporter(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeUnbondReporter,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyReporterAddress, reporter.ReporterAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgUnbondReporterResponse{
		Reporter: reporterRecordNativeToProto(reporter),
	}, nil
}

func (m msgServer) SubmitReport(ctx context.Context, msg *v1.MsgSubmitReport) (*v1.MsgSubmitReportResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgSubmitReport{
		Authority:       msg.Authority,
		ReporterAddress: msg.ReporterAddress,
		ReportID:        msg.ReportId,
		ReportType:      msg.ReportType,
		Subject:         msg.Subject,
		PayloadHash:     msg.PayloadHash,
		PayloadSizeBytes: msg.PayloadSizeBytes,
		Accepted:        msg.Accepted,
		Malicious:      msg.Malicious,
		RewardAmount:   msg.RewardAmount,
		Height:          msg.Height,
	}
	report, err := m.Keeper.SubmitReport(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeSubmitReport,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyReporterAddress, report.ReporterAddress),
		sdk.NewAttribute(v1.AttributeKeyReportID, report.ReportID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgSubmitReportResponse{
		Report: reportRecordNativeToProto(report),
	}, nil
}

func (m msgServer) ClaimReporterReward(ctx context.Context, msg *v1.MsgClaimReporterReward) (*v1.MsgClaimReporterRewardResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgClaimReporterReward{
		Authority:       msg.Authority,
		ReporterAddress: msg.ReporterAddress,
		ReportID:        msg.ReportId,
		Height:          msg.Height,
	}
	reward, err := m.Keeper.ClaimReporterReward(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeClaimReporterReward,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyReporterAddress, msg.ReporterAddress),
		sdk.NewAttribute(v1.AttributeKeyReportID, msg.ReportId),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgClaimReporterRewardResponse{
		Reward: reporterRewardNativeToProto(reward),
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

func reporterRecordNativeToProto(n types.ReporterRecord) v1.ReporterRecord {
	return v1.ReporterRecord{
		ReporterAddress:          n.ReporterAddress,
		BondedAmount:             n.BondedAmount,
		ReporterScore:            n.ReporterScore,
		AcceptedReports:          n.AcceptedReports,
		RejectedReports:          n.RejectedReports,
		SlashedReporterBond:      n.SlashedReporterBond,
		Status:                   n.Status,
		UnbondingStartHeight:     n.UnbondingStartHeight,
		UnbondingCompleteHeight:  n.UnbondingCompleteHeight,
		RewardHistory:            reporterRewardSliceNativeToProto(n.RewardHistory),
	}
}

func reporterRewardNativeToProto(n types.ReporterReward) v1.ReporterReward {
	return v1.ReporterReward{
		ReportId:  n.ReportID,
		Amount:    n.Amount,
		Claimed:   n.Claimed,
		CreatedAt: n.CreatedAt,
		ClaimedAt: n.ClaimedAt,
	}
}

func reporterRewardSliceNativeToProto(ns []types.ReporterReward) []v1.ReporterReward {
	out := make([]v1.ReporterReward, len(ns))
	for i, n := range ns {
		out[i] = reporterRewardNativeToProto(n)
	}
	return out
}

func reportRecordNativeToProto(n types.ReportRecord) v1.ReportRecord {
	return v1.ReportRecord{
		ReportId:        n.ReportID,
		ReporterAddress: n.ReporterAddress,
		ReportType:      n.ReportType,
		Subject:         n.Subject,
		PayloadHash:     n.PayloadHash,
		PayloadSizeBytes: n.PayloadSizeBytes,
		Status:          n.Status,
		SubmittedHeight: n.SubmittedHeight,
		FinalizedHeight: n.FinalizedHeight,
		RewardAmount:    n.RewardAmount,
		RewardClaimed:   n.RewardClaimed,
		SlashAmount:     n.SlashAmount,
	}
}

func reporterRecordSliceNativeToProto(ns []types.ReporterRecord) []v1.ReporterRecord {
	out := make([]v1.ReporterRecord, len(ns))
	for i, n := range ns {
		out[i] = reporterRecordNativeToProto(n)
	}
	return out
}

func reportRecordSliceNativeToProto(ns []types.ReportRecord) []v1.ReportRecord {
	out := make([]v1.ReportRecord, len(ns))
	for i, n := range ns {
		out[i] = reportRecordNativeToProto(n)
	}
	return out
}