package validatorelectionv1

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgApplyForValidatorSet{}, "l1/validator-election/MsgApplyForValidatorSet", nil)
	cdc.RegisterConcrete(&MsgWithdrawApplication{}, "l1/validator-election/MsgWithdrawApplication", nil)
	cdc.RegisterConcrete(&MsgCommitElection{}, "l1/validator-election/MsgCommitElection", nil)
	cdc.RegisterConcrete(&MsgFinalizeElection{}, "l1/validator-election/MsgFinalizeElection", nil)
	cdc.RegisterConcrete(&MsgRequestValidatorExit{}, "l1/validator-election/MsgRequestValidatorExit", nil)
	cdc.RegisterConcrete(&MsgCancelValidatorExit{}, "l1/validator-election/MsgCancelValidatorExit", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgApplyForValidatorSet{},
		&MsgWithdrawApplication{},
		&MsgCommitElection{},
		&MsgFinalizeElection{},
		&MsgRequestValidatorExit{},
		&MsgCancelValidatorExit{},
	)
}
