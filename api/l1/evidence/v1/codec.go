package evidencev1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSubmitEvidence{}, "l1/evidence/MsgSubmitEvidence", nil)
	cdc.RegisterConcrete(&MsgVoteEvidence{}, "l1/evidence/MsgVoteEvidence", nil)
	cdc.RegisterConcrete(&MsgFinalizeEvidence{}, "l1/evidence/MsgFinalizeEvidence", nil)
	cdc.RegisterConcrete(&MsgCancelExpiredEvidence{}, "l1/evidence/MsgCancelExpiredEvidence", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgSubmitEvidence{},
		&MsgVoteEvidence{},
		&MsgFinalizeEvidence{},
		&MsgCancelExpiredEvidence{},
	)
}