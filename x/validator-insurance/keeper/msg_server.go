package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	addressing "github.com/sovereign-l1/l1/app/addressing"
	v1 "github.com/sovereign-l1/l1/api/l1/validatorinsurance/v1"
	"github.com/sovereign-l1/l1/x/validator-insurance/types"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

func NewMsgServerImpl(k *Keeper) v1.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) FundValidatorInsurance(ctx context.Context, msg *v1.MsgFundValidatorInsurance) (*v1.MsgFundValidatorInsuranceResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgFundValidatorInsurance{
		Authority:        msg.Authority,
		ValidatorAddress: msg.ValidatorAddress,
		Funder:           msg.Funder,
		Amount:           msg.Amount,
		Height:           msg.Height,
	}
	insurance, err := m.Keeper.FundValidatorInsurance(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeFundValidatorInsurance,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyValidatorAddress, msg.ValidatorAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgFundValidatorInsuranceResponse{
		Insurance: validatorInsuranceNativeToProto(insurance),
	}, nil
}

func (m msgServer) WithdrawValidatorInsurance(ctx context.Context, msg *v1.MsgWithdrawValidatorInsurance) (*v1.MsgWithdrawValidatorInsuranceResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgWithdrawValidatorInsurance{
		Authority:        msg.Authority,
		ValidatorAddress: msg.ValidatorAddress,
		Recipient:        msg.Recipient,
		Amount:           msg.Amount,
		Height:           msg.Height,
		ValidatorStatus:  msg.ValidatorStatus,
	}
	withdrawal, err := m.Keeper.WithdrawValidatorInsurance(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeWithdrawValidatorInsurance,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyValidatorAddress, msg.ValidatorAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgWithdrawValidatorInsuranceResponse{
		Withdrawal: pendingInsuranceWithdrawalNativeToProto(withdrawal),
	}, nil
}

func (m msgServer) SubmitInsuranceClaim(ctx context.Context, msg *v1.MsgSubmitInsuranceClaim) (*v1.MsgSubmitInsuranceClaimResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgSubmitInsuranceClaim{
		Authority:        msg.Authority,
		ClaimID:          msg.ClaimId,
		ValidatorAddress: msg.ValidatorAddress,
		Claimant:         msg.Claimant,
		Amount:           msg.Amount,
		Reason:           msg.Reason,
		Height:           msg.Height,
	}
	claim, err := m.Keeper.SubmitInsuranceClaim(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeSubmitInsuranceClaim,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyClaimID, msg.ClaimId),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgSubmitInsuranceClaimResponse{
		Claim: insuranceClaimNativeToProto(claim),
	}, nil
}

func (m msgServer) ResolveInsuranceClaim(ctx context.Context, msg *v1.MsgResolveInsuranceClaim) (*v1.MsgResolveInsuranceClaimResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgResolveInsuranceClaim{
		Authority: msg.Authority,
		ClaimID:   msg.ClaimId,
		Approved:  msg.Approved,
		Height:    msg.Height,
	}
	claim, err := m.Keeper.ResolveInsuranceClaim(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeResolveInsuranceClaim,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyClaimID, msg.ClaimId),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgResolveInsuranceClaimResponse{
		Claim: insuranceClaimNativeToProto(claim),
	}, nil
}

func (m msgServer) requireAuthority(authority string) error {
	if err := addressing.ValidateAuthorityAddress("authority", authority); err != nil {
		return v1.ErrUnauthorized.Wrap(err.Error())
	}
	if authority != m.Keeper.genesis.Params.Authority {
		return v1.ErrUnauthorized.Wrap("invalid authority")
	}
	return nil
}

func validatorInsuranceNativeToProto(n types.ValidatorInsurance) v1.ValidatorInsurance {
	return v1.ValidatorInsurance{
		ValidatorAddress:  n.ValidatorAddress,
		Balance:           n.Balance,
		PendingWithdrawal: pendingInsuranceWithdrawalNativeToProto(n.PendingWithdrawal),
		ValidatorStatus:   n.ValidatorStatus,
	}
}

func pendingInsuranceWithdrawalNativeToProto(n types.PendingInsuranceWithdrawal) v1.PendingInsuranceWithdrawal {
	return v1.PendingInsuranceWithdrawal{
		Amount:         n.Amount,
		Recipient:      n.Recipient,
		RequestHeight:  n.RequestHeight,
		CompleteHeight: n.CompleteHeight,
		Status:         n.Status,
	}
}

func insuranceClaimNativeToProto(n types.InsuranceClaim) v1.InsuranceClaim {
	return v1.InsuranceClaim{
		ClaimId:          n.ClaimID,
		ValidatorAddress: n.ValidatorAddress,
		Claimant:         n.Claimant,
		Amount:           n.Amount,
		PayoutAmount:     n.PayoutAmount,
		Status:           n.Status,
		Reason:           n.Reason,
		SubmittedHeight:  n.SubmittedHeight,
		ResolvedHeight:   n.ResolvedHeight,
		Paid:             n.Paid,
	}
}

func insuranceClaimSliceNativeToProto(ns []types.InsuranceClaim) []v1.InsuranceClaim {
	out := make([]v1.InsuranceClaim, len(ns))
	for i, n := range ns {
		out[i] = insuranceClaimNativeToProto(n)
	}
	return out
}

func paramsNativeToProto(n types.Params) v1.Params {
	return v1.Params{
		Authority:               n.Authority,
		Enabled:                 n.Enabled,
		MinimumInsurance:        n.MinimumInsurance,
		WithdrawalLockBlocks:    n.WithdrawalLockBlocks,
		DefaultSlashCoverageBps: n.DefaultSlashCoverageBps,
		MaxValidators:           n.MaxValidators,
		MaxClaims:               n.MaxClaims,
		MaxClaimIdBytes:         n.MaxClaimIDBytes,
		MaxReasonBytes:          n.MaxReasonBytes,
		MaxCoverageRules:        n.MaxCoverageRules,
		MaxFaultTypeBytes:       n.MaxFaultTypeBytes,
	}
}