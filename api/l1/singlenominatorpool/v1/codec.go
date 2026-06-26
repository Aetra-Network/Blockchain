package singlenominatorpoolv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateSingleNominatorPool{}, "l1/single-nominator-pool/MsgCreateSingleNominatorPool", nil)
	cdc.RegisterConcrete(&MsgDepositSingleNominator{}, "l1/single-nominator-pool/MsgDepositSingleNominator", nil)
	cdc.RegisterConcrete(&MsgWithdrawSingleNominator{}, "l1/single-nominator-pool/MsgWithdrawSingleNominator", nil)
	cdc.RegisterConcrete(&MsgClaimSingleNominatorRewards{}, "l1/single-nominator-pool/MsgClaimSingleNominatorRewards", nil)
	cdc.RegisterConcrete(&MsgEmergencyLockSingleNominator{}, "l1/single-nominator-pool/MsgEmergencyLockSingleNominator", nil)
	cdc.RegisterConcrete(&MsgChangeSingleNominatorValidator{}, "l1/single-nominator-pool/MsgChangeSingleNominatorValidator", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgCreateSingleNominatorPool{},
		&MsgDepositSingleNominator{},
		&MsgWithdrawSingleNominator{},
		&MsgClaimSingleNominatorRewards{},
		&MsgEmergencyLockSingleNominator{},
		&MsgChangeSingleNominatorValidator{},
	)
}