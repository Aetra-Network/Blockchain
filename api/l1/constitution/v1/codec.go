package constitutionv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgProposeConstitutionAmendment{}, "l1/constitution/MsgProposeConstitutionAmendment", nil)
	cdc.RegisterConcrete(&MsgProposeConstitutionAmendmentResponse{}, "l1/constitution/MsgProposeConstitutionAmendmentResponse", nil)
	cdc.RegisterConcrete(&MsgVoteConstitutionAmendment{}, "l1/constitution/MsgVoteConstitutionAmendment", nil)
	cdc.RegisterConcrete(&MsgVoteConstitutionAmendmentResponse{}, "l1/constitution/MsgVoteConstitutionAmendmentResponse", nil)
	cdc.RegisterConcrete(&MsgExecuteConstitutionAmendment{}, "l1/constitution/MsgExecuteConstitutionAmendment", nil)
	cdc.RegisterConcrete(&MsgExecuteConstitutionAmendmentResponse{}, "l1/constitution/MsgExecuteConstitutionAmendmentResponse", nil)
	cdc.RegisterConcrete(&MsgCancelConstitutionAmendment{}, "l1/constitution/MsgCancelConstitutionAmendment", nil)
	cdc.RegisterConcrete(&MsgCancelConstitutionAmendmentResponse{}, "l1/constitution/MsgCancelConstitutionAmendmentResponse", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgProposeConstitutionAmendment{},
		&MsgProposeConstitutionAmendmentResponse{},
		&MsgVoteConstitutionAmendment{},
		&MsgVoteConstitutionAmendmentResponse{},
		&MsgExecuteConstitutionAmendment{},
		&MsgExecuteConstitutionAmendmentResponse{},
		&MsgCancelConstitutionAmendment{},
		&MsgCancelConstitutionAmendmentResponse{},
	)
}