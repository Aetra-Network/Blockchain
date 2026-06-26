package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/evidence/v1"
	"github.com/sovereign-l1/l1/x/evidence/types"
)

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

var _ v1.QueryServer = queryServer{}

func (q queryServer) Evidence(ctx context.Context, req *v1.QueryEvidenceRequest) (*v1.QueryEvidenceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	record, found := q.Keeper.Evidence(req.EvidenceId)
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	return &v1.QueryEvidenceResponse{
		Evidence: evidenceRecordNativeToProto(record),
	}, nil
}

func (q queryServer) EvidenceByValidator(ctx context.Context, req *v1.QueryEvidenceByValidatorRequest) (*v1.QueryEvidenceByValidatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	records := q.Keeper.EvidenceByValidator(req.ValidatorAddress)
	return &v1.QueryEvidenceByValidatorResponse{
		Evidence: evidenceRecordSliceNativeToProto(records),
	}, nil
}

func (q queryServer) EvidenceByReporter(ctx context.Context, req *v1.QueryEvidenceByReporterRequest) (*v1.QueryEvidenceByReporterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	records := q.Keeper.EvidenceByReporter(req.Reporter)
	return &v1.QueryEvidenceByReporterResponse{
		Evidence: evidenceRecordSliceNativeToProto(records),
	}, nil
}

func (q queryServer) PendingEvidence(ctx context.Context, req *v1.QueryPendingEvidenceRequest) (*v1.QueryPendingEvidenceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	records := q.Keeper.PendingEvidence()
	return &v1.QueryPendingEvidenceResponse{
		Evidence: evidenceRecordSliceNativeToProto(records),
	}, nil
}

func (q queryServer) EvidenceParams(ctx context.Context, req *v1.QueryEvidenceParamsRequest) (*v1.QueryEvidenceParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	params := q.Keeper.EvidenceParams()
	return &v1.QueryEvidenceParamsResponse{
		Params: paramsNativeToProto(params),
	}, nil
}

func paramsNativeToProto(n types.Params) v1.Params {
	return v1.Params{
		Authority:                     n.Authority,
		MaxEvidence:                   n.MaxEvidence,
		MaxPendingEvidence:            n.MaxPendingEvidence,
		MaxProofHashBytes:            n.MaxProofHashBytes,
		MaxPayloadBytes:              n.MaxPayloadBytes,
		MaxVotes:                      n.MaxVotes,
		MaxSideEffectHistory:         n.MaxSideEffectHistory,
		EvidenceTtlBlocks:             n.EvidenceTTLBlocks,
		ReviewQuorumBps:              n.ReviewQuorumBps,
		MinSlashFractionBps:           n.MinSlashFractionBps,
		MaxSlashFractionBps:           n.MaxSlashFractionBps,
		CriticalFaultSlashFractionBps: n.CriticalFaultSlashFractionBps,
		MaxReporterRewardNaet:         n.MaxReporterRewardNaet,
	}
}