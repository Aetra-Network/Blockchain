package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/reporter/v1"
)

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

var _ v1.QueryServer = queryServer{}

func (q queryServer) Reporter(ctx context.Context, req *v1.QueryReporterRequest) (*v1.QueryReporterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	reporter, found := q.Keeper.Reporter(req.ReporterAddress)
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	return &v1.QueryReporterResponse{
		Reporter: reporterRecordNativeToProto(reporter),
	}, nil
}

func (q queryServer) Reporters(ctx context.Context, req *v1.QueryReportersRequest) (*v1.QueryReportersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	reporters := q.Keeper.Reporters()
	return &v1.QueryReportersResponse{
		Reporters: reporterRecordSliceNativeToProto(reporters),
	}, nil
}

func (q queryServer) ReporterReports(ctx context.Context, req *v1.QueryReporterReportsRequest) (*v1.QueryReporterReportsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	reports := q.Keeper.ReporterReports(req.ReporterAddress)
	return &v1.QueryReporterReportsResponse{
		Reports: reportRecordSliceNativeToProto(reports),
	}, nil
}

func (q queryServer) ReporterRewards(ctx context.Context, req *v1.QueryReporterRewardsRequest) (*v1.QueryReporterRewardsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	rewards := q.Keeper.ReporterRewards(req.ReporterAddress)
	return &v1.QueryReporterRewardsResponse{
		Rewards: reporterRewardSliceNativeToProto(rewards),
	}, nil
}