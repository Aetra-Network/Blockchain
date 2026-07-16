package stakingpolicy

import (
	"bytes"
	"context"
	"errors"

	sdkmath "cosmossdk.io/math"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
)

// minSelfBondNaet is the network's minimum validator self-bond, computed once
// from the app/params constant.
var minSelfBondNaet = sdkmath.NewInt(appparams.StakingMinSelfBondNaet)

const DirectUserDelegationDisabledMessage = "direct user delegation to validators is disabled; use official liquid staking pool deposit"

const (
	nominatorPoolModule		= "nominator-pool"
	singleNominatorPoolModule	= "single-nominator-pool"
)

type DirectDelegationPolicy struct {
	DirectUserValidatorDelegation string
}

type PoolOnlyMsgServer struct {
	inner stakingtypes.MsgServer
}

func NewPoolOnlyMsgServer(inner stakingtypes.MsgServer) PoolOnlyMsgServer {
	return PoolOnlyMsgServer{inner: inner}
}

func DirectUserDelegationDisabledError() error {
	return errors.New(DirectUserDelegationDisabledMessage)
}

func DefaultDirectDelegationPolicy() DirectDelegationPolicy {
	return DirectDelegationPolicy{
		DirectUserValidatorDelegation: appparams.DirectUserDelegationDisabled,
	}
}

func ValidateDelegate(policy DirectDelegationPolicy, msg *stakingtypes.MsgDelegate) error {
	if msg == nil {
		return DirectUserDelegationDisabledError()
	}
	if !directUserDelegationDisabled(policy) {
		return nil
	}
	if IsValidatorSelfBond(msg.DelegatorAddress, msg.ValidatorAddress) {
		return nil
	}
	if IsNominatorPoolControlledDelegator(msg.DelegatorAddress) {
		return nil
	}
	return DirectUserDelegationDisabledError()
}

func ValidateBeginRedelegate(policy DirectDelegationPolicy, msg *stakingtypes.MsgBeginRedelegate) error {
	if msg == nil {
		return DirectUserDelegationDisabledError()
	}
	if !directUserDelegationDisabled(policy) {
		return nil
	}
	// Same exemptions as ValidateDelegate: whoever is allowed to bond must be
	// allowed to move that bond. The source validator is what the delegation
	// currently sits under, so that is what a self-bond is measured against.
	if IsValidatorSelfBond(msg.DelegatorAddress, msg.ValidatorSrcAddress) {
		return nil
	}
	if IsNominatorPoolControlledDelegator(msg.DelegatorAddress) {
		return nil
	}
	return DirectUserDelegationDisabledError()
}

func ValidateUndelegate(policy DirectDelegationPolicy, msg *stakingtypes.MsgUndelegate) error {
	if msg == nil {
		return DirectUserDelegationDisabledError()
	}
	if !directUserDelegationDisabled(policy) {
		return nil
	}
	// Mirror ValidateDelegate. Without these, the pool-only policy is not a
	// delegation restriction but a one-way trap: a validator can create its
	// self-bond and then never withdraw it, and the pool can never unwind a
	// delegation it is allowed to make. Every bonded token on the chain would
	// be permanently locked.
	if IsValidatorSelfBond(msg.DelegatorAddress, msg.ValidatorAddress) {
		return nil
	}
	if IsNominatorPoolControlledDelegator(msg.DelegatorAddress) {
		return nil
	}
	return DirectUserDelegationDisabledError()
}

func IsValidatorSelfBond(delegatorAddress, validatorAddress string) bool {
	delegator, err := addressing.Parse(delegatorAddress)
	if err != nil {
		return false
	}
	validator, err := addressing.Parse(validatorAddress)
	if err != nil {
		return false
	}
	delegatorRaw, err := addressing.ToRawPayload(delegator)
	if err != nil {
		return false
	}
	validatorRaw, err := addressing.ToRawPayload(validator)
	if err != nil {
		return false
	}
	return bytes.Equal(delegatorRaw, validatorRaw)
}

