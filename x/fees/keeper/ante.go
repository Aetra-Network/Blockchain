package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/observability"
	"github.com/sovereign-l1/l1/x/fees/types"
)

func (k Keeper) AnteHandlerDecorator(next sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if err := validateNoZeroTxAddresses(tx); err != nil {
			return ctx, err
		}
		if err := aetraaddress.ValidateAnteAddressPolicy(tx); err != nil {
			return ctx, types.ErrInvalidFee.Wrap(err.Error())
		}
		if isGenesisCreateValidatorTx(ctx, tx) {
			return next(ctx, tx, simulate)
		}
		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			observability.RecordFeeRejected("missing_fee_tx")
			return ctx, types.ErrInvalidFee.Wrap("transaction must expose fees")
		}
		if err := validateTxEnvelope(ctx, tx); err != nil {
			observability.RecordFeeRejected("tx_envelope_limit")
			return ctx, err
		}
		fees := feeTx.GetFee()
		if _, err := k.AdmitTx(ctx, feeTx, selectTxSender(tx, feeTx), simulate); err != nil {
			return ctx, err
		}
		newCtx, err := next(ctx, tx, simulate)
		if err != nil || simulate {
			if err != nil {
				observability.RecordModuleError(types.ModuleName, "ante", "next_error")
			}
			return newCtx, err
		}
		if err := k.RecordCollectedFees(newCtx, fees); err != nil {
			observability.RecordModuleError(types.ModuleName, "record_collected_fees", "error")
			return newCtx, err
		}
		observability.RecordFeeAccepted()
		return newCtx, nil
	}
}

func validateTxEnvelope(ctx sdk.Context, tx sdk.Tx) error {
	// Count nested messages too (e.g. inside an authz.MsgExec), not just the
	// top-level envelope, so the per-tx message-count cap actually bounds the
	// number of messages that will execute (FINDING-013).
	msgCount, err := aetraaddress.CountMessages(tx.GetMsgs())
	if err != nil {
		return types.ErrInvalidFee.Wrap(err.Error())
	}
	input := types.TxEnvelopeInput{MsgCount: msgCount}
	if txBytes := ctx.TxBytes(); len(txBytes) > 0 {
		input.TxBytes = uint64(len(txBytes))
	} else if sizedTx, ok := tx.(interface{ Size() int }); ok && sizedTx.Size() > 0 {
		input.TxBytes = uint64(sizedTx.Size())
	}
	if memoTx, ok := tx.(sdk.TxWithMemo); ok {
		input.Memo = memoTx.GetMemo()
	}
	return types.ValidateTxEnvelope(types.DefaultTxEnvelopeLimits(), input)
}

func validateNoZeroTxAddresses(tx sdk.Tx) error {
	// Walk nested messages too (e.g. inside an authz.MsgExec), not just the
	// top-level envelope, so a zero-address / reserved-recipient send cannot
	// evade this guard by being wrapped (FINDING-014).
	if err := aetraaddress.WalkMessages(tx.GetMsgs(), validateNoZeroMsgAddresses); err != nil {
		return err
	}
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		if payer := sdk.AccAddress(feeTx.FeePayer()); len(payer) > 0 {
			if aetraaddress.IsZeroAccAddress(payer) {
				return types.ErrInvalidFee.Wrap("fee payer must not be zero address")
			}
			if reserved, found := aetraaddress.SystemAddressByBytes(payer); found {
				return types.ErrInvalidFee.Wrapf("fee payer is reserved system address %s", reserved.Name)
			}
		}
	}
	if sigTx, ok := tx.(authsigning.SigVerifiableTx); ok {
		signers, err := sigTx.GetSigners()
		if err != nil {
			return err
		}
		for i, signer := range signers {
			if aetraaddress.IsZero(signer) {
				return types.ErrInvalidFee.Wrapf("signer %d must not be zero address", i)
			}
			if reserved, found := aetraaddress.SystemAddressByBytes(signer); found {
				return types.ErrInvalidFee.Wrapf("signer %d is reserved system address %s", i, reserved.Name)
			}
		}
	}
	return nil
}

