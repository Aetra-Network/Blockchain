package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/scheduler/v1"
	"github.com/sovereign-l1/l1/x/internal/prototype"
	schedulertypes "github.com/sovereign-l1/l1/x/scheduler/types"
)

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

var _ v1.QueryServer = queryServer{}

func (q queryServer) Params(ctx context.Context, req *v1.QueryParamsRequest) (*v1.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	params := q.Keeper.genesis.Params
	schedulerParams := q.Keeper.SchedulerParams()
	return &v1.QueryParamsResponse{
		Params:          paramsNativeToProto(params),
		SchedulerParams: schedulerParamsNativeToProto(schedulerParams),
	}, nil
}

func (q queryServer) ScheduledJob(ctx context.Context, req *v1.QueryScheduledJobRequest) (*v1.QueryScheduledJobResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	job, found, err := q.Keeper.ScheduledJob(req.OwnerModule, req.JobId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	return &v1.QueryScheduledJobResponse{
		Job: scheduledJobNativeToProto(job),
	}, nil
}

func (q queryServer) ScheduledJobs(ctx context.Context, req *v1.QueryScheduledJobsRequest) (*v1.QueryScheduledJobsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	pageReq := &prototype.PageRequest{
		Offset: req.Offset,
		Limit:  req.Limit,
	}
	jobs, pageRes, err := q.Keeper.ScheduledJobs(pageReq)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryScheduledJobsResponse{
		Jobs:        scheduledJobSliceNativeToProto(jobs),
		NextOffset: pageRes.NextOffset,
	}, nil
}

func (q queryServer) DueJobs(ctx context.Context, req *v1.QueryDueJobsRequest) (*v1.QueryDueJobsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	pageReq := &prototype.PageRequest{
		Offset: req.Offset,
		Limit:  req.Limit,
	}
	jobs, pageRes, err := q.Keeper.DueJobs(req.DueHeight, pageReq)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryDueJobsResponse{
		Jobs:        scheduledJobSliceNativeToProto(jobs),
		NextOffset: pageRes.NextOffset,
	}, nil
}

func (q queryServer) JobHistory(ctx context.Context, req *v1.QueryJobHistoryRequest) (*v1.QueryJobHistoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	pageReq := &prototype.PageRequest{
		Offset: req.Offset,
		Limit:  req.Limit,
	}
	history, pageRes, err := q.Keeper.JobHistory(req.JobId, pageReq)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryJobHistoryResponse{
		History:     jobHistoryRecordSliceNativeToProto(history),
		NextOffset: pageRes.NextOffset,
	}, nil
}

func paramsNativeToProto(n prototype.Params) v1.Params {
	return v1.Params{
		Enabled:               n.Enabled,
		TestnetProfile:        n.TestnetProfile,
		ProductionVersionGate: n.ProductionVersionGate,
		Authority:            n.Authority,
		DefaultQueryLimit:    n.DefaultQueryLimit,
		MaxQueryLimit:        n.MaxQueryLimit,
	}
}

func schedulerParamsNativeToProto(n schedulertypes.SchedulerParams) v1.SchedulerParams {
	return v1.SchedulerParams{
		MaxJobsPerBlock:   n.MaxJobsPerBlock,
		MaxSchedulerGas:   n.MaxSchedulerGas,
		MaxGasPerJob:       n.MaxGasPerJob,
		AuthorizedModules:  n.AuthorizedModules,
		HistoryRetention:   n.HistoryRetention,
	}
}