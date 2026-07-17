package keeper

import (
	"context"
	"encoding/hex"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sovereign-l1/l1/x/aez/types"
)

type queryServer struct {
	keeper *Keeper
}

// NewQueryServerImpl returns the x/aez Query service implementation.
//
// Query only: x/aez has no Msg service in Phase 1, so nothing here mutates
// state. Every method is a pure read of committed state.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return queryServer{keeper: k}
}

var _ types.QueryServer = queryServer{}

func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	params, err := q.keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

func (q queryServer) RoutingTable(ctx context.Context, req *types.QueryRoutingTableRequest) (*types.QueryRoutingTableResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	var (
		table	types.RoutingTable
		err	error
	)
	if req.Version == 0 {
		table, err = q.keeper.GetRoutingTable(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else {
		var found bool
		table, found, err = q.keeper.GetRoutingTableVersion(ctx, req.Version)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if !found {
			return nil, status.Errorf(codes.NotFound, "routing table version %d not found", req.Version)
		}
	}
	buckets := make([]uint32, 0, types.BucketCount)
	for i := 0; i < types.BucketCount; i++ {
		buckets = append(buckets, uint32(table.Buckets[i]))
	}
	return &types.QueryRoutingTableResponse{
		Version:		table.Version,
		Epoch:			table.Epoch,
		ActivationHeight:	table.ActivationHeight,
		Buckets:		buckets,
		TableHash:		hex.EncodeToString(table.TableHash),
	}, nil
}

func (q queryServer) Zones(ctx context.Context, req *types.QueryZonesRequest) (*types.QueryZonesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	zones, err := q.keeper.GetAllZones(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]types.QueryZone, 0, len(zones))
	for _, zone := range zones {
		out = append(out, types.QueryZone{ID: uint32(zone.ID), Kind: string(zone.Kind)})
	}
	return &types.QueryZonesResponse{Zones: out}, nil
}

func (q queryServer) ZoneOf(ctx context.Context, req *types.QueryZoneOfRequest) (*types.QueryZoneOfResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	resolution, err := q.keeper.ZoneOfEntity(ctx, types.EntityKind(req.Kind), req.Entity)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &types.QueryZoneOfResponse{
		Zone:		uint32(resolution.Zone),
		Namespace:	string(resolution.Namespace),
		Pinned:		resolution.Pinned,
		Hashed:		resolution.Hashed,
		Bucket:		uint32(resolution.Bucket),
		TableVersion:	resolution.TableVersion,
	}, nil
}
