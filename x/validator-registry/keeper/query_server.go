package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/validatorregistry/v1"
)

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

var _ v1.QueryServer = queryServer{}

func (q queryServer) Validator(ctx context.Context, req *v1.QueryValidatorRequest) (*v1.QueryValidatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	validator, found, err := q.Keeper.Validator(req.OperatorAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrValidatorNotFound.Error())
	}
	return &v1.QueryValidatorResponse{
		Validator: validatorRecordNativeToProto(validator),
	}, nil
}

func (q queryServer) Validators(ctx context.Context, req *v1.QueryValidatorsRequest) (*v1.QueryValidatorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	validators, err := q.Keeper.Validators()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryValidatorsResponse{
		Validators: validatorRecordSliceNativeToProto(validators),
	}, nil
}

func (q queryServer) ValidatorKeys(ctx context.Context, req *v1.QueryValidatorKeysRequest) (*v1.QueryValidatorKeysResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	keys, found, err := q.Keeper.ValidatorKeys(req.OperatorAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrValidatorNotFound.Error())
	}
	return &v1.QueryValidatorKeysResponse{
		OperatorAddress:              keys.OperatorAddress,
		ConsensusPublicKey:           keys.ConsensusPublicKey,
		PendingConsensusPublicKey:    keys.PendingConsensusPublicKey,
		ConsensusKeyActivationHeight: keys.ConsensusKeyActivationHeight,
		TreasuryAddress:              keys.TreasuryAddress,
		WithdrawalAddress:            keys.WithdrawalAddress,
		EmergencyAddress:             keys.EmergencyAddress,
	}, nil
}

func (q queryServer) ValidatorPerformance(ctx context.Context, req *v1.QueryValidatorPerformanceRequest) (*v1.QueryValidatorPerformanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	perf, found, err := q.Keeper.ValidatorPerformance(req.OperatorAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrValidatorNotFound.Error())
	}
	return &v1.QueryValidatorPerformanceResponse{
		OperatorAddress:    perf.OperatorAddress,
		UptimeHistory:      uptimeSampleSliceNativeToProto(perf.UptimeHistory),
		LatencyHistory:     latencySampleSliceNativeToProto(perf.LatencyHistory),
		MissedBlockCounter: perf.MissedBlockCounter,
		ReputationScore:    perf.ReputationScore,
		PerformanceScore:   perf.PerformanceScore,
	}, nil
}

func (q queryServer) ValidatorSecurityStatus(ctx context.Context, req *v1.QueryValidatorSecurityStatusRequest) (*v1.QueryValidatorSecurityStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	sec, found, err := q.Keeper.ValidatorSecurityStatus(req.OperatorAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrValidatorNotFound.Error())
	}
	return &v1.QueryValidatorSecurityStatusResponse{
		OperatorAddress:    sec.OperatorAddress,
		Status:             sec.Status,
		SlashingHistory:    slashingEventSliceNativeToProto(sec.SlashingHistory),
		ExternalAuditFlags: sec.ExternalAuditFlags,
	}, nil
}

func (q queryServer) ValidatorHistory(ctx context.Context, req *v1.QueryValidatorHistoryRequest) (*v1.QueryValidatorHistoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	history, found, err := q.Keeper.ValidatorHistory(req.OperatorAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrValidatorNotFound.Error())
	}
	return &v1.QueryValidatorHistoryResponse{
		History: validatorHistoryEventSliceNativeToProto(history),
	}, nil
}