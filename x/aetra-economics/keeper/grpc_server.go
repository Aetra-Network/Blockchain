package keeper

import (
	"context"
	"errors"

	"github.com/sovereign-l1/l1/x/aetra-economics/types"
)

var _ types.MsgServer = grpcMsgServer{}
var _ types.QueryServer = grpcQueryServer{}

type grpcMsgServer struct{ keeper *Keeper }
type grpcQueryServer struct{ keeper *Keeper }

func NewGRPCMsgServer(k *Keeper) types.MsgServer	{ return grpcMsgServer{keeper: k} }
func NewGRPCQueryServer(k *Keeper) types.QueryServer	{ return grpcQueryServer{keeper: k} }

func (s grpcMsgServer) UpdateEconomicsParams(ctx context.Context, msg *types.MsgUpdateEconomicsParams) (*types.MsgUpdateEconomicsParamsResponse, error) {
	if msg == nil {
		return nil, errors.New("empty economics params update request")
	}
	if err := NewMsgServerImpl(s.keeper).UpdateEconomicsParams(ctx, *msg); err != nil {
		return nil, err
	}
	return &types.MsgUpdateEconomicsParamsResponse{}, nil
}

func (s grpcMsgServer) ApplyEpochEconomics(ctx context.Context, msg *types.MsgApplyEpochEconomics) (*types.MsgApplyEpochEconomicsResponse, error) {
	if msg == nil {
		return nil, errors.New("empty economics epoch request")
	}
	if err := NewMsgServerImpl(s.keeper).ApplyEpochEconomics(ctx, *msg); err != nil {
		return nil, err
	}
	return &types.MsgApplyEpochEconomicsResponse{}, nil
}

func (s grpcQueryServer) CurrentInflation(ctx context.Context, req *types.QueryCurrentInflationRequest) (*types.QueryCurrentInflationResponse, error) {
	res, err := s.keeper.QueryCurrentInflation(ctx, *req)
	return &res, err
}
func (s grpcQueryServer) CurrentBondedRatio(ctx context.Context, req *types.QueryCurrentBondedRatioRequest) (*types.QueryCurrentBondedRatioResponse, error) {
	res, err := s.keeper.QueryCurrentBondedRatio(ctx, *req)
	return &res, err
}
func (s grpcQueryServer) EstimatedAPR(ctx context.Context, req *types.QueryEstimatedAPRRequest) (*types.QueryEstimatedAPRResponse, error) {
	res, err := s.keeper.QueryEstimatedAPR(ctx, *req)
	return &res, err
}
func (s grpcQueryServer) FeeSplitParams(ctx context.Context, req *types.QueryFeeSplitParamsRequest) (*types.QueryFeeSplitParamsResponse, error) {
	res, err := s.keeper.QueryFeeSplitParams(ctx, *req)
	return &res, err
}
func (s grpcQueryServer) BurnedSupply(ctx context.Context, req *types.QueryBurnedSupplyRequest) (*types.QueryBurnedSupplyResponse, error) {
	res, err := s.keeper.QueryBurnedSupply(ctx, *req)
	return &res, err
}
func (s grpcQueryServer) TreasuryBalance(ctx context.Context, req *types.QueryTreasuryBalanceRequest) (*types.QueryTreasuryBalanceResponse, error) {
	res, err := s.keeper.QueryTreasuryBalance(ctx, *req)
	return &res, err
}
func (s grpcQueryServer) EpochRewardSummary(ctx context.Context, req *types.QueryEpochRewardSummaryRequest) (*types.QueryEpochRewardSummaryResponse, error) {
	res, err := s.keeper.QueryEpochRewardSummary(ctx, *req)
	return &res, err
}
