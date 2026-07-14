package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	addressing "github.com/sovereign-l1/l1/app/addressing"
	v1 "github.com/sovereign-l1/l1/api/l1/scheduler/v1"
	"github.com/sovereign-l1/l1/x/internal/prototype"
	schedulertypes "github.com/sovereign-l1/l1/x/scheduler/types"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

func NewMsgServerImpl(k *Keeper) v1.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) UpdateParams(ctx context.Context, msg *v1.MsgUpdateParams) (*v1.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	params := paramsProtoToNative(msg.Params)
	schedulerParams := schedulerParamsProtoToNative(msg.SchedulerParams)
	if err := m.Keeper.UpdateParams(msg.Authority, params, schedulerParams); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeUpdateParams,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
	))
	return &v1.MsgUpdateParamsResponse{}, nil
}

func (m msgServer) RegisterScheduledJob(ctx context.Context, msg *v1.MsgRegisterScheduledJob) (*v1.MsgRegisterScheduledJobResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := schedulertypes.MsgRegisterScheduledJob{
		Authority: msg.Authority,
		Job:       scheduledJobProtoToNative(msg.Job),
	}
	if err := m.Keeper.RegisterScheduledJob(nativeMsg); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeRegisterScheduledJob,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOwnerModule, msg.Job.OwnerModule),
		sdk.NewAttribute(v1.AttributeKeyJobID, msg.Job.Id),
	))
	return &v1.MsgRegisterScheduledJobResponse{}, nil
}

func (m msgServer) PauseScheduledJob(ctx context.Context, msg *v1.MsgPauseScheduledJob) (*v1.MsgPauseScheduledJobResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := schedulertypes.MsgPauseScheduledJob{
		Authority:   msg.Authority,
		OwnerModule: msg.OwnerModule,
		JobID:      msg.JobId,
	}
	if err := m.Keeper.PauseScheduledJob(nativeMsg); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypePauseScheduledJob,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOwnerModule, msg.OwnerModule),
		sdk.NewAttribute(v1.AttributeKeyJobID, msg.JobId),
	))
	return &v1.MsgPauseScheduledJobResponse{}, nil
}

func (m msgServer) ResumeScheduledJob(ctx context.Context, msg *v1.MsgResumeScheduledJob) (*v1.MsgResumeScheduledJobResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := schedulertypes.MsgResumeScheduledJob{
		Authority:   msg.Authority,
		OwnerModule: msg.OwnerModule,
		JobID:      msg.JobId,
	}
	if err := m.Keeper.ResumeScheduledJob(nativeMsg); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeResumeScheduledJob,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOwnerModule, msg.OwnerModule),
		sdk.NewAttribute(v1.AttributeKeyJobID, msg.JobId),
	))
	return &v1.MsgResumeScheduledJobResponse{}, nil
}

func (m msgServer) CancelScheduledJob(ctx context.Context, msg *v1.MsgCancelScheduledJob) (*v1.MsgCancelScheduledJobResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := schedulertypes.MsgCancelScheduledJob{
		Authority:   msg.Authority,
		OwnerModule: msg.OwnerModule,
		JobID:      msg.JobId,
	}
	if err := m.Keeper.CancelScheduledJob(nativeMsg); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeCancelScheduledJob,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOwnerModule, msg.OwnerModule),
		sdk.NewAttribute(v1.AttributeKeyJobID, msg.JobId),
	))
	return &v1.MsgCancelScheduledJobResponse{}, nil
}

