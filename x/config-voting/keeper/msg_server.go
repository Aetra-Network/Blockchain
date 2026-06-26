package keeper

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	v1 "github.com/sovereign-l1/l1/api/l1/configvoting/v1"
	"github.com/sovereign-l1/l1/x/config-voting/types"
	configkeeper "github.com/sovereign-l1/l1/x/config/keeper"
)

var _ v1.MsgServer = msgServer{}

type msgServer struct {
	*Keeper
	configKeeper *configkeeper.Keeper
}

func NewMsgServerImpl(k *Keeper, configKeeper *configkeeper.Keeper) v1.MsgServer {
	return msgServer{Keeper: k, configKeeper: configKeeper}
}

func (m msgServer) SubmitConfigProposal(ctx context.Context, msg *v1.MsgSubmitConfigProposal) (*v1.MsgSubmitConfigProposalResponse, error) {
	if msg == nil {
		return nil, errEmptyRequest("SubmitConfigProposal")
	}
	nativeMsg := types.MsgSubmitConfigProposal{
		Authority: msg.Authority,
		Proposal:  configProposalProtoToNative(msg.Proposal),
	}
	proposal, err := m.Keeper.SubmitConfigProposal(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeConfigProposalSubmitted,
		sdk.NewAttribute(v1.AttributeKeyProposalID, proposal.ProposalID),
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
	))
	return &v1.MsgSubmitConfigProposalResponse{
		Proposal: configProposalNativeToProto(proposal),
	}, nil
}

func (m msgServer) VoteConfigProposal(ctx context.Context, msg *v1.MsgVoteConfigProposal) (*v1.MsgVoteConfigProposalResponse, error) {
	if msg == nil {
		return nil, errEmptyRequest("VoteConfigProposal")
	}
	nativeMsg := types.MsgVoteConfigProposal{
		Voter:      msg.Voter,
		ProposalID: msg.ProposalId,
		Option:     msg.Option,
		Height:     msg.Height,
	}
	vote, err := m.Keeper.VoteConfigProposal(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeConfigProposalVoted,
		sdk.NewAttribute(v1.AttributeKeyProposalID, vote.ProposalID),
		sdk.NewAttribute(v1.AttributeKeyVoter, msg.Voter),
		sdk.NewAttribute(v1.AttributeKeyOption, msg.Option),
	))
	return &v1.MsgVoteConfigProposalResponse{
		Vote: configVoteNativeToProto(vote),
	}, nil
}

func (m msgServer) ExecuteConfigProposal(ctx context.Context, msg *v1.MsgExecuteConfigProposal) (*v1.MsgExecuteConfigProposalResponse, error) {
	if msg == nil {
		return nil, errEmptyRequest("ExecuteConfigProposal")
	}
	if m.configKeeper == nil {
		return nil, errors.New("config keeper is not configured")
	}
	configGenesis, err := m.configKeeper.ExportGenesisState(ctx)
	if err != nil {
		return nil, err
	}
	nativeMsg := types.MsgExecuteConfigProposal{
		Authority:    msg.Authority,
		ProposalID:   msg.ProposalId,
		Height:       msg.Height,
		ConfigState:  configGenesis.State,
		ConfigParams: configGenesis.Params,
	}
	proposal, err := m.Keeper.ExecuteConfigProposal(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeConfigProposalExecuted,
		sdk.NewAttribute(v1.AttributeKeyProposalID, proposal.ProposalID),
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
	))
	return &v1.MsgExecuteConfigProposalResponse{
		Proposal: configProposalNativeToProto(proposal),
	}, nil
}

func (m msgServer) VetoConfigProposal(ctx context.Context, msg *v1.MsgVetoConfigProposal) (*v1.MsgVetoConfigProposalResponse, error) {
	if msg == nil {
		return nil, errEmptyRequest("VetoConfigProposal")
	}
	nativeMsg := types.MsgVetoConfigProposal{
		Authority:  msg.Authority,
		ProposalID: msg.ProposalId,
		Reason:     msg.Reason,
		Height:     msg.Height,
	}
	proposal, err := m.Keeper.VetoConfigProposal(nativeMsg)
	if err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		v1.EventTypeConfigProposalVetoed,
		sdk.NewAttribute(v1.AttributeKeyProposalID, proposal.ProposalID),
		sdk.NewAttribute(v1.AttributeKeyAuthority, msg.Authority),
	))
	return &v1.MsgVetoConfigProposalResponse{
		Proposal: configProposalNativeToProto(proposal),
	}, nil
}