func IsNominatorPoolControlledDelegator(delegatorAddress string) bool {
	systemAddress, found := addressing.SystemAddressByText(delegatorAddress)
	if !found {
		return false
	}
	return systemAddress.ModuleName == nominatorPoolModule || systemAddress.ModuleName == singleNominatorPoolModule
}

func directUserDelegationDisabled(policy DirectDelegationPolicy) bool {
	return policy.DirectUserValidatorDelegation == "" ||
		policy.DirectUserValidatorDelegation == appparams.DirectUserDelegationDisabled
}

// ErrSelfBondBelowFloor is returned when a MsgCreateValidator's self-delegation
// is below the network's minimum self-bond. Genesis validators (created via
// gentx/InitGenesis) bypass this message server entirely and are unaffected;
// this only gates new validators joining after genesis via a live tx.
var ErrSelfBondBelowFloor = errors.New("validator self-delegation is below the minimum required self-bond")

func (s PoolOnlyMsgServer) CreateValidator(ctx context.Context, msg *stakingtypes.MsgCreateValidator) (*stakingtypes.MsgCreateValidatorResponse, error) {
	// Live-verified decentralization defect: this was previously an unchecked
	// passthrough, so a validator could join with a self-bond as low as 1
	// naet (10^-9 AET) -- StakingMinSelfBondNaet (app/params/staking_policy.go)
	// was defined but never consulted anywhere on the live create-validator
	// path. A minimum self-bond makes a per-address voting-power cap (if one
	// is later enforced) mean something: without a real entry cost, a single
	// actor can always defeat a cap by splitting into arbitrarily many
	// validators for near-zero additional capital.
	if msg != nil && msg.Value.Denom == appparams.BaseDenom && msg.Value.Amount.LT(minSelfBondNaet) {
		return nil, ErrSelfBondBelowFloor
	}
	return s.inner.CreateValidator(ctx, msg)
}

func (s PoolOnlyMsgServer) EditValidator(ctx context.Context, msg *stakingtypes.MsgEditValidator) (*stakingtypes.MsgEditValidatorResponse, error) {
	return s.inner.EditValidator(ctx, msg)
}

func (s PoolOnlyMsgServer) Delegate(ctx context.Context, msg *stakingtypes.MsgDelegate) (*stakingtypes.MsgDelegateResponse, error) {
	if err := ValidateDelegate(DefaultDirectDelegationPolicy(), msg); err != nil {
		return nil, err
	}
	return s.inner.Delegate(ctx, msg)
}

func (s PoolOnlyMsgServer) BeginRedelegate(ctx context.Context, msg *stakingtypes.MsgBeginRedelegate) (*stakingtypes.MsgBeginRedelegateResponse, error) {
	if err := ValidateBeginRedelegate(DefaultDirectDelegationPolicy(), msg); err != nil {
		return nil, err
	}
	return s.inner.BeginRedelegate(ctx, msg)
}

func (s PoolOnlyMsgServer) Undelegate(ctx context.Context, msg *stakingtypes.MsgUndelegate) (*stakingtypes.MsgUndelegateResponse, error) {
	if err := ValidateUndelegate(DefaultDirectDelegationPolicy(), msg); err != nil {
		return nil, err
	}
	return s.inner.Undelegate(ctx, msg)
}

func (s PoolOnlyMsgServer) CancelUnbondingDelegation(ctx context.Context, msg *stakingtypes.MsgCancelUnbondingDelegation) (*stakingtypes.MsgCancelUnbondingDelegationResponse, error) {
	return s.inner.CancelUnbondingDelegation(ctx, msg)
}

func (s PoolOnlyMsgServer) UpdateParams(ctx context.Context, msg *stakingtypes.MsgUpdateParams) (*stakingtypes.MsgUpdateParamsResponse, error) {
	return s.inner.UpdateParams(ctx, msg)
}
