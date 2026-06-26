package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/singlenominatorpool/v1"
)

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

var _ v1.QueryServer = queryServer{}

func (q queryServer) SingleNominatorPool(ctx context.Context, req *v1.QuerySingleNominatorPoolRequest) (*v1.QuerySingleNominatorPoolResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	pool, found := q.Keeper.SingleNominatorPool(req.PoolAddress)
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	protoPool := singleNominatorPoolNativeToProto(pool)
	return &v1.QuerySingleNominatorPoolResponse{
		Pool: &protoPool,
	}, nil
}

func (q queryServer) SingleNominatorPools(ctx context.Context, req *v1.QuerySingleNominatorPoolsRequest) (*v1.QuerySingleNominatorPoolsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	pools := q.Keeper.SingleNominatorPools()
	return &v1.QuerySingleNominatorPoolsResponse{
		Pools: singleNominatorPoolSliceNativeToProto(pools),
	}, nil
}

func (q queryServer) SingleNominatorRewards(ctx context.Context, req *v1.QuerySingleNominatorRewardsRequest) (*v1.QuerySingleNominatorRewardsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	reward, found := q.Keeper.SingleNominatorRewards(req.PoolAddress)
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	return &v1.QuerySingleNominatorRewardsResponse{
		RewardBalance: reward,
	}, nil
}