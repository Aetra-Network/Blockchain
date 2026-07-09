package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	v1 "github.com/sovereign-l1/l1/api/l1/validatorelection/v1"
	"github.com/sovereign-l1/l1/x/validator-election/types"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
}

func NewMsgServerImpl(k *Keeper) v1.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) ApplyForValidatorSet(ctx context.Context, msg *v1.MsgApplyForValidatorSet) (*v1.MsgApplyForValidatorSetResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	// Refresh in-memory state from the committed store on the live block context
	// so restarted/state-synced nodes act on the same state as continuously
	// running ones. See SEC-HIGH: election EndBlocker off in-memory genesis.
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgApplyForValidatorSet{
		Authority:   msg.Authority,
		Application: candidateApplicationProtoToNative(msg.Application),
		Height:      msg.Height,
	}
	app, err := m.Keeper.ApplyForValidatorSet(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeApplyForValidatorSet,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperator, app.OperatorAddress),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", msg.Height)),
	))
	return &v1.MsgApplyForValidatorSetResponse{
		Application: candidateApplicationNativeToProto(app),
	}, nil
}

func (m msgServer) WithdrawApplication(ctx context.Context, msg *v1.MsgWithdrawApplication) (*v1.MsgWithdrawApplicationResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	// Refresh in-memory state from the committed store on the live block context
	// so restarted/state-synced nodes act on the same state as continuously
	// running ones. See SEC-HIGH: election EndBlocker off in-memory genesis.
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgWithdrawApplication{
		Authority:      msg.Authority,
		OperatorAddress: msg.OperatorAddress,
		Height:         msg.Height,
	}
	app, err := m.Keeper.WithdrawApplication(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeWithdrawApplication,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperator, app.OperatorAddress),
	))
	return &v1.MsgWithdrawApplicationResponse{
		Application: candidateApplicationNativeToProto(app),
	}, nil
}

func (m msgServer) CommitElection(ctx context.Context, msg *v1.MsgCommitElection) (*v1.MsgCommitElectionResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	// Refresh in-memory state from the committed store on the live block context
	// so restarted/state-synced nodes act on the same state as continuously
	// running ones. See SEC-HIGH: election EndBlocker off in-memory genesis.
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgCommitElection{
		Authority: msg.Authority,
		Height:    msg.Height,
	}
	result, err := m.Keeper.CommitElection(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeCommitElection,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyEpoch, fmt.Sprintf("%d", result.Epoch)),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", result.Height)),
	))
	return &v1.MsgCommitElectionResponse{
		Result: electionResultNativeToProto(result),
	}, nil
}

func (m msgServer) FinalizeElection(ctx context.Context, msg *v1.MsgFinalizeElection) (*v1.MsgFinalizeElectionResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	// Refresh in-memory state from the committed store on the live block context
	// so restarted/state-synced nodes act on the same state as continuously
	// running ones. See SEC-HIGH: election EndBlocker off in-memory genesis.
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgFinalizeElection{
		Authority: msg.Authority,
		Height:    msg.Height,
	}
	transition, err := m.Keeper.FinalizeElection(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeFinalizeElection,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyEpoch, fmt.Sprintf("%d", transition.Epoch)),
		sdk.NewAttribute(v1.AttributeKeyHeight, fmt.Sprintf("%d", transition.Height)),
	))
	return &v1.MsgFinalizeElectionResponse{
		Transition: validatorSetTransitionNativeToProto(transition),
	}, nil
}

func (m msgServer) RequestValidatorExit(ctx context.Context, msg *v1.MsgRequestValidatorExit) (*v1.MsgRequestValidatorExitResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	// Refresh in-memory state from the committed store on the live block context
	// so restarted/state-synced nodes act on the same state as continuously
	// running ones. See SEC-HIGH: election EndBlocker off in-memory genesis.
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgRequestValidatorExit{
		Authority:      msg.Authority,
		OperatorAddress: msg.OperatorAddress,
		Height:         msg.Height,
	}
	exit, err := m.Keeper.RequestValidatorExit(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeRequestValidatorExit,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperator, exit.OperatorAddress),
	))
	return &v1.MsgRequestValidatorExitResponse{
		Exit: pendingExitNativeToProto(exit),
	}, nil
}

