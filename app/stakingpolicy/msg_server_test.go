package stakingpolicy

import (
	"bytes"
	"context"
	"testing"

	"cosmossdk.io/log/v2"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
)

// liveBlockCtx returns an sdk.Context at the given block height. sdk.Context
// satisfies context.Context directly, so this is the same shape a real Msg
// handler receives. Height must be > 0 to be treated as post-genesis:
// CreateValidator treats BlockHeight() <= 0 as InitChain/gentx processing and
// exempts it from the self-bond floor (see msg_server.go).
func liveBlockCtx(height int64) context.Context {
	return sdk.NewContext(nil, cmtproto.Header{Height: height}, false, log.NewNopLogger())
}

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
	// live-join floor (StakingMinSelfBondNaet, 10,000 AET). At a live
	// (post-genesis) height this must be rejected.
	belowFloor := sdk.NewInt64Coin(appparams.BaseDenom, 100*1_000_000_000)

	_, err := server.CreateValidator(liveBlockCtx(100), &stakingtypes.MsgCreateValidator{
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

	_, err := server.CreateValidator(liveBlockCtx(100), &stakingtypes.MsgCreateValidator{
		ValidatorAddress: operator,
		Value:            atFloor,
	})
	require.NoError(t, err)
	require.NotNil(t, inner.createValidator, "a self-bond at the floor must reach the inner staking msg server")
}

// TestCreateValidatorExemptsGenesisGentxFromSelfBondFloor is a regression
// guard: x/genutil.DeliverGenTxs routes every gentx MsgCreateValidator
// through this SAME message server during InitChain. An unconditional floor
// here made every genesis panic ("failed to execute DeliverTx") on the first
// gentx, since the documented testnet bootstrap self-bond (100 AET,
// cmd/l1d/cmd/testnet.go) is below the live-join floor (10,000 AET) by
// design -- confirmed live on a fresh testnet init-files + start run.
func TestCreateValidatorExemptsGenesisGentxFromSelfBondFloor(t *testing.T) {
	inner := &recordingStakingMsgServer{}
	server := NewPoolOnlyMsgServer(inner)
	operator := aeFromBytesForPolicyTest(t, bytesOf(0x66))
	belowFloor := sdk.NewInt64Coin(appparams.BaseDenom, 100*1_000_000_000)

	_, err := server.CreateValidator(liveBlockCtx(0), &stakingtypes.MsgCreateValidator{
		ValidatorAddress: operator,
		Value:            belowFloor,
	})
	require.NoError(t, err, "InitChain-time (height <= 0) gentx processing must not be gated by the live-join self-bond floor")
	require.NotNil(t, inner.createValidator)
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
