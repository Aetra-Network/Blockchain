package app

import (
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	emissionstypes "github.com/sovereign-l1/l1/x/emissions/types"
	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
	mintauthoritytypes "github.com/sovereign-l1/l1/x/mint-authority/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// EventTypeNativeEmissionSkipped is emitted when an epoch's scheduled emission
// is skipped because it would exceed a mint-authority safety cap. Skipping
// (rather than erroring) keeps a mis-parameterized cap from halting the chain
// in EndBlock.
const EventTypeNativeEmissionSkipped = "native_emission_skipped"

// FinalizeNativeEconomyEpoch connects emission accounting to bank supply and
// module balances. Rounding remainder is credited to treasury/community.
//
// The scheduled emission is previewed and pre-checked against the
// mint-authority caps before any state is committed. A cap breach is treated
// as a policy limit: the epoch's emission is skipped (with an event) instead
// of returning an error, because this runs from EndBlock where a returned
// error deterministically halts the whole chain. See security audit finding
// SEC-CRIT: genesis emission vs mint-cap chain halt.
func (app *L1App) FinalizeNativeEconomyEpoch(ctx sdk.Context, epoch uint64, stakingRatioBps uint32) (emissionstypes.EmissionEpoch, error) {
	if ctx.BlockHeight() < 0 {
		return emissionstypes.EmissionEpoch{}, fmt.Errorf("native economy epoch height cannot be negative")
	}

	// Reject an already-finalized epoch up front so the duplicate is reported
	// as such rather than surfacing later as a mint-authority duplicate event.
	if _, found, err := app.EmissionsKeeper.GetEmissionEpoch(ctx, epoch); err != nil {
		return emissionstypes.EmissionEpoch{}, err
	} else if found {
		return emissionstypes.EmissionEpoch{}, emissionstypes.ErrDuplicateEpoch.Wrapf("epoch %d", epoch)
	}

	emParams, err := app.EmissionsKeeper.GetParams(ctx)
	if err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}
	// ComputeEpochEmission is pure, so this preview is byte-identical to the
	// amount FinalizeEmissionEpoch commits below (params are not mutated in
	// between). This lets us pre-check the mint-authority caps without leaving
	// a recorded-but-unminted epoch behind if the cap rejects it.
	preview, err := emissionstypes.ComputeEpochEmission(emParams, epoch, uint64(stakingRatioBps), ctx.BlockHeight())
	if err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}

	state, err := app.MintAuthorityKeeper.GetState(ctx)
	if err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}

	var newState mintauthoritytypes.MintAuthorityState
	if preview.EmissionAmount.Amount.IsPositive() {
		decision := mintauthoritytypes.EmissionDecision{
			Caller:		mintauthoritytypes.DefaultEmissionCaller,
			Denom:		preview.EmissionAmount.Denom,
			Amount:		preview.EmissionAmount.Amount,
			Epoch:		epoch,
			Height:		uint64(ctx.BlockHeight()),
			Approved:	true,
		}
		decision.DecisionHash = mintauthoritytypes.ComputeEmissionDecisionHash(decision)

		newState, _, err = mintauthoritytypes.ApplyMintProtocolCoins(state, mintauthoritytypes.MsgMintProtocolCoins{
			Caller:			mintauthoritytypes.DefaultEmissionCaller,
			Recipient:		aetraaddress.FormatAccAddress(app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)),
			Denom:			preview.EmissionAmount.Denom,
			Amount:			preview.EmissionAmount.Amount,
			Epoch:			epoch,
			Height:			uint64(ctx.BlockHeight()),
			EmissionsDecisionHash:	decision.DecisionHash,
		}, decision, mintauthoritytypes.ConstitutionEmergencyAuthorization{})
		if err != nil {
			if errors.Is(err, mintauthoritytypes.ErrEpochCapExceeded) || errors.Is(err, mintauthoritytypes.ErrLifetimeCapExceeded) {
				ctx.Logger().Error("native emission skipped: mint authority cap exceeded",
					"epoch", epoch, "amount", preview.EmissionAmount.String(), "err", err.Error())
				ctx.EventManager().EmitEvent(sdk.NewEvent(
					EventTypeNativeEmissionSkipped,
					sdk.NewAttribute("epoch", fmt.Sprintf("%d", epoch)),
					sdk.NewAttribute("amount", preview.EmissionAmount.String()),
					sdk.NewAttribute("reason", err.Error()),
				))
				return emissionstypes.EmissionEpoch{}, nil
			}
			return emissionstypes.EmissionEpoch{}, err
		}
	}

	// Caps cleared (or emission is zero): commit the emission epoch. The
	// recomputed amount equals the pre-checked preview.
	record, err := app.EmissionsKeeper.FinalizeEmissionEpoch(ctx, epoch, stakingRatioBps)
	if err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}
	if record.EmissionAmount.Amount.IsZero() {
		return record, nil
	}

	if err := app.MintAuthorityKeeper.SetState(ctx, newState); err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}

	if err := app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(record.EmissionAmount)); err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}
	if err := app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, sdk.NewCoins(record.EmissionAmount)); err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}
	if err := app.distributeNativeEmission(ctx, epoch, record); err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}
	return record, nil
}

func (app *L1App) maybeFinalizeNativeEmissionEpoch(ctx sdk.Context) error {
	if ctx.BlockHeight() <= 0 {
		return nil
	}
	interval := uint64(nominatorpooltypes.DefaultRewardEpochDurationBlocks)
	height := uint64(ctx.BlockHeight())
	if interval == 0 || height%interval != 0 {
		return nil
	}
	epoch := height / interval
	if _, found, err := app.EmissionsKeeper.GetEmissionEpoch(ctx, epoch); err != nil {
		return err
	} else if found {
		return nil
	}
	params, err := app.EmissionsKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	_, err = app.FinalizeNativeEconomyEpoch(ctx, epoch, params.TargetStakingRatioBps)
	return err
}

func (app *L1App) distributeNativeEmission(ctx sdk.Context, epoch uint64, record emissionstypes.EmissionEpoch) error {

	treasury := record.Treasury
	if record.RoundingRemainder.Amount.IsPositive() {
		treasury = treasury.Add(record.RoundingRemainder)
	}
	if err := app.sendFromFeeCollector(ctx, feecollectortypes.TreasuryModuleName, treasury); err != nil {
		return err
	}
	if err := app.sendFromFeeCollector(ctx, feecollectortypes.ProtectionModuleName, record.ProtectionFund); err != nil {
		return err
	}
	if err := app.sendFromFeeCollector(ctx, feecollectortypes.EcosystemGrantsModuleName, record.Ecosystem); err != nil {
		return err
	}
	if record.Burn.Amount.IsPositive() {
		if _, err := app.BurnKeeper.BurnProtocolCoins(ctx, authtypes.FeeCollectorName, sdk.NewCoins(record.Burn), epoch, "emissions.distribute"); err != nil {
			return err
		}
	}
	return nil
}

func (app *L1App) sendFromFeeCollector(ctx sdk.Context, recipientModule string, coin sdk.Coin) error {
	if coin.Amount.IsNil() || !coin.Amount.IsPositive() {
		return nil
	}
	return app.BankKeeper.SendCoinsFromModuleToModule(ctx, authtypes.FeeCollectorName, recipientModule, sdk.NewCoins(coin))
}
