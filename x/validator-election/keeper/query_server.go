package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/validatorelection/v1"
)

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

var _ v1.QueryServer = queryServer{}

func (q queryServer) PreviousValidatorSet(ctx context.Context, req *v1.QueryPreviousValidatorSetRequest) (*v1.QueryPreviousValidatorSetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	set := q.Keeper.PreviousValidatorSet()
	return &v1.QueryPreviousValidatorSetResponse{
		Validators: validatorPowerSliceNativeToProto(set),
	}, nil
}

func (q queryServer) CurrentValidatorSet(ctx context.Context, req *v1.QueryCurrentValidatorSetRequest) (*v1.QueryCurrentValidatorSetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	set := q.Keeper.CurrentValidatorSet()
	return &v1.QueryCurrentValidatorSetResponse{
		Validators: validatorPowerSliceNativeToProto(set),
	}, nil
}

func (q queryServer) NextValidatorSet(ctx context.Context, req *v1.QueryNextValidatorSetRequest) (*v1.QueryNextValidatorSetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	set := q.Keeper.NextValidatorSet()
	return &v1.QueryNextValidatorSetResponse{
		Validators: validatorPowerSliceNativeToProto(set),
	}, nil
}

func (q queryServer) Election(ctx context.Context, req *v1.QueryElectionRequest) (*v1.QueryElectionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	state := q.Keeper.Election()
	return &v1.QueryElectionResponse{
		Election: stateNativeToProto(state),
	}, nil
}

func (q queryServer) ElectionCandidates(ctx context.Context, req *v1.QueryElectionCandidatesRequest) (*v1.QueryElectionCandidatesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	candidates := q.Keeper.ElectionCandidates()
	return &v1.QueryElectionCandidatesResponse{
		Candidates: candidateApplicationSliceNativeToProto(candidates),
	}, nil
}

func (q queryServer) FrozenStake(ctx context.Context, req *v1.QueryFrozenStakeRequest) (*v1.QueryFrozenStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	stakes, err := q.Keeper.FrozenStake(req.OperatorAddress)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryFrozenStakeResponse{
		FrozenStakes: frozenStakeSliceNativeToProto(stakes),
	}, nil
}

func (q queryServer) ValidatorSetTransition(ctx context.Context, req *v1.QueryValidatorSetTransitionRequest) (*v1.QueryValidatorSetTransitionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	transition, found := q.Keeper.ValidatorSetTransition(req.Epoch)
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	protoTransition := validatorSetTransitionNativeToProto(transition)
	return &v1.QueryValidatorSetTransitionResponse{
		Transition: &protoTransition,
	}, nil
}
