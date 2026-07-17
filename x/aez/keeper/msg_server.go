package keeper

import (
	"context"
	"encoding/hex"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sovereign-l1/l1/x/aez/types"
)

type msgServer struct {
	keeper *Keeper
}

// NewMsgServerImpl returns the x/aez Msg service implementation.
//
// x/aez has exactly ONE Msg, and it is the module's entire consensus-reachable
// write surface. Everything else about the module is either genesis or a pure
// read.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{keeper: k}
}

var _ types.MsgServer = msgServer{}

// UpdateRoutingTable stages a routing table for a future routing-epoch boundary.
//
// It STAGES. It never applies. The swap is the BeginBlocker's job at
// ActivationHeight (keeper/abci.go), which is what keeps every transaction
// inside one block resolving against one table.
//
// Authorization is Params.Prototype.Authorize -- an exact match against
// Params.Prototype.Authority, which DefaultParams points at the gov module
// account (types.GovAuthority). Two layers must agree before this handler runs
// at all: the signing context resolves the tx's signer FROM this same authority
// field (types.MsgUpdateRoutingTableSigners), and SigVerificationDecorator
// independently checks that signer against the tx's pubkey. So a caller cannot
// simply write the gov address into the field -- they would also have to produce
// gov's signature, which only a passed proposal can.
//
// Note this is gated on the AUTHORITY, not on Params.Prototype.Enabled. Enabled
// is false at genesis (I-23) and x/aez ships no param-update Msg, so nothing on a
// live chain could ever flip it: gating here would make this handler permanently
// unreachable -- the same dead-handler bug as a keyless authority, arrived at
// from the other direction. The safety of this path rests on the authority check
// plus ValidateRoutingTableTransition's core-zone trap, neither of which Enabled
// contributes to.
func (m msgServer) UpdateRoutingTable(ctx context.Context, msg *types.MsgUpdateRoutingTable) (*types.MsgUpdateRoutingTableResponse, error) {
	if msg == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	params, err := m.keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := params.Prototype.Authorize(msg.Authority); err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}
	table, err := types.RoutingTableFromMsg(msg)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := m.keeper.StageRoutingTable(ctx, table, msg.Authority); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &types.MsgUpdateRoutingTableResponse{
		Version:		table.Version,
		Epoch:			table.Epoch,
		ActivationHeight:	table.ActivationHeight,
		TableHash:		hex.EncodeToString(table.TableHash),
	}, nil
}
