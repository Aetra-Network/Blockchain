package reporterv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterReporter{}, "l1/reporter/MsgRegisterReporter", nil)
	cdc.RegisterConcrete(&MsgBondReporter{}, "l1/reporter/MsgBondReporter", nil)
	cdc.RegisterConcrete(&MsgUnbondReporter{}, "l1/reporter/MsgUnbondReporter", nil)
	cdc.RegisterConcrete(&MsgSubmitReport{}, "l1/reporter/MsgSubmitReport", nil)
	cdc.RegisterConcrete(&MsgClaimReporterReward{}, "l1/reporter/MsgClaimReporterReward", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgRegisterReporter{},
		&MsgBondReporter{},
		&MsgUnbondReporter{},
		&MsgSubmitReport{},
		&MsgClaimReporterReward{},
	)
}