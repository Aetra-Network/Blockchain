package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	addressing "github.com/sovereign-l1/l1/app/addressing"
	v1 "github.com/sovereign-l1/l1/api/l1/validatorregistry/v1"
	"github.com/sovereign-l1/l1/x/validator-registry/types"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

func NewMsgServerImpl(k *Keeper) v1.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) RegisterValidator(ctx context.Context, msg *v1.MsgRegisterValidator) (*v1.MsgRegisterValidatorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgRegisterValidator{
		Authority: msg.Authority,
		Validator: validatorRecordProtoToNative(msg.Validator),
		Height:    msg.Height,
	}
	validator, err := m.Keeper.RegisterValidator(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeRegisterValidator,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperatorAddress, validator.OperatorAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgRegisterValidatorResponse{
		Validator: validatorRecordNativeToProto(validator),
	}, nil
}

func (m msgServer) UpdateValidatorMetadata(ctx context.Context, msg *v1.MsgUpdateValidatorMetadata) (*v1.MsgUpdateValidatorMetadataResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgUpdateValidatorMetadata{
		Authority:       msg.Authority,
		OperatorAddress: msg.OperatorAddress,
		Metadata:        msg.Metadata,
		Height:          msg.Height,
	}
	validator, err := m.Keeper.UpdateValidatorMetadata(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeUpdateValidatorMetadata,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperatorAddress, validator.OperatorAddress),
	))
	return &v1.MsgUpdateValidatorMetadataResponse{
		Validator: validatorRecordNativeToProto(validator),
	}, nil
}

func (m msgServer) RotateConsensusKey(ctx context.Context, msg *v1.MsgRotateConsensusKey) (*v1.MsgRotateConsensusKeyResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgRotateConsensusKey{
		Authority:             msg.Authority,
		OperatorAddress:       msg.OperatorAddress,
		NewConsensusPublicKey: msg.NewConsensusPublicKey,
		ActivationHeight:     msg.ActivationHeight,
		Height:                msg.Height,
	}
	validator, err := m.Keeper.RotateConsensusKey(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeRotateConsensusKey,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperatorAddress, validator.OperatorAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgRotateConsensusKeyResponse{
		Validator: validatorRecordNativeToProto(validator),
	}, nil
}

func (m msgServer) UpdateWithdrawalAddress(ctx context.Context, msg *v1.MsgUpdateWithdrawalAddress) (*v1.MsgUpdateWithdrawalAddressResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgUpdateWithdrawalAddress{
		Authority:         msg.Authority,
		OperatorAddress:   msg.OperatorAddress,
		WithdrawalAddress: msg.WithdrawalAddress,
		Height:            msg.Height,
	}
	validator, err := m.Keeper.UpdateWithdrawalAddress(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeUpdateWithdrawalAddress,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperatorAddress, validator.OperatorAddress),
	))
	return &v1.MsgUpdateWithdrawalAddressResponse{
		Validator: validatorRecordNativeToProto(validator),
	}, nil
}

func (m msgServer) UpdateTreasuryAddress(ctx context.Context, msg *v1.MsgUpdateTreasuryAddress) (*v1.MsgUpdateTreasuryAddressResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgUpdateTreasuryAddress{
		Authority:       msg.Authority,
		OperatorAddress: msg.OperatorAddress,
		TreasuryAddress: msg.TreasuryAddress,
		Height:          msg.Height,
	}
	validator, err := m.Keeper.UpdateTreasuryAddress(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeUpdateTreasuryAddress,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperatorAddress, validator.OperatorAddress),
	))
	return &v1.MsgUpdateTreasuryAddressResponse{
		Validator: validatorRecordNativeToProto(validator),
	}, nil
}

func (m msgServer) RetireValidator(ctx context.Context, msg *v1.MsgRetireValidator) (*v1.MsgRetireValidatorResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgRetireValidator{
		Authority:       msg.Authority,
		OperatorAddress: msg.OperatorAddress,
		Height:          msg.Height,
	}
	validator, err := m.Keeper.RetireValidator(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeRetireValidator,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperatorAddress, validator.OperatorAddress),
	))
	return &v1.MsgRetireValidatorResponse{
		Validator: validatorRecordNativeToProto(validator),
	}, nil
}

