package keeper

import (
	"context"
	"encoding/hex"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/aez/types"
)

type queryServer struct {
	keeper *Keeper
}

// NewQueryServerImpl returns the x/aez Query service implementation.
//
// Every method is a pure read of committed state. The write surface is the Msg
// service (keeper/msg_server.go); nothing here mutates anything.
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
	return routingTableResponse(table), nil
}

// PendingRoutingTable reports the table scheduled to activate, if any.
//
// This is the query that makes a scheduled swap visible BEFORE it happens. The
// current table alone cannot show it: a governance proposal that passes at
// height H may not take effect for thousands of blocks, and during that whole
// window RoutingTable() answers with the OLD table and nothing in its response
// hints that a replacement is already committed and inevitable.
func (q queryServer) PendingRoutingTable(ctx context.Context, req *types.QueryPendingRoutingTableRequest) (*types.QueryPendingRoutingTableResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	version, found, err := q.keeper.GetPendingVersion(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return &types.QueryPendingRoutingTableResponse{Found: false}, nil
	}
	table, found, err := q.keeper.GetRoutingTableVersion(ctx, version)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		// The pending pointer names a version whose table is missing. That
		// is a corrupt store, not an empty result: report it rather than
		// answering "nothing pending", which would hide the fault and is
		// also the exact state that would make the next BeginBlocker fail.
		return nil, status.Errorf(codes.Internal, "pending routing table version %d is missing", version)
	}
	return &types.QueryPendingRoutingTableResponse{
		Found:			true,
		Table:			*routingTableResponse(table),
		BlocksUntilActivation:	table.ActivationHeight - sdk.UnwrapSDKContext(ctx).BlockHeight(),
	}, nil
}

func routingTableResponse(table types.RoutingTable) *types.QueryRoutingTableResponse {
	return &types.QueryRoutingTableResponse{
		Version:		table.Version,
		Epoch:			table.Epoch,
		ActivationHeight:	table.ActivationHeight,
		Buckets:		types.BucketsFromTable(table),
		TableHash:		hex.EncodeToString(table.TableHash),
	}
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
