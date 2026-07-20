package keeper

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/contracts/types"
)

var (
	_ types.GRPCMsgServer   = grpcMsgServer{}
	_ types.GRPCQueryServer = grpcQueryServer{}
)

// blockHeight returns the consensus block height for the current context.
// ScheduleContractUpgrade/ApplyScheduledUpgrade use the caller-supplied
// msg.Height to compute and enforce MinUpgradeDelay -- if that value were
// trusted as-is, any caller could self-report a far-future Height and apply
// a scheduled upgrade in the very next transaction, defeating the timelock
// entirely. So (mirroring x/identity-root/keeper/grpc_server.go's blockHeight)
// every handler that feeds a security-critical delay check overwrites
// msg.Height with this value before the keeper runs. The overwrite is
// UNCONDITIONAL, not a zero-fill, so a non-zero attacker-chosen height can
// never survive. Floored at 1 so the keeper's Height==0 guard is never
// tripped spuriously (e.g. a genesis/height-0 context).
func blockHeight(ctx context.Context) uint64 {
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if height <= 0 {
		return 1
	}
	return uint64(height)
}

type grpcMsgServer struct {
	types.UnimplementedGRPCMsgServer
	keeper *Keeper
}

type grpcQueryServer struct {
	types.UnimplementedGRPCQueryServer
	keeper *Keeper
}

func NewGRPCMsgServer(k *Keeper) types.GRPCMsgServer {
	return grpcMsgServer{keeper: k}
}

func NewGRPCQueryServer(k *Keeper) types.GRPCQueryServer {
	return grpcQueryServer{keeper: k}
}

func (m grpcMsgServer) StoreCode(ctx context.Context, msg *types.MsgStoreCode) (*types.StoreCodeResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts store code request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	res, err := m.keeper.StoreCodeState(ctx, *msg)
	return &res, err
}

func (m grpcMsgServer) DeployContract(ctx context.Context, msg *types.MsgDeployContract) (*types.InstantiateContractResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts deploy request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	res, err := m.keeper.DeployContractState(ctx, *msg)
	return &res, err
}

func (m grpcMsgServer) ExecuteExternal(ctx context.Context, msg *types.MsgExecuteExternal) (*types.ExecuteContractResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts external execution request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	res, err := m.keeper.ExecuteExternalState(ctx, *msg)
	return &res, err
}

