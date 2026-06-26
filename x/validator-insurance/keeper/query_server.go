package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/validatorinsurance/v1"
)

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

var _ v1.QueryServer = queryServer{}

func (q queryServer) ValidatorInsurance(ctx context.Context, req *v1.QueryValidatorInsuranceRequest) (*v1.QueryValidatorInsuranceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	insurance, found := q.Keeper.ValidatorInsurance(req.ValidatorAddress)
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrInsuranceNotFound.Error())
	}
	protoInsurance := validatorInsuranceNativeToProto(insurance)
	return &v1.QueryValidatorInsuranceResponse{
		Insurance: &protoInsurance,
	}, nil
}

func (q queryServer) InsuranceClaims(ctx context.Context, req *v1.QueryInsuranceClaimsRequest) (*v1.QueryInsuranceClaimsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	claims := q.Keeper.InsuranceClaims(req.ValidatorAddress)
	return &v1.QueryInsuranceClaimsResponse{
		Claims: insuranceClaimSliceNativeToProto(claims),
	}, nil
}

func (q queryServer) InsuranceParams(ctx context.Context, req *v1.QueryInsuranceParamsRequest) (*v1.QueryInsuranceParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	params := q.Keeper.InsuranceParams()
	return &v1.QueryInsuranceParamsResponse{
		Params: paramsNativeToProto(params),
	}, nil
}