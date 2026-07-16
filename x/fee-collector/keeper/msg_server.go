package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/fee-collector/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	Keeper
}

func NewMsgServerImpl(k Keeper) types.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) DistributeFees(ctx context.Context, msg *types.MsgDistributeFees) (*types.MsgDistributeFeesResponse, error) {
	if msg == nil {
		return nil, types.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	// The FeeHistory store is keyed by block height for the automatic
	// EndBlock distribution (see module.go). A caller-supplied epoch that
	// does not match the current height could plant an entry at a future
	// height; when the chain naturally reaches that height, the automatic
	// distribution would collide with ErrDuplicateHistory. Reject any epoch
	// that isn't the current height to keep the two write paths from ever
	// targeting different keys for the same block.
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if msg.Epoch != height {
		return nil, types.ErrInvalidParams.Wrapf("epoch must equal current height %d, got %d", height, msg.Epoch)
	}
	history, err := m.Keeper.DistributeFees(ctx, msg.Epoch)
	if err != nil {
		return nil, err
	}
	return &types.MsgDistributeFeesResponse{History: history}, nil
}

func (m msgServer) UpdateFeeDistributionParams(ctx context.Context, msg *types.MsgUpdateFeeDistributionParams) (*types.MsgUpdateFeeDistributionParamsResponse, error) {
	if msg == nil {
		return nil, types.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	if err := m.Keeper.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeUpdateDistribution,
		sdk.NewAttribute(types.AttributeKeyAuthority, msg.Authority),
	))
	return &types.MsgUpdateFeeDistributionParamsResponse{}, nil
}

func (m msgServer) requireAuthority(authority string) error {
	if err := aetraaddress.ValidateAuthorityAddress("authority", authority); err != nil {
		return types.ErrUnauthorized.Wrap(err.Error())
	}
	if authority != m.Authority() {
		return types.ErrUnauthorized.Wrap("invalid authority")
	}
	return nil
}
