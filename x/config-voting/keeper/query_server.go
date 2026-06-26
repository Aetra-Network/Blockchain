package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/configvoting/v1"
	"github.com/sovereign-l1/l1/x/config-voting/types"
)

var _ v1.QueryServer = queryServer{}

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

func (q queryServer) ConfigProposal(ctx context.Context, req *v1.QueryConfigProposalRequest) (*v1.QueryConfigProposalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	proposal, found, err := q.Keeper.ConfigProposal(req.ProposalId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, "config proposal not found")
	}
	return &v1.QueryConfigProposalResponse{
		Proposal: configProposalNativeToProto(proposal),
	}, nil
}

func (q queryServer) ConfigProposals(ctx context.Context, req *v1.QueryConfigProposalsRequest) (*v1.QueryConfigProposalsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	proposals, err := q.Keeper.ConfigProposals()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryConfigProposalsResponse{
		Proposals: configProposalSliceNativeToProto(proposals),
	}, nil
}

func (q queryServer) ConfigVotes(ctx context.Context, req *v1.QueryConfigVotesRequest) (*v1.QueryConfigVotesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	proposalID := req.ProposalId
	votes, err := q.Keeper.ConfigVotes(proposalID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryConfigVotesResponse{
		Votes: configVoteSliceNativeToProto(votes),
	}, nil
}

func (q queryServer) ConfigVotingParams(ctx context.Context, req *v1.QueryConfigVotingParamsRequest) (*v1.QueryConfigVotingParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	params, err := q.Keeper.ConfigVotingParams()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryConfigVotingParamsResponse{
		Params: configVotingParamsNativeToProto(params),
	}, nil
}

func configProposalSliceNativeToProto(ns []types.ConfigProposal) []v1.ConfigProposal {
	out := make([]v1.ConfigProposal, len(ns))
	for i, n := range ns {
		out[i] = configProposalNativeToProto(n)
	}
	return out
}

func configVoteSliceNativeToProto(ns []types.ConfigVote) []v1.ConfigVote {
	out := make([]v1.ConfigVote, len(ns))
	for i, n := range ns {
		out[i] = configVoteNativeToProto(n)
	}
	return out
}