func configProposalProtoToNative(p v1.ConfigProposal) types.ConfigProposal {
	return types.ConfigProposal{
		ProposalID:                       p.ProposalId,
		Title:                            p.Title,
		ConfigKey:                        p.ConfigKey,
		ConfigValue:                      p.ConfigValue,
		Operation:                        p.Operation,
		SubmittedBy:                      p.SubmittedBy,
		Status:                           p.Status,
		Metadata:                         p.Metadata,
		ConstitutionReference:            p.ConstitutionReference,
		Emergency:                        p.Emergency,
		RequiresConstitutionalException:  p.RequiresConstitutionalException,
		SnapshotHeight:                   p.SnapshotHeight,
		SubmitHeight:                     p.SubmitHeight,
		VotingEndHeight:                  p.VotingEndHeight,
		EarliestExecutionHeight:          p.EarliestExecutionHeight,
		ExecutedHeight:                   p.ExecutedHeight,
		TotalVotingPower:                 p.TotalVotingPower,
		VotingPowerSnapshot:              votingPowerSnapshotSliceProtoToNative(p.VotingPowerSnapshot),
		ExpectedPreviousVersion:          p.ExpectedPreviousVersion,
		AllowMissingExpectedPrevious:     p.AllowMissingExpectedPrevious,
		ExecutionConstitutionValidatedAt: p.ExecutionConstitutionValidatedAt,
	}
}

func configProposalNativeToProto(n types.ConfigProposal) v1.ConfigProposal {
	return v1.ConfigProposal{
		ProposalId:                       n.ProposalID,
		Title:                            n.Title,
		ConfigKey:                        n.ConfigKey,
		ConfigValue:                      n.ConfigValue,
		Operation:                        n.Operation,
		SubmittedBy:                      n.SubmittedBy,
		Status:                           n.Status,
		Metadata:                         n.Metadata,
		ConstitutionReference:            n.ConstitutionReference,
		Emergency:                        n.Emergency,
		RequiresConstitutionalException:  n.RequiresConstitutionalException,
		SnapshotHeight:                   n.SnapshotHeight,
		SubmitHeight:                     n.SubmitHeight,
		VotingEndHeight:                  n.VotingEndHeight,
		EarliestExecutionHeight:          n.EarliestExecutionHeight,
		ExecutedHeight:                   n.ExecutedHeight,
		TotalVotingPower:                 n.TotalVotingPower,
		VotingPowerSnapshot:              votingPowerSnapshotSliceNativeToProto(n.VotingPowerSnapshot),
		ExpectedPreviousVersion:          n.ExpectedPreviousVersion,
		AllowMissingExpectedPrevious:     n.AllowMissingExpectedPrevious,
		ExecutionConstitutionValidatedAt: n.ExecutionConstitutionValidatedAt,
	}
}

func votingPowerSnapshotProtoToNative(p v1.VotingPowerSnapshotEntry) types.VotingPowerSnapshotEntry {
	return types.VotingPowerSnapshotEntry{
		Voter: p.Voter,
		Power: p.Power,
	}
}

func votingPowerSnapshotNativeToProto(n types.VotingPowerSnapshotEntry) v1.VotingPowerSnapshotEntry {
	return v1.VotingPowerSnapshotEntry{
		Voter: n.Voter,
		Power: n.Power,
	}
}

func votingPowerSnapshotSliceProtoToNative(ps []v1.VotingPowerSnapshotEntry) []types.VotingPowerSnapshotEntry {
	out := make([]types.VotingPowerSnapshotEntry, len(ps))
	for i, p := range ps {
		out[i] = votingPowerSnapshotProtoToNative(p)
	}
	return out
}

func votingPowerSnapshotSliceNativeToProto(ns []types.VotingPowerSnapshotEntry) []v1.VotingPowerSnapshotEntry {
	out := make([]v1.VotingPowerSnapshotEntry, len(ns))
	for i, n := range ns {
		out[i] = votingPowerSnapshotNativeToProto(n)
	}
	return out
}

func configVoteNativeToProto(n types.ConfigVote) v1.ConfigVote {
	return v1.ConfigVote{
		ProposalId: n.ProposalID,
		Voter:      n.Voter,
		Option:     n.Option,
		Power:      n.Power,
		Height:     n.Height,
	}
}

func configVotingParamsNativeToProto(n types.ConfigVotingParams) v1.ConfigVotingParams {
	return v1.ConfigVotingParams{
		MaxProposals:         n.MaxProposals,
		MaxVotes:             n.MaxVotes,
		MaxSnapshotEntries:   n.MaxSnapshotEntries,
		QuorumBps:            n.QuorumBps,
		ThresholdBps:         n.ThresholdBps,
		VetoThresholdBps:     n.VetoThresholdBps,
		VotingPeriod:         n.VotingPeriod,
		ExecutionDelay:       n.ExecutionDelay,
		EmergencyDelay:       n.EmergencyDelay,
		MaxMetadataBytes:     n.MaxMetadataBytes,
		MaxConstitutionBytes: n.MaxConstitutionBytes,
		BpsScale:             n.BpsScale,
		VetoAuthorities:      n.VetoAuthorities,
	}
}

func errEmptyRequest(method string) error {
	return v1.ErrEmptyRequest.Wrapf("empty request for %s", method)
}
