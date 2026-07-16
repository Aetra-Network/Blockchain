package app

import (
	"errors"
	"fmt"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
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
	// Anchor this epoch's emission to the chain's real circulating supply, not
	// to the genesis-time AnnualReferenceSupply constant. With the constant,
	// inflation is a FIXED amount regardless of supply -- 3% of 365 AET is
	// 10.95 AET/year whether the chain holds 21,000 AET or 21,000,000, i.e. an
	// effective rate of 0.05% rather than the 3% the params claim.
	emParams, err := app.EmissionsKeeper.GetParams(ctx)
	if err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}
	anchor, err := app.emissionReferenceSupply(ctx, emParams.BaseDenom)
	if err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}
	return app.finalizeNativeEconomyEpoch(ctx, epoch, stakingRatioBps, anchor)
}

// FinalizeNativeEconomyEpochWithReferenceSupply is FinalizeNativeEconomyEpoch
// against a caller-supplied supply anchor instead of the chain's real
// circulating supply. Production code should always use
// FinalizeNativeEconomyEpoch; this exists so tests can hand-verify the
// distribution math (fee/emission splits, pool rewards, module balances) with
// small, round numbers without depending on whatever a test genesis's real
// supply happens to be.
func (app *L1App) FinalizeNativeEconomyEpochWithReferenceSupply(ctx sdk.Context, epoch uint64, stakingRatioBps uint32, anchor sdkmath.Int) (emissionstypes.EmissionEpoch, error) {
	return app.finalizeNativeEconomyEpoch(ctx, epoch, stakingRatioBps, anchor)
}

func (app *L1App) finalizeNativeEconomyEpoch(ctx sdk.Context, epoch uint64, stakingRatioBps uint32, anchor sdkmath.Int) (emissionstypes.EmissionEpoch, error) {
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
	// amount FinalizeEmissionEpoch commits below (both are handed the same
	// anchor and params are not mutated in between). This lets us pre-check the
	// mint-authority caps without leaving a recorded-but-unminted epoch behind
	// if the cap rejects it.
	preview, err := emissionstypes.ComputeEpochEmissionWithSupply(emParams, epoch, uint64(stakingRatioBps), ctx.BlockHeight(), anchor)
	if err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}

	state, err := app.MintAuthorityKeeper.GetState(ctx)
	if err != nil {
		return emissionstypes.EmissionEpoch{}, err
	}
	// Re-size the mint-authority caps against the same anchor, rate and
	// cadence. They are derived from AnnualReferenceSupply too, so
	// re-anchoring emission without re-anchoring the caps would push every
	// epoch past ErrEpochCapExceeded and silently skip it below -- turning
	// 0.05% inflation into exactly 0%. The rate/cadence must be x/emissions'
	// own configured values, not the package-level bootstrap constants: a
	// chain (or a test) that overrides ConstitutionalMaxInflationBps or
	// EpochsPerYear away from the defaults would otherwise size the cap for a
	// different schedule than the one actually emitting.
	state = refreshMintCapsForSupply(state, emParams.BaseDenom, anchor,
		int64(emParams.ConstitutionalMaxInflationBps), int64(emParams.EpochsPerYear))

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
	record, err := app.EmissionsKeeper.FinalizeEmissionEpochWithSupply(ctx, epoch, stakingRatioBps, anchor)
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

// nonCirculatingEmissionModules are protocol reserves whose balances are not in
// circulation, so inflation should not be computed against them. Bonded stake
// is deliberately absent: it IS circulating and is exactly what the target
// staking ratio is measured over.
var nonCirculatingEmissionModules = []string{
	feecollectortypes.TreasuryModuleName,
	feecollectortypes.ProtectionModuleName,
	feecollectortypes.EcosystemGrantsModuleName,
	feecollectortypes.StorageRentReserveModuleName,
}

// emissionReferenceSupply is the anchor this epoch's inflation is applied to:
// real bank supply of the base denom, less the protocol reserves that are not
// in circulation. Burned coins need no subtraction -- BurnCoins already reduced
// supply.
//
// A chain with no supply yet (genesis edge) falls back to the bootstrap
// constant so emission is defined rather than zero.
func (app *L1App) emissionReferenceSupply(ctx sdk.Context, denom string) (sdkmath.Int, error) {
	supply := app.BankKeeper.GetSupply(ctx, denom).Amount
	for _, module := range nonCirculatingEmissionModules {
		addr := app.AccountKeeper.GetModuleAddress(module)
		if addr == nil {
			continue
		}
		supply = supply.Sub(app.BankKeeper.GetBalance(ctx, addr, denom).Amount)
	}
	if !supply.IsPositive() {
		return sdkmath.NewInt(appparams.AnnualReferenceSupplyNaet), nil
	}
	return supply, nil
}

// refreshMintCapsForSupply re-derives the mint-authority safety ceilings from a
// live supply anchor and x/emissions' own configured maximum rate/cadence, so
// they bound emission at a fixed multiple of the constitutional maximum rate
// rather than at a genesis-time constant.
//
// CapHash is a content hash of Denom/EpochCap/LifetimeCap, verified by
// ApplyMintProtocolCoins (authority.go:289); it must be recomputed whenever
// the caps it covers change, or the state fails validation on the next write.
func refreshMintCapsForSupply(state mintauthoritytypes.MintAuthorityState, denom string, anchor sdkmath.Int, maxInflationBps, epochsPerYear int64) mintauthoritytypes.MintAuthorityState {
	epochCap := appparams.MintAuthorityEpochCapNaetFor(anchor, maxInflationBps, epochsPerYear)
	lifetimeCap := appparams.MintAuthorityLifetimeCapNaetFor(anchor, maxInflationBps, epochsPerYear)
	caps := make([]mintauthoritytypes.MintCap, 0, len(state.Caps))
	found := false
	for _, cap := range state.Caps {
		if cap.Denom == denom {
			cap.EpochCap = epochCap
			cap.LifetimeCap = lifetimeCap
			cap.CapHash = mintauthoritytypes.ComputeMintCapHash(cap)
			found = true
		}
		caps = append(caps, cap)
	}
	if !found {
		cap := mintauthoritytypes.MintCap{
			Denom:       denom,
			EpochCap:    epochCap,
			LifetimeCap: lifetimeCap,
		}
		cap.CapHash = mintauthoritytypes.ComputeMintCapHash(cap)
		caps = append(caps, cap)
	}
	state.Caps = caps
	return state
}

// realStakingRatioBps is the chain's actual bonded ratio in basis points.
//
// Passing the TARGET ratio here (as this EndBlocker previously did) makes
// ComputeInflationBps compute delta = target - actual = 0 on every epoch, so
// inflation never moves off its current value and the whole 1.5%-5% adaptive
// band is decorative.
func (app *L1App) realStakingRatioBps(ctx sdk.Context) (uint32, error) {
	ratio, err := app.StakingKeeper.BondedRatio(ctx)
	if err != nil {
		return 0, err
	}
	bps := ratio.MulInt64(appparams.BasisPoints).TruncateInt64()
	if bps < 0 {
		bps = 0
	}
	if bps > appparams.BasisPoints {
		bps = appparams.BasisPoints
	}
	return uint32(bps), nil
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
	stakingRatioBps, err := app.realStakingRatioBps(ctx)
	if err != nil {
		return err
	}
	_, err = app.FinalizeNativeEconomyEpoch(ctx, epoch, stakingRatioBps)
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