func (m msgServer) CancelValidatorExit(ctx context.Context, msg *v1.MsgCancelValidatorExit) (*v1.MsgCancelValidatorExitResponse, error) {
	if msg == nil {
		return nil, v1.ErrInvalidParams.Wrap("empty request")
	}
	// Refresh in-memory state from the committed store on the live block context
	// so restarted/state-synced nodes act on the same state as continuously
	// running ones. See SEC-HIGH: election EndBlocker off in-memory genesis.
	if err := m.Keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.requireAuthority(msg.Authority); err != nil {
		return nil, err
	}
	nativeMsg := types.MsgCancelValidatorExit{
		Authority:      msg.Authority,
		OperatorAddress: msg.OperatorAddress,
		Height:         msg.Height,
	}
	exit, err := m.Keeper.CancelValidatorExit(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeCancelValidatorExit,
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
		sdk.NewAttribute(v1.AttributeKeyOperator, exit.OperatorAddress),
	))
	return &v1.MsgCancelValidatorExitResponse{
		Exit: pendingExitNativeToProto(exit),
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

func candidateApplicationProtoToNative(p v1.CandidateApplication) types.CandidateApplication {
	return types.CandidateApplication{
		OperatorAddress:   p.OperatorAddress,
		ConsensusPublicKey: p.ConsensusPublicKey,
		RequestedPower:    p.RequestedPower,
		SelfBond:          p.SelfBond,
		ValidatorStatus:   p.ValidatorStatus,
		Status:            p.Status,
		AppliedHeight:     p.AppliedHeight,
		UpdatedHeight:     p.UpdatedHeight,
	}
}

func candidateApplicationNativeToProto(n types.CandidateApplication) v1.CandidateApplication {
	return v1.CandidateApplication{
		OperatorAddress:   n.OperatorAddress,
		ConsensusPublicKey: n.ConsensusPublicKey,
		RequestedPower:    n.RequestedPower,
		SelfBond:          n.SelfBond,
		ValidatorStatus:   n.ValidatorStatus,
		Status:            n.Status,
		AppliedHeight:     n.AppliedHeight,
		UpdatedHeight:     n.UpdatedHeight,
	}
}

func validatorPowerNativeToProto(n types.ValidatorPower) v1.ValidatorPower {
	return v1.ValidatorPower{
		OperatorAddress:   n.OperatorAddress,
		ConsensusPublicKey: n.ConsensusPublicKey,
		VotingPower:       n.VotingPower,
		ValidatorStatus:   n.ValidatorStatus,
	}
}

func validatorPowerSliceNativeToProto(ns []types.ValidatorPower) []v1.ValidatorPower {
	out := make([]v1.ValidatorPower, len(ns))
	for i, n := range ns {
		out[i] = validatorPowerNativeToProto(n)
	}
	return out
}

func electionResultNativeToProto(n types.ElectionResult) v1.ElectionResult {
	return v1.ElectionResult{
		Epoch:     n.Epoch,
		Height:    n.Height,
		NextSet:   validatorPowerSliceNativeToProto(n.NextSet),
		Committed: n.Committed,
		Finalized: n.Finalized,
	}
}

func validatorSetTransitionNativeToProto(n types.ValidatorSetTransition) v1.ValidatorSetTransition {
	return v1.ValidatorSetTransition{
		Epoch:        n.Epoch,
		Height:       n.Height,
		PreviousSet:  validatorPowerSliceNativeToProto(n.PreviousSet),
		CurrentSet:   validatorPowerSliceNativeToProto(n.CurrentSet),
		NextSet:      validatorPowerSliceNativeToProto(n.NextSet),
	}
}

func pendingExitNativeToProto(n types.PendingExit) v1.PendingExit {
	return v1.PendingExit{
		OperatorAddress: n.OperatorAddress,
		RequestedHeight: n.RequestedHeight,
		Status:          n.Status,
	}
}

func frozenStakeNativeToProto(n types.FrozenStake) v1.FrozenStake {
	return v1.FrozenStake{
		OperatorAddress: n.OperatorAddress,
		Amount:          n.Amount,
		FrozenAtHeight:  n.FrozenAtHeight,
		UnlockHeight:    n.UnlockHeight,
		Released:        n.Released,
	}
}

func stateNativeToProto(n types.State) v1.State {
	return v1.State{
		PreviousValidatorSet:        validatorPowerSliceNativeToProto(n.PreviousValidatorSet),
		CurrentValidatorSet:         validatorPowerSliceNativeToProto(n.CurrentValidatorSet),
		NextValidatorSet:            validatorPowerSliceNativeToProto(n.NextValidatorSet),
		ElectionEpoch:               n.ElectionEpoch,
		ElectionWindow:              electionWindowNativeToProto(n.ElectionWindow),
		CandidateApplications:       candidateApplicationSliceNativeToProto(n.CandidateApplications),
		FrozenStakes:                frozenStakeSliceNativeToProto(n.FrozenStakes),
		PendingExits:                pendingExitSliceNativeToProto(n.PendingExits),
		ValidatorPowerCaps:          validatorPowerCapSliceNativeToProto(n.ValidatorPowerCaps),
		ElectionResults:             electionResultSliceNativeToProto(n.ElectionResults),
		RewardDistributionSnapshots: rewardDistributionSnapshotSliceNativeToProto(n.RewardDistributionSnapshots),
		TransitionHistory:           validatorSetTransitionSliceNativeToProto(n.TransitionHistory),
	}
}

func electionWindowNativeToProto(n types.ElectionWindow) v1.ElectionWindow {
	return v1.ElectionWindow{
		StartHeight:             n.StartHeight,
		EndHeight:               n.EndHeight,
		WithdrawDeadlineHeight:  n.WithdrawDeadlineHeight,
	}
}

func candidateApplicationSliceNativeToProto(ns []types.CandidateApplication) []v1.CandidateApplication {
	out := make([]v1.CandidateApplication, len(ns))
	for i, n := range ns {
		out[i] = candidateApplicationNativeToProto(n)
	}
	return out
}

func frozenStakeSliceNativeToProto(ns []types.FrozenStake) []v1.FrozenStake {
	out := make([]v1.FrozenStake, len(ns))
	for i, n := range ns {
		out[i] = frozenStakeNativeToProto(n)
	}
	return out
}

func pendingExitSliceNativeToProto(ns []types.PendingExit) []v1.PendingExit {
	out := make([]v1.PendingExit, len(ns))
	for i, n := range ns {
		out[i] = pendingExitNativeToProto(n)
	}
	return out
}

func validatorPowerCapSliceNativeToProto(ns []types.ValidatorPowerCap) []v1.ValidatorPowerCap {
	out := make([]v1.ValidatorPowerCap, len(ns))
	for i, n := range ns {
		out[i] = v1.ValidatorPowerCap{
			OperatorAddress: n.OperatorAddress,
			MaxVotingPower:  n.MaxVotingPower,
		}
	}
	return out
}

func electionResultSliceNativeToProto(ns []types.ElectionResult) []v1.ElectionResult {
	out := make([]v1.ElectionResult, len(ns))
	for i, n := range ns {
		out[i] = electionResultNativeToProto(n)
	}
	return out
}

func rewardDistributionSnapshotSliceNativeToProto(ns []types.RewardDistributionSnapshot) []v1.RewardDistributionSnapshot {
	out := make([]v1.RewardDistributionSnapshot, len(ns))
	for i, n := range ns {
		out[i] = v1.RewardDistributionSnapshot{
			Epoch:            n.Epoch,
			Height:           n.Height,
			ValidatorPowers:  validatorPowerSliceNativeToProto(n.ValidatorPowers),
			TotalVotingPower: n.TotalVotingPower,
		}
	}
	return out
}

func validatorSetTransitionSliceNativeToProto(ns []types.ValidatorSetTransition) []v1.ValidatorSetTransition {
	out := make([]v1.ValidatorSetTransition, len(ns))
	for i, n := range ns {
		out[i] = validatorSetTransitionNativeToProto(n)
	}
	return out
}
