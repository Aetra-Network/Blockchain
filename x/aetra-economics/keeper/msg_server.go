package keeper

import (
	"context"

	"github.com/sovereign-l1/l1/x/aetra-economics/types"
)

type MsgServer struct {
	Keeper *Keeper
}

func NewMsgServerImpl(k *Keeper) MsgServer {
	return MsgServer{Keeper: k}
}

func (m MsgServer) UpdateEconomicsParams(ctx context.Context, msg types.MsgUpdateEconomicsParams) error {
	if err := m.requireAuthority(msg.Authority); err != nil {
		return err
	}
	return m.Keeper.SetParams(ctx, msg.Params)
}

func (m MsgServer) ApplyEpochEconomics(ctx context.Context, msg types.MsgApplyEpochEconomics) error {
	if err := m.requireAuthority(msg.Authority); err != nil {
		return err
	}
	_, err := m.Keeper.ApplyEpoch(ctx, msg.Input)
	return err
}

func (m MsgServer) requireAuthority(authority string) error {
	if authority != m.Keeper.Authority() {
		return types.ErrUnauthorized.Wrap("invalid authority")
	}
	return nil
}