func selectTxSender(tx sdk.Tx, feeTx sdk.FeeTx) sdk.AccAddress {
	if payer := sdk.AccAddress(feeTx.FeePayer()); len(payer) > 0 {
		return payer
	}
	if sigTx, ok := tx.(authsigning.SigVerifiableTx); ok {
		signers, err := sigTx.GetSigners()
		if err == nil {
			for _, signer := range signers {
				if len(signer) > 0 {
					return sdk.AccAddress(signer)
				}
			}
		}
	}
	return nil
}

func validateNoZeroMsgAddresses(msg sdk.Msg) error {
	switch msg := msg.(type) {
	case *banktypes.MsgSend:
		if err := validateWellFormedNonZeroAddress("bank send sender", msg.FromAddress); err != nil {
			return types.ErrInvalidFee.Wrap(err.Error())
		}
		if err := validateUserFundSender("bank send sender", msg.FromAddress); err != nil {
			return err
		}
		if err := validateWellFormedNonZeroAddress("bank send recipient", msg.ToAddress); err != nil {
			return types.ErrInvalidFee.Wrap(err.Error())
		}
		if err := validateUserFundRecipient("bank send recipient", msg.ToAddress); err != nil {
			return err
		}
	case *banktypes.MsgMultiSend:
		for i, input := range msg.Inputs {
			if err := validateWellFormedNonZeroAddress("bank multisend input", input.Address); err != nil {
				return types.ErrInvalidFee.Wrapf("input %d: %s", i, err.Error())
			}
			if err := validateUserFundSender("bank multisend input", input.Address); err != nil {
				return types.ErrInvalidFee.Wrapf("input %d: %s", i, err.Error())
			}
		}
		for i, output := range msg.Outputs {
			if err := validateWellFormedNonZeroAddress("bank multisend output", output.Address); err != nil {
				return types.ErrInvalidFee.Wrapf("output %d: %s", i, err.Error())
			}
			if err := validateUserFundRecipient("bank multisend output", output.Address); err != nil {
				return types.ErrInvalidFee.Wrapf("output %d: %s", i, err.Error())
			}
		}
	case *distrtypes.MsgSetWithdrawAddress:
		if err := validateWellFormedNonZeroAddress("distribution withdraw delegator", msg.DelegatorAddress); err != nil {
			return types.ErrInvalidFee.Wrap(err.Error())
		}
		if err := validateWellFormedNonZeroAddress("distribution withdraw address", msg.WithdrawAddress); err != nil {
			return types.ErrInvalidFee.Wrap(err.Error())
		}
		if err := validateUserFundRecipient("distribution withdraw address", msg.WithdrawAddress); err != nil {
			return err
		}
	}
	return nil
}

// validateWellFormedNonZeroAddress checks that text is a well-formed,
// non-zero address in either form aetraaddress.Parse accepts (the "AE…"
// user-facing form or the "ae1…" bech32 form). Unlike
// aetraaddress.ValidateUserAddress, it does not require the "AE…" form
// specifically: the fields validated here belong to stock cosmos-sdk
// message types (bank.MsgSend/MsgMultiSend,
// distribution.MsgSetWithdrawAddress), whose address string fields are
// always populated by the SDK's own bech32 (ae1…) address-to-string
// conversion when a client builds these messages -- there is no way for a
// client to make these fields carry the "AE…" form instead, so requiring it
// here rejected every legitimate transaction using these message types.
func validateWellFormedNonZeroAddress(field, text string) error {
	bz, err := aetraaddress.Parse(text)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	return aetraaddress.RejectZeroAddress(field, bz)
}

func validateUserFundSender(field, text string) error {
	if reserved, found := aetraaddress.SystemAddressByText(text); found {
		return types.ErrInvalidFee.Wrapf("%s is reserved system address %s", field, reserved.Name)
	}
	return nil
}

func validateUserFundRecipient(field, text string) error {
	if reserved, found := aetraaddress.SystemAddressByText(text); found && !reserved.CanReceiveUserFunds {
		return types.ErrInvalidFee.Wrapf("%s is reserved system address %s and cannot receive user funds", field, reserved.Name)
	}
	return nil
}

func isGenesisCreateValidatorTx(ctx sdk.Context, tx sdk.Tx) bool {
	if ctx.BlockHeight() != 0 {
		return false
	}
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return false
	}
	for _, msg := range msgs {
		if _, ok := msg.(*stakingtypes.MsgCreateValidator); !ok {
			return false
		}
	}
	return true
}