func (m msgServer) ExecuteDueJobs(ctx context.Context, msg *v1.MsgExecuteDueJobs) (*v1.MsgExecuteDueJobsResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := schedulertypes.MsgExecuteDueJobs{
		Authority:     msg.Authority,
		CurrentHeight: msg.CurrentHeight,
	}
	result, err := m.Keeper.ExecuteDueJobs(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeExecuteDueJobs,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.CurrentHeight)),
	))
	return &v1.MsgExecuteDueJobsResponse{
		Result: executionBatchResultNativeToProto(result),
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

func paramsProtoToNative(p v1.Params) prototype.Params {
	return prototype.Params{
		Enabled:               p.Enabled,
		TestnetProfile:        p.TestnetProfile,
		ProductionVersionGate: p.ProductionVersionGate,
		Authority:            p.Authority,
		DefaultQueryLimit:    p.DefaultQueryLimit,
		MaxQueryLimit:        p.MaxQueryLimit,
	}
}

func schedulerParamsProtoToNative(p v1.SchedulerParams) schedulertypes.SchedulerParams {
	return schedulertypes.SchedulerParams{
		MaxJobsPerBlock:  p.MaxJobsPerBlock,
		MaxSchedulerGas:  p.MaxSchedulerGas,
		MaxGasPerJob:     p.MaxGasPerJob,
		AuthorizedModules: p.AuthorizedModules,
		HistoryRetention: p.HistoryRetention,
	}
}

func scheduledJobProtoToNative(p v1.ScheduledJob) schedulertypes.ScheduledJob {
	return schedulertypes.ScheduledJob{
		ID:                   p.Id,
		OwnerModule:          p.OwnerModule,
		Type:                 p.Type,
		NextExecutionHeight:  p.NextExecutionHeight,
		Interval:            p.Interval,
		MaxGas:               p.MaxGas,
		RetryPolicy:          retryPolicyProtoToNative(p.RetryPolicy),
		FailureCount:         p.FailureCount,
		Paused:              p.Paused,
		Cancelled:           p.Cancelled,
		Payload:             p.Payload,
		ExecutionCount:       p.ExecutionCount,
	}
}

func scheduledJobNativeToProto(n schedulertypes.ScheduledJob) v1.ScheduledJob {
	return v1.ScheduledJob{
		Id:                  n.ID,
		OwnerModule:         n.OwnerModule,
		Type:                n.Type,
		NextExecutionHeight: n.NextExecutionHeight,
		Interval:            n.Interval,
		MaxGas:              n.MaxGas,
		RetryPolicy:         retryPolicyNativeToProto(n.RetryPolicy),
		FailureCount:        n.FailureCount,
		Paused:             n.Paused,
		Cancelled:          n.Cancelled,
		Payload:            n.Payload,
		ExecutionCount:      n.ExecutionCount,
	}
}

func retryPolicyProtoToNative(p v1.RetryPolicy) schedulertypes.RetryPolicy {
	return schedulertypes.RetryPolicy{
		MaxRetries:      p.MaxRetries,
		BackoffInterval: p.BackoffInterval,
	}
}

func retryPolicyNativeToProto(n schedulertypes.RetryPolicy) v1.RetryPolicy {
	return v1.RetryPolicy{
		MaxRetries:      n.MaxRetries,
		BackoffInterval: n.BackoffInterval,
	}
}

func executionBatchResultNativeToProto(n schedulertypes.ExecutionBatchResult) v1.ExecutionBatchResult {
	return v1.ExecutionBatchResult{
		Height:        n.Height,
		ExecutedJobs:  n.ExecutedJobs,
		SkippedJobs:   n.SkippedJobs,
		GasReserved:   n.GasReserved,
		GasUsed:       n.GasUsed,
		History:       jobHistoryRecordSliceNativeToProto(n.History),
		RemainingDue:  scheduledJobSliceNativeToProto(n.RemainingDue),
	}
}

func jobHistoryRecordNativeToProto(n schedulertypes.JobHistoryRecord) v1.JobHistoryRecord {
	return v1.JobHistoryRecord{
		JobId:       n.JobID,
		OwnerModule: n.OwnerModule,
		Height:      n.Height,
		Status:      n.Status,
		GasUsed:     n.GasUsed,
		Error:       n.Error,
		Attempt:     n.Attempt,
	}
}

func jobHistoryRecordSliceNativeToProto(ns []schedulertypes.JobHistoryRecord) []v1.JobHistoryRecord {
	out := make([]v1.JobHistoryRecord, len(ns))
	for i, n := range ns {
		out[i] = jobHistoryRecordNativeToProto(n)
	}
	return out
}

func scheduledJobSliceNativeToProto(ns []schedulertypes.ScheduledJob) []v1.ScheduledJob {
	out := make([]v1.ScheduledJob, len(ns))
	for i, n := range ns {
		out[i] = scheduledJobNativeToProto(n)
	}
	return out
}