func (m grpcMsgServer) ExecuteInternal(ctx context.Context, msg *types.MsgExecuteInternal) (*types.InternalMessage, error) {
	if msg == nil {
		return nil, errors.New("empty contracts internal execution request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	res, err := m.keeper.ExecuteInternal(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &res, nil
}

func (m grpcMsgServer) SendInternalMessage(ctx context.Context, msg *types.MsgSendInternalMessage) (*types.InternalMessage, error) {
	if msg == nil {
		return nil, errors.New("empty contracts internal send request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	res, err := m.keeper.SendInternalMessage(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &res, nil
}

func (m grpcMsgServer) UpdateContractParams(ctx context.Context, msg *types.MsgUpdateContractParams) (*types.MsgUpdateContractParamsResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts params update request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.keeper.UpdateContractParams(*msg); err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &types.MsgUpdateContractParamsResponse{StateRoot: m.keeper.ExportGenesis().StateRoot}, nil
}

func (m grpcMsgServer) SubmitSecurityAttestation(ctx context.Context, msg *types.MsgSubmitSecurityAttestation) (*types.MsgSubmitSecurityAttestationResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts security attestation submit request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	res, err := m.keeper.SubmitSecurityAttestation(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &res, nil
}

func (m grpcMsgServer) RevokeSecurityAttestation(ctx context.Context, msg *types.MsgRevokeSecurityAttestation) (*types.MsgRevokeSecurityAttestationResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts security attestation revoke request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	res, err := m.keeper.RevokeSecurityAttestation(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &res, nil
}

func (m grpcMsgServer) TopUpContract(ctx context.Context, msg *types.MsgTopUpContract) (*types.MsgTopUpContractResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts top-up request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	contract, err := m.keeper.TopUpContractState(ctx, *msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgTopUpContractResponse{Contract: contract}, nil
}

func (m grpcMsgServer) PayContractStorageDebt(ctx context.Context, msg *types.MsgPayContractStorageDebt) (*types.MsgPayContractStorageDebtResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts storage debt payment request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	contract, err := m.keeper.PayContractStorageDebtState(ctx, *msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgPayContractStorageDebtResponse{Contract: contract}, nil
}

func (m grpcMsgServer) UnfreezeContract(ctx context.Context, msg *types.MsgUnfreezeContract) (*types.MsgUnfreezeContractResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts unfreeze request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	contract, err := m.keeper.UnfreezeContractState(ctx, *msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgUnfreezeContractResponse{Contract: contract}, nil
}

// UpgradeContractCode, MigrateContractState, SetContractAdmin, and
// DisableContractUpgrades below compose the keeper's existing (already
// authorized, already unit-tested) business-logic methods with writeGenesis
// inline, the same way ExecuteInternal/SendInternalMessage/
// UpdateContractParams/SubmitSecurityAttestation/RevokeSecurityAttestation do
// above -- rather than adding a dedicated "...State" keeper wrapper (the
// InstantiateContractState/ExecuteExternalState pattern) -- because the
// keeper's own method here is already named MigrateContractState, so a
// wrapper following that convention would have to be named
// "MigrateContractStateState". Composing in the msg server avoids that and
// stays exactly consistent with the five sibling handlers already using this
// shape in this file.

func (m grpcMsgServer) UpgradeContractCode(ctx context.Context, msg *types.MsgUpgradeContractCode) (*types.MsgUpgradeContractCodeResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts upgrade-code request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	receipt, err := m.keeper.UpgradeContractCode(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &types.MsgUpgradeContractCodeResponse{Receipt: receipt}, nil
}

func (m grpcMsgServer) MigrateContractState(ctx context.Context, msg *types.MsgMigrateContractState) (*types.MsgMigrateContractStateResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts migrate-state request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	receipt, err := m.keeper.MigrateContractState(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &types.MsgMigrateContractStateResponse{Receipt: receipt}, nil
}

func (m grpcMsgServer) SetContractAdmin(ctx context.Context, msg *types.MsgSetContractAdmin) (*types.MsgSetContractAdminResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts set-admin request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	receipt, err := m.keeper.SetContractAdmin(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &types.MsgSetContractAdminResponse{Receipt: receipt}, nil
}

func (m grpcMsgServer) DisableContractUpgrades(ctx context.Context, msg *types.MsgDisableContractUpgrades) (*types.MsgDisableContractUpgradesResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts disable-upgrades request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	receipt, err := m.keeper.DisableContractUpgrades(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &types.MsgDisableContractUpgradesResponse{Receipt: receipt}, nil
}

func (m grpcMsgServer) ScheduleContractUpgrade(ctx context.Context, msg *types.MsgScheduleContractUpgrade) (*types.MsgScheduleContractUpgradeResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts schedule-upgrade request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	res, err := m.keeper.ScheduleContractUpgrade(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &res, nil
}

func (m grpcMsgServer) ApplyScheduledUpgrade(ctx context.Context, msg *types.MsgApplyScheduledUpgrade) (*types.MsgApplyScheduledUpgradeResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts apply-scheduled-upgrade request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	receipt, err := m.keeper.ApplyScheduledContractUpgrade(*msg)
	if err != nil {
		return nil, err
	}
	if err := m.keeper.writeGenesis(ctx); err != nil {
		return nil, err
	}
	return &types.MsgApplyScheduledUpgradeResponse{Receipt: receipt}, nil
}

func (m grpcMsgServer) DeleteExpiredContract(ctx context.Context, msg *types.MsgDeleteExpiredContract) (*types.MsgDeleteExpiredContractResponse, error) {
	if msg == nil {
		return nil, errors.New("empty contracts delete-expired-contract request")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	receipt, err := m.keeper.DeleteExpiredContractState(ctx, *msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgDeleteExpiredContractResponse{Receipt: receipt}, nil
}

func (q grpcQueryServer) Params(context.Context, *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return &types.QueryParamsResponse{Params: q.keeper.Params()}, nil
}

func (q grpcQueryServer) Code(_ context.Context, req *types.QueryCodeRequest) (*types.QueryCodeResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts code query")
	}
	code, found, err := q.keeper.Code(*req)
	return &types.QueryCodeResponse{Code: code, Found: found}, err
}

func (q grpcQueryServer) Codes(_ context.Context, req *types.QueryCodesRequest) (*types.QueryCodesResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts codes query")
	}
	codes, err := q.keeper.Codes(*req)
	return &types.QueryCodesResponse{Codes: codes}, err
}

func (q grpcQueryServer) ContractGet(_ context.Context, req *types.QueryContractGetRequest) (*types.QueryContractGetResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts get-method query")
	}
	res, err := q.keeper.ContractGet(*req)
	return &res, err
}

func (q grpcQueryServer) ContractManifest(_ context.Context, req *types.QueryContractManifestRequest) (*types.QueryContractManifestResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts manifest query")
	}
	res, err := q.keeper.ContractManifest(*req)
	return &res, err
}

func (q grpcQueryServer) Contract(_ context.Context, req *types.QueryContractRequest) (*types.QueryContractResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts contract query")
	}
	res, err := q.keeper.Contract(*req)
	return &res, err
}

func (q grpcQueryServer) Contracts(_ context.Context, req *types.QueryContractsRequest) (*types.QueryContractsResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts list query")
	}
	contracts, err := q.keeper.Contracts(*req)
	return &types.QueryContractsResponse{Contracts: contracts}, err
}

func (q grpcQueryServer) ContractStorage(_ context.Context, req *types.QueryContractStorageRequest) (*types.QueryContractStorageResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts storage query")
	}
	entries, err := q.keeper.ContractStorage(*req)
	return &types.QueryContractStorageResponse{Entries: entries}, err
}

func (q grpcQueryServer) ContractReceipts(_ context.Context, req *types.QueryContractReceiptsRequest) (*types.QueryContractReceiptsResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts receipts query")
	}
	receipts, err := q.keeper.ContractReceipts(*req)
	return &types.QueryContractReceiptsResponse{Receipts: receipts}, err
}

func (q grpcQueryServer) ContractQueue(_ context.Context, req *types.QueryContractQueueRequest) (*types.QueryContractQueueResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts queue query")
	}
	messages, err := q.keeper.ContractQueue(*req)
	return &types.QueryContractQueueResponse{Messages: messages}, err
}

func (q grpcQueryServer) ContractEvents(_ context.Context, req *types.QueryContractEventsRequest) (*types.QueryContractEventsResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts events query")
	}
	return &types.QueryContractEventsResponse{}, q.keeper.ContractEvents(*req)
}

func (q grpcQueryServer) ContractStateRoot(_ context.Context, req *types.QueryContractStateRootRequest) (*types.QueryContractStateRootResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts state root query")
	}
	root, err := q.keeper.ContractStateRoot(*req)
	return &types.QueryContractStateRootResponse{StateRoot: root}, err
}

func (q grpcQueryServer) SecurityAttestations(_ context.Context, req *types.QuerySecurityAttestationsRequest) (*types.QuerySecurityAttestationsResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts security attestations query")
	}
	attestations, err := q.keeper.SecurityAttestations(*req)
	return &types.QuerySecurityAttestationsResponse{Attestations: attestations}, err
}

func (q grpcQueryServer) SecurityBadge(_ context.Context, req *types.QuerySecurityBadgeRequest) (*types.QuerySecurityBadgeResponse, error) {
	if req == nil {
		return nil, errors.New("empty contracts security badge query")
	}
	badge, found, err := q.keeper.SecurityBadge(*req)
	return &types.QuerySecurityBadgeResponse{Badge: badge, Found: found}, err
}
