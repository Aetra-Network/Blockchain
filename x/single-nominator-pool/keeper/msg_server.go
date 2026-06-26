package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	v1 "github.com/sovereign-l1/l1/api/l1/singlenominatorpool/v1"
	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/single-nominator-pool/types"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

func NewMsgServerImpl(k *Keeper) v1.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) CreateSingleNominatorPool(ctx context.Context, msg *v1.MsgCreateSingleNominatorPool) (*v1.MsgCreateSingleNominatorPoolResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	m.Keeper.runtimeCtx = ctx
	nativeMsg := types.MsgCreateSingleNominatorPool{
		Authority:       msg.Authority,
		PoolAddress:     msg.PoolAddress,
		Owner:           msg.Owner,
		Validator:       msg.Validator,
		ValidatorStatus: msg.ValidatorStatus,
		Height:          msg.Height,
	}
	pool, err := m.Keeper.CreateSingleNominatorPool(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeCreateSingleNominatorPool,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyPoolAddress, msg.PoolAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgCreateSingleNominatorPoolResponse{
		Pool: singleNominatorPoolNativeToProto(pool),
	}, nil
}

func (m msgServer) DepositSingleNominator(ctx context.Context, msg *v1.MsgDepositSingleNominator) (*v1.MsgDepositSingleNominatorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	m.Keeper.runtimeCtx = ctx
	nativeMsg := types.MsgDepositSingleNominator{
		Authority:   msg.Authority,
		PoolAddress: msg.PoolAddress,
		Owner:       msg.Owner,
		Amount:      msg.Amount,
		Height:      msg.Height,
	}
	pool, err := m.Keeper.DepositSingleNominator(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeDepositSingleNominator,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyPoolAddress, msg.PoolAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgDepositSingleNominatorResponse{
		Pool: singleNominatorPoolNativeToProto(pool),
	}, nil
}

func (m msgServer) WithdrawSingleNominator(ctx context.Context, msg *v1.MsgWithdrawSingleNominator) (*v1.MsgWithdrawSingleNominatorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	m.Keeper.runtimeCtx = ctx
	nativeMsg := types.MsgWithdrawSingleNominator{
		Authority:   msg.Authority,
		PoolAddress: msg.PoolAddress,
		Owner:       msg.Owner,
		Amount:      msg.Amount,
		Height:      msg.Height,
	}
	withdrawal, err := m.Keeper.WithdrawSingleNominator(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeWithdrawSingleNominator,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyPoolAddress, msg.PoolAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgWithdrawSingleNominatorResponse{
		Withdrawal: pendingWithdrawalNativeToProto(withdrawal),
	}, nil
}

func (m msgServer) ClaimSingleNominatorRewards(ctx context.Context, msg *v1.MsgClaimSingleNominatorRewards) (*v1.MsgClaimSingleNominatorRewardsResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	m.Keeper.runtimeCtx = ctx
	nativeMsg := types.MsgClaimSingleNominatorRewards{
		Authority:   msg.Authority,
		PoolAddress: msg.PoolAddress,
		Owner:       msg.Owner,
		Height:      msg.Height,
	}
	reward, err := m.Keeper.ClaimSingleNominatorRewards(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeClaimSingleNominatorRewards,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyPoolAddress, msg.PoolAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgClaimSingleNominatorRewardsResponse{
		RewardAmount: reward,
	}, nil
}

func (m msgServer) EmergencyLockSingleNominator(ctx context.Context, msg *v1.MsgEmergencyLockSingleNominator) (*v1.MsgEmergencyLockSingleNominatorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	m.Keeper.runtimeCtx = ctx
	nativeMsg := types.MsgEmergencyLockSingleNominator{
		Authority:   msg.Authority,
		PoolAddress: msg.PoolAddress,
		Owner:       msg.Owner,
		Locked:      msg.Locked,
		Height:      msg.Height,
	}
	pool, err := m.Keeper.EmergencyLockSingleNominator(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeEmergencyLockSingleNominator,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyPoolAddress, msg.PoolAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgEmergencyLockSingleNominatorResponse{
		Pool: singleNominatorPoolNativeToProto(pool),
	}, nil
}

func (m msgServer) ChangeSingleNominatorValidator(ctx context.Context, msg *v1.MsgChangeSingleNominatorValidator) (*v1.MsgChangeSingleNominatorValidatorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	m.Keeper.runtimeCtx = ctx
	nativeMsg := types.MsgChangeSingleNominatorValidator{
		Authority:       msg.Authority,
		PoolAddress:     msg.PoolAddress,
		Owner:           msg.Owner,
		Validator:       msg.Validator,
		ValidatorStatus: msg.ValidatorStatus,
		Height:          msg.Height,
	}
	pool, err := m.Keeper.ChangeSingleNominatorValidator(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeChangeSingleNominatorValidator,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyPoolAddress, msg.PoolAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgChangeSingleNominatorValidatorResponse{
		Pool: singleNominatorPoolNativeToProto(pool),
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

func singleNominatorPoolNativeToProto(n types.SingleNominatorPool) v1.SingleNominatorPool {
	return v1.SingleNominatorPool{
		PoolAddress:       n.PoolAddress,
		Owner:             n.Owner,
		Validator:         n.Validator,
		BondedStake:       n.BondedStake,
		PendingWithdrawal: pendingWithdrawalNativeToProto(n.PendingWithdrawal),
		RewardBalance:     n.RewardBalance,
		EmergencyLock:     n.EmergencyLock,
		Status:            n.Status,
	}
}

func singleNominatorPoolSliceNativeToProto(ns []types.SingleNominatorPool) []v1.SingleNominatorPool {
	out := make([]v1.SingleNominatorPool, len(ns))
	for i, n := range ns {
		out[i] = singleNominatorPoolNativeToProto(n)
	}
	return out
}

func pendingWithdrawalNativeToProto(n types.PendingWithdrawal) v1.PendingWithdrawal {
	return v1.PendingWithdrawal{
		Amount:         n.Amount,
		RequestHeight:  n.RequestHeight,
		CompleteHeight: n.CompleteHeight,
		Status:         n.Status,
	}
}
