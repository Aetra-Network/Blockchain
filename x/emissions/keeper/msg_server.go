package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/emissions/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct{ Keeper }

func NewMsgServerImpl(k Keeper) types.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) UpdateEmissionsParams(ctx context.Context, msg *types.MsgUpdateEmissionsParams) (*types.MsgUpdateEmissionsParamsResponse, error) {
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
		types.EventTypeUpdateParams,
		sdk.NewAttribute(types.AttributeKeyAuthority, msg.Authority),
	))
	return &types.MsgUpdateEmissionsParamsResponse{}, nil
}

func (m msgServer) FinalizeEmissionEpoch(ctx context.Context, msg *types.MsgFinalizeEmissionEpoch) (*types.MsgFinalizeEmissionEpochResponse, error) {
	if msg == nil {
		return nil, types.ErrInvalidEpoch.Wrap("empty request")
	}
	// SA2-S06: emission epochs are finalized exclusively by the protocol
	// EndBlocker (app/native_economy.go), which finalizes the epoch, mints the
	// emission, and enforces the mint-authority caps in a single commit.
	// Finalizing through this standalone authority message records the epoch and
	// advances TotalMintedAccounting WITHOUT minting or cap-checking; the
	// EndBlocker then skips that epoch as already-finalized, suppressing the real
	// emission and overstating accounting versus bank supply. Reject it.
	return nil, types.ErrUnauthorized.Wrap("emission epochs are finalized by the protocol end-blocker, not by a standalone message")
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
