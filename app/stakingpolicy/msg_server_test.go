package stakingpolicy

import (
	"bytes"
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
)

func TestPoolOnlyMsgServerRejectsDirectUserValidatorMessages(t *testing.T) {
	server := NewPoolOnlyMsgServer(nil)
	amount := sdk.NewInt64Coin("naet", 10)

	_, err := server.Delegate(context.Background(), stakingtypes.NewMsgDelegate("AE1", "AE2", amount))
	require.ErrorContains(t, err, DirectUserDelegationDisabledMessage)

	_, err = server.BeginRedelegate(context.Background(), stakingtypes.NewMsgBeginRedelegate("AE1", "AE2", "AE3", amount))
	require.ErrorContains(t, err, DirectUserDelegationDisabledMessage)

	_, err = server.Undelegate(context.Background(), stakingtypes.NewMsgUndelegate("AE1", "AE2", amount))
	require.ErrorContains(t, err, DirectUserDelegationDisabledMessage)
}

func TestPoolOnlyMsgServerAllowsValidatorSelfBondWhenDirectUserDelegationDisabled(t *testing.T) {
	inner := &recordingStakingMsgServer{}
	server := NewPoolOnlyMsgServer(inner)
	operator := aeFromBytesForPolicyTest(t, bytesOf(0x11))
	amount := sdk.NewInt64Coin("naet", 10)

	_, err := server.Delegate(context.Background(), stakingtypes.NewMsgDelegate(operator, operator, amount))
	require.NoError(t, err)
	require.Equal(t, operator, inner.delegate.DelegatorAddress)
	require.Equal(t, appparams.DirectUserDelegationDisabled, DefaultDirectDelegationPolicy().DirectUserValidatorDelegation)
}

func TestValidateDelegateRejectsOrdinaryUserWhenGovernanceParamDisabled(t *testing.T) {
	user := aeFromBytesForPolicyTest(t, bytesOf(0x22))
	validator := aeFromBytesForPolicyTest(t, bytesOf(0x33))
	msg := stakingtypes.NewMsgDelegate(user, validator, sdk.NewInt64Coin("naet", 10))

	err := ValidateDelegate(DefaultDirectDelegationPolicy(), msg)
	require.ErrorContains(t, err, DirectUserDelegationDisabledMessage)
	require.Equal(t, appparams.DirectUserDelegationDisabled, DefaultDirectDelegationPolicy().DirectUserValidatorDelegation)

	err = ValidateDelegate(DirectDelegationPolicy{}, msg)
	require.ErrorContains(t, err, DirectUserDelegationDisabledMessage)
}

func TestCreateValidatorRejectsSelfBondBelowFloor(t *testing.T) {
	inner := &recordingStakingMsgServer{}
	server := NewPoolOnlyMsgServer(inner)
	operator := aeFromBytesForPolicyTest(t, bytesOf(0x44))
	// The testnet bootstrap gentx default (100 AET) is intentionally below the
	// live-join floor (StakingMinSelfBondNaet, 10,000 AET): genesis validators
	// are created via gentx/InitGenesis, which never goes through this message
	// server, so this check cannot break testnet bootstrap.
	belowFloor := sdk.NewInt64Coin(appparams.BaseDenom, 100*1_000_000_000)

	_, err := server.CreateValidator(context.Background(), &stakingtypes.MsgCreateValidator{
		ValidatorAddress: operator,
		Value:            belowFloor,
	})
	require.ErrorIs(t, err, ErrSelfBondBelowFloor)
	require.Nil(t, inner.createValidator, "the inner staking msg server must not be reached on a rejected self-bond")
}

func TestCreateValidatorAllowsSelfBondAtOrAboveFloor(t *testing.T) {
	inner := &recordingStakingMsgServer{}
	server := NewPoolOnlyMsgServer(inner)
	operator := aeFromBytesForPolicyTest(t, bytesOf(0x55))
	atFloor := sdk.NewCoin(appparams.BaseDenom, minSelfBondNaet)

	_, err := server.CreateValidator(context.Background(), &stakingtypes.MsgCreateValidator{
		ValidatorAddress: operator,
		Value:            atFloor,
	})
	require.NoError(t, err)
	require.NotNil(t, inner.createValidator, "a self-bond at the floor must reach the inner staking msg server")
}

type recordingStakingMsgServer struct {
	stakingtypes.UnimplementedMsgServer
	delegate		*stakingtypes.MsgDelegate
	createValidator		*stakingtypes.MsgCreateValidator
}

func (s *recordingStakingMsgServer) Delegate(_ context.Context, msg *stakingtypes.MsgDelegate) (*stakingtypes.MsgDelegateResponse, error) {
	s.delegate = msg
	return &stakingtypes.MsgDelegateResponse{}, nil
}

func (s *recordingStakingMsgServer) CreateValidator(_ context.Context, msg *stakingtypes.MsgCreateValidator) (*stakingtypes.MsgCreateValidatorResponse, error) {
	s.createValidator = msg
	return &stakingtypes.MsgCreateValidatorResponse{}, nil
}

func bytesOf(value byte) []byte {
	return bytes.Repeat([]byte{value}, 20)
}

func aeFromBytesForPolicyTest(t *testing.T, bz []byte) string {
	t.Helper()
	text, err := addressing.FormatUserFriendly(bz)
	require.NoError(t, err)
	return text
}
