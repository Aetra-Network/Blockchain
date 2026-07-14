package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	v1 "github.com/sovereign-l1/l1/api/l1/actorregistry/v1"
	"github.com/sovereign-l1/l1/x/actor-registry/types"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

func NewMsgServerImpl(k *Keeper) v1.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) RegisterActor(ctx context.Context, msg *v1.MsgRegisterActor) (*v1.MsgRegisterActorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgRegisterActor{
		Authority:       msg.Authority,
		Owner:           msg.Owner,
		CodeHash:        msg.CodeHash,
		Salt:            msg.Salt,
		ActorID:         msg.ActorId,
		ContractAddress: msg.ContractAddress,
		StorageRoot:     msg.StorageRoot,
		MailboxRoot:     msg.MailboxRoot,
		Balance:         msg.Balance,
		Height:          msg.Height,
		Capabilities:    msg.Capabilities,
	}
	actor, err := m.Keeper.RegisterActor(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeRegisterActor,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyActorID, actor.ActorID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgRegisterActorResponse{
		Actor: actorRecordNativeToProto(actor),
	}, nil
}

func (m msgServer) UpdateActorCode(ctx context.Context, msg *v1.MsgUpdateActorCode) (*v1.MsgUpdateActorCodeResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgUpdateActorCode{
		Authority:   msg.Authority,
		ActorID:     msg.ActorId,
		CodeHash:    msg.CodeHash,
		Height:      msg.Height,
		LogicalTime: msg.LogicalTime,
	}
	actor, err := m.Keeper.UpdateActorCode(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeUpdateActorCode,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyActorID, actor.ActorID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgUpdateActorCodeResponse{
		Actor: actorRecordNativeToProto(actor),
	}, nil
}

func (m msgServer) FreezeActor(ctx context.Context, msg *v1.MsgFreezeActor) (*v1.MsgFreezeActorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgFreezeActor{
		Authority: msg.Authority,
		ActorID:   msg.ActorId,
		Height:    msg.Height,
	}
	actor, err := m.Keeper.FreezeActor(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeFreezeActor,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyActorID, actor.ActorID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgFreezeActorResponse{
		Actor: actorRecordNativeToProto(actor),
	}, nil
}

func (m msgServer) UnfreezeActor(ctx context.Context, msg *v1.MsgUnfreezeActor) (*v1.MsgUnfreezeActorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgUnfreezeActor{
		Authority: msg.Authority,
		ActorID:   msg.ActorId,
		Height:    msg.Height,
	}
	actor, err := m.Keeper.UnfreezeActor(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeUnfreezeActor,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyActorID, actor.ActorID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgUnfreezeActorResponse{
		Actor: actorRecordNativeToProto(actor),
	}, nil
}

func (m msgServer) DeleteActor(ctx context.Context, msg *v1.MsgDeleteActor) (*v1.MsgDeleteActorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgDeleteActor{
		Authority: msg.Authority,
		ActorID:   msg.ActorId,
		Height:    msg.Height,
	}
	actor, err := m.Keeper.DeleteActor(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeDeleteActor,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyActorID, actor.ActorID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgDeleteActorResponse{
		Actor: actorRecordNativeToProto(actor),
	}, nil
}

func (m msgServer) MigrateActor(ctx context.Context, msg *v1.MsgMigrateActor) (*v1.MsgMigrateActorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgMigrateActor{
		Authority:      msg.Authority,
		ActorID:        msg.ActorId,
		NewCodeHash:    msg.NewCodeHash,
		NewStorageRoot: msg.NewStorageRoot,
		NewMailboxRoot: msg.NewMailboxRoot,
		NewActorID:     msg.NewActorId,
		NewAddress:     msg.NewAddress,
		Height:         msg.Height,
		LogicalTime:    msg.LogicalTime,
	}
	actor, err := m.Keeper.MigrateActor(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeMigrateActor,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyActorID, actor.ActorID),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgMigrateActorResponse{
		Actor: actorRecordNativeToProto(actor),
	}, nil
}

func (m msgServer) requireAuthority(authority string) error {
	if err := aetraaddress.ValidateAuthorityAddress("authority", authority); err != nil {
		return v1.ErrUnauthorized.Wrap(err.Error())
	}
	if authority != m.Keeper.genesis.Params.Authority {
		return v1.ErrUnauthorized.Wrap("invalid authority")
	}
	return nil
}

func actorRecordNativeToProto(n types.ActorRecord) v1.ActorRecord {
	return v1.ActorRecord{
		ActorId:          n.ActorID,
		ContractAddress:  n.ContractAddress,
		Owner:            n.Owner,
		CodeHash:         n.CodeHash,
		StorageRoot:      n.StorageRoot,
		MailboxRoot:      n.MailboxRoot,
		Balance:          n.Balance,
		LogicalTime:      n.LogicalTime,
		Status:           n.Status,
		RentStatus:       n.RentStatus,
		LastActiveHeight: n.LastActiveHeight,
		Capabilities:     n.Capabilities,
		MigratedFrom:     n.MigratedFrom,
		MigratedTo:       n.MigratedTo,
	}
}

func actorRecordSliceNativeToProto(ns []types.ActorRecord) []v1.ActorRecord {
	out := make([]v1.ActorRecord, len(ns))
	for i, n := range ns {
		out[i] = actorRecordNativeToProto(n)
	}
	return out
}