func (m msgServer) SetValidatorCapabilities(ctx context.Context, msg *v1.MsgSetValidatorCapabilities) (*v1.MsgSetValidatorCapabilitiesResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgSetValidatorCapabilities{
		Authority:       msg.Authority,
		OperatorAddress: msg.OperatorAddress,
		Capabilities:    msg.Capabilities,
		Height:          msg.Height,
	}
	validator, err := m.Keeper.SetValidatorCapabilities(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeSetValidatorCapabilities,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperatorAddress, validator.OperatorAddress),
	))
	return &v1.MsgSetValidatorCapabilitiesResponse{
		Validator: validatorRecordNativeToProto(validator),
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

func validatorRecordProtoToNative(p v1.ValidatorRecord) types.ValidatorRecord {
	return types.ValidatorRecord{
		OperatorAddress:              p.OperatorAddress,
		ConsensusPublicKey:           p.ConsensusPublicKey,
		PendingConsensusPublicKey:    p.PendingConsensusPublicKey,
		ConsensusKeyActivationHeight: p.ConsensusKeyActivationHeight,
		TreasuryAddress:              p.TreasuryAddress,
		WithdrawalAddress:            p.WithdrawalAddress,
		EmergencyAddress:             p.EmergencyAddress,
		Metadata:                     p.Metadata,
		CommissionPolicy:             commissionPolicyProtoToNative(p.CommissionPolicy),
		UptimeHistory:                uptimeSampleSliceProtoToNative(p.UptimeHistory),
		LatencyHistory:               latencySampleSliceProtoToNative(p.LatencyHistory),
		MissedBlockCounter:           p.MissedBlockCounter,
		SlashingHistory:              slashingEventSliceProtoToNative(p.SlashingHistory),
		ReputationScore:              p.ReputationScore,
		PerformanceScore:             p.PerformanceScore,
		Status:                       p.Status,
		Capabilities:                 p.Capabilities,
		SelfBond:                     p.SelfBond,
		NominatorBond:                p.NominatorBond,
		ExternalAuditFlags:           p.ExternalAuditFlags,
		History:                      validatorHistoryEventSliceProtoToNative(p.History),
	}
}

func validatorRecordNativeToProto(n types.ValidatorRecord) v1.ValidatorRecord {
	return v1.ValidatorRecord{
		OperatorAddress:              n.OperatorAddress,
		ConsensusPublicKey:           n.ConsensusPublicKey,
		PendingConsensusPublicKey:    n.PendingConsensusPublicKey,
		ConsensusKeyActivationHeight: n.ConsensusKeyActivationHeight,
		TreasuryAddress:              n.TreasuryAddress,
		WithdrawalAddress:            n.WithdrawalAddress,
		EmergencyAddress:             n.EmergencyAddress,
		Metadata:                     n.Metadata,
		CommissionPolicy:             commissionPolicyNativeToProto(n.CommissionPolicy),
		UptimeHistory:                uptimeSampleSliceNativeToProto(n.UptimeHistory),
		LatencyHistory:               latencySampleSliceNativeToProto(n.LatencyHistory),
		MissedBlockCounter:           n.MissedBlockCounter,
		SlashingHistory:              slashingEventSliceNativeToProto(n.SlashingHistory),
		ReputationScore:              n.ReputationScore,
		PerformanceScore:             n.PerformanceScore,
		Status:                       n.Status,
		Capabilities:                 n.Capabilities,
		SelfBond:                     n.SelfBond,
		NominatorBond:                n.NominatorBond,
		ExternalAuditFlags:           n.ExternalAuditFlags,
		History:                      validatorHistoryEventSliceNativeToProto(n.History),
	}
}

func commissionPolicyProtoToNative(p v1.CommissionPolicy) types.CommissionPolicy {
	return types.CommissionPolicy{
		CurrentRateBps:   p.CurrentRateBps,
		MaxRateBps:       p.MaxRateBps,
		MaxChangeRateBps: p.MaxChangeRateBps,
	}
}

func commissionPolicyNativeToProto(n types.CommissionPolicy) v1.CommissionPolicy {
	return v1.CommissionPolicy{
		CurrentRateBps:   n.CurrentRateBps,
		MaxRateBps:       n.MaxRateBps,
		MaxChangeRateBps: n.MaxChangeRateBps,
	}
}

func uptimeSampleProtoToNative(p v1.UptimeSample) types.UptimeSample {
	return types.UptimeSample{
		Height:    p.Height,
		UptimeBps: p.UptimeBps,
	}
}

func uptimeSampleNativeToProto(n types.UptimeSample) v1.UptimeSample {
	return v1.UptimeSample{
		Height:    n.Height,
		UptimeBps: n.UptimeBps,
	}
}

func uptimeSampleSliceProtoToNative(ps []v1.UptimeSample) []types.UptimeSample {
	out := make([]types.UptimeSample, len(ps))
	for i, p := range ps {
		out[i] = uptimeSampleProtoToNative(p)
	}
	return out
}

func uptimeSampleSliceNativeToProto(ns []types.UptimeSample) []v1.UptimeSample {
	out := make([]v1.UptimeSample, len(ns))
	for i, n := range ns {
		out[i] = uptimeSampleNativeToProto(n)
	}
	return out
}

func latencySampleProtoToNative(p v1.LatencySample) types.LatencySample {
	return types.LatencySample{
		Height:    p.Height,
		LatencyMs: p.LatencyMs,
	}
}

func latencySampleNativeToProto(n types.LatencySample) v1.LatencySample {
	return v1.LatencySample{
		Height:    n.Height,
		LatencyMs: n.LatencyMs,
	}
}

func latencySampleSliceProtoToNative(ps []v1.LatencySample) []types.LatencySample {
	out := make([]types.LatencySample, len(ps))
	for i, p := range ps {
		out[i] = latencySampleProtoToNative(p)
	}
	return out
}

func latencySampleSliceNativeToProto(ns []types.LatencySample) []v1.LatencySample {
	out := make([]v1.LatencySample, len(ns))
	for i, n := range ns {
		out[i] = latencySampleNativeToProto(n)
	}
	return out
}

func slashingEventProtoToNative(p v1.SlashingEvent) types.SlashingEvent {
	return types.SlashingEvent{
		Height:      p.Height,
		FractionBps: p.FractionBps,
		Reason:      p.Reason,
	}
}

func slashingEventNativeToProto(n types.SlashingEvent) v1.SlashingEvent {
	return v1.SlashingEvent{
		Height:      n.Height,
		FractionBps: n.FractionBps,
		Reason:      n.Reason,
	}
}

func slashingEventSliceProtoToNative(ps []v1.SlashingEvent) []types.SlashingEvent {
	out := make([]types.SlashingEvent, len(ps))
	for i, p := range ps {
		out[i] = slashingEventProtoToNative(p)
	}
	return out
}

func slashingEventSliceNativeToProto(ns []types.SlashingEvent) []v1.SlashingEvent {
	out := make([]v1.SlashingEvent, len(ns))
	for i, n := range ns {
		out[i] = slashingEventNativeToProto(n)
	}
	return out
}

func validatorHistoryEventProtoToNative(p v1.ValidatorHistoryEvent) types.ValidatorHistoryEvent {
	return types.ValidatorHistoryEvent{
		Height: p.Height,
		Type:   p.Type,
		Detail: p.Detail,
	}
}

func validatorHistoryEventNativeToProto(n types.ValidatorHistoryEvent) v1.ValidatorHistoryEvent {
	return v1.ValidatorHistoryEvent{
		Height: n.Height,
		Type:   n.Type,
		Detail: n.Detail,
	}
}

func validatorHistoryEventSliceProtoToNative(ps []v1.ValidatorHistoryEvent) []types.ValidatorHistoryEvent {
	out := make([]types.ValidatorHistoryEvent, len(ps))
	for i, p := range ps {
		out[i] = validatorHistoryEventProtoToNative(p)
	}
	return out
}

func validatorHistoryEventSliceNativeToProto(ns []types.ValidatorHistoryEvent) []v1.ValidatorHistoryEvent {
	out := make([]v1.ValidatorHistoryEvent, len(ns))
	for i, n := range ns {
		out[i] = validatorHistoryEventNativeToProto(n)
	}
	return out
}

func validatorRecordSliceNativeToProto(ns []types.ValidatorRecord) []v1.ValidatorRecord {
	out := make([]v1.ValidatorRecord, len(ns))
	for i, n := range ns {
		out[i] = validatorRecordNativeToProto(n)
	}
	return out
}