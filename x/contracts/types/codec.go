package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgStoreCode{}, "l1/contracts/MsgStoreCode", nil)
	cdc.RegisterConcrete(&MsgDeployContract{}, "l1/contracts/MsgDeployContract", nil)
	cdc.RegisterConcrete(&MsgExecuteExternal{}, "l1/contracts/MsgExecuteExternal", nil)
	cdc.RegisterConcrete(&MsgExecuteInternal{}, "l1/contracts/MsgExecuteInternal", nil)
	cdc.RegisterConcrete(&MsgSendInternalMessage{}, "l1/contracts/MsgSendInternalMessage", nil)
	cdc.RegisterConcrete(&MsgUpdateContractParams{}, "l1/contracts/MsgUpdateContractParams", nil)
	cdc.RegisterConcrete(&MsgSubmitSecurityAttestation{}, "l1/contracts/MsgSubmitSecurityAttestation", nil)
	cdc.RegisterConcrete(&MsgRevokeSecurityAttestation{}, "l1/contracts/MsgRevokeSecurityAttestation", nil)
	cdc.RegisterConcrete(&MsgTopUpContract{}, "l1/contracts/MsgTopUpContract", nil)
	cdc.RegisterConcrete(&MsgPayContractStorageDebt{}, "l1/contracts/MsgPayContractStorageDebt", nil)
	cdc.RegisterConcrete(&MsgUnfreezeContract{}, "l1/contracts/MsgUnfreezeContract", nil)
	cdc.RegisterConcrete(&MsgUpgradeContractCode{}, "l1/contracts/MsgUpgradeContractCode", nil)
	cdc.RegisterConcrete(&MsgMigrateContractState{}, "l1/contracts/MsgMigrateContractState", nil)
	cdc.RegisterConcrete(&MsgSetContractAdmin{}, "l1/contracts/MsgSetContractAdmin", nil)
	cdc.RegisterConcrete(&MsgDisableContractUpgrades{}, "l1/contracts/MsgDisableContractUpgrades", nil)
	cdc.RegisterConcrete(&MsgScheduleContractUpgrade{}, "l1/contracts/MsgScheduleContractUpgrade", nil)
	cdc.RegisterConcrete(&MsgApplyScheduledUpgrade{}, "l1/contracts/MsgApplyScheduledUpgrade", nil)
	cdc.RegisterConcrete(&MsgDeleteExpiredContract{}, "l1/contracts/MsgDeleteExpiredContract", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgStoreCode{},
		&MsgDeployContract{},
		&MsgExecuteExternal{},
		&MsgExecuteInternal{},
		&MsgSendInternalMessage{},
		&MsgUpdateContractParams{},
		&MsgSubmitSecurityAttestation{},
		&MsgRevokeSecurityAttestation{},
		&MsgTopUpContract{},
		&MsgPayContractStorageDebt{},
		&MsgUnfreezeContract{},
		&MsgUpgradeContractCode{},
		&MsgMigrateContractState{},
		&MsgSetContractAdmin{},
		&MsgDisableContractUpgrades{},
		&MsgScheduleContractUpgrade{},
		&MsgApplyScheduledUpgrade{},
		&MsgDeleteExpiredContract{},
	)
	registry.RegisterImplementations(
		(*txtypes.MsgResponse)(nil),
		&StoreCodeResponse{},
		&InstantiateContractResponse{},
		&ExecuteContractResponse{},
		&InternalMessage{},
		&MsgUpdateContractParamsResponse{},
		&MsgSubmitSecurityAttestationResponse{},
		&MsgRevokeSecurityAttestationResponse{},
		&MsgTopUpContractResponse{},
		&MsgPayContractStorageDebtResponse{},
		&MsgUnfreezeContractResponse{},
		&MsgUpgradeContractCodeResponse{},
		&MsgMigrateContractStateResponse{},
		&MsgSetContractAdminResponse{},
		&MsgDisableContractUpgradesResponse{},
		&MsgScheduleContractUpgradeResponse{},
		&MsgApplyScheduledUpgradeResponse{},
		&MsgDeleteExpiredContractResponse{},
	)
}
