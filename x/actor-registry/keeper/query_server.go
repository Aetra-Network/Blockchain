package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/sovereign-l1/l1/api/l1/actorregistry/v1"
)

type queryServer struct {
	*Keeper
}

func NewQueryServerImpl(k *Keeper) v1.QueryServer {
	return queryServer{Keeper: k}
}

var _ v1.QueryServer = queryServer{}

func (q queryServer) Actor(ctx context.Context, req *v1.QueryActorRequest) (*v1.QueryActorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	actor, found, err := q.Keeper.Actor(req.ActorId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	return &v1.QueryActorResponse{
		Actor: actorRecordNativeToProto(actor),
	}, nil
}

func (q queryServer) ActorsByOwner(ctx context.Context, req *v1.QueryActorsByOwnerRequest) (*v1.QueryActorsByOwnerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	actors, err := q.Keeper.ActorsByOwner(req.Owner)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryActorsByOwnerResponse{
		Actors: actorRecordSliceNativeToProto(actors),
	}, nil
}

func (q queryServer) ActorsByCodeHash(ctx context.Context, req *v1.QueryActorsByCodeHashRequest) (*v1.QueryActorsByCodeHashResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	actors, err := q.Keeper.ActorsByCodeHash(req.CodeHash)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &v1.QueryActorsByCodeHashResponse{
		Actors: actorRecordSliceNativeToProto(actors),
	}, nil
}

func (q queryServer) ActorStatus(ctx context.Context, req *v1.QueryActorStatusRequest) (*v1.QueryActorStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	statusStr, found, err := q.Keeper.ActorStatus(req.ActorId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	return &v1.QueryActorStatusResponse{
		Status: statusStr,
	}, nil
}

func (q queryServer) ActorMailbox(ctx context.Context, req *v1.QueryActorMailboxRequest) (*v1.QueryActorMailboxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	mailboxRoot, found, err := q.Keeper.ActorMailbox(req.ActorId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	return &v1.QueryActorMailboxResponse{
		MailboxRoot: mailboxRoot,
	}, nil
}

func (q queryServer) ActorStorageRoot(ctx context.Context, req *v1.QueryActorStorageRootRequest) (*v1.QueryActorStorageRootResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	storageRoot, found, err := q.Keeper.ActorStorageRoot(req.ActorId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, v1.ErrNotFound.Error())
	}
	return &v1.QueryActorStorageRootResponse{
		StorageRoot: storageRoot,
	}, nil
}