package types

import (
	"fmt"
	"sort"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/sovereign-l1/l1/app/params"
)

// DefaultDistributionWeights splits each epoch's emission. The weights must sum
// to 10000 bps; they decide WHERE a fixed emission lands, never HOW MUCH is
// minted, so every weight here is neutral to total supply growth.
//
// v = ValidatorRewardBps = 8500 sends the bulk of emission to the parties that
// actually secure the chain. With inflation once again the adaptive staking
// lever (see appparams.DefaultTargetInflationBps), the split is no longer asked
// to double as a second lever; v is simply sized so emission reaches
// circulating validators and delegators rather than being stranded in reserves.
//
// Sizing v = 8500:
//   - Circulating growth is NET_circ = v*i - (burn + treasury share of fees).
//     At v = 7000 and the 4% start that is 0.70*4.00% - 0.641% = 2.16%; the
//     shortfall is emission minted into reserves that never circulate. At
//     v = 8500 it is 0.85*4.00% - 0.641% = 3.16%, so almost all of the start
//     rate reaches holders.
//   - Validator APR = 0.98*(v*i + 0.35*phi)/sigma is a real return at the
//     target operating point, satisfying the "validators must earn" constraint.
//
// ProtectionBps and BurnBps go to 0 deliberately:
//   - Burn: burning freshly minted coins is a no-op that only makes the
//     advertised rate a lie (mint i, destroy some of it, realize less). The
//     protocol's burn belongs on the FEE side, where it is capped and
//     supply-aware (see appparams.EmissionFeeBurnAnnualCapBps).
//   - Protection: the protection module has no spend path (only x/treasury can
//     spend), so minting into it is phantom inflation -- it dilutes holders and
//     buys nothing. Restore a weight here only alongside a spend path.
func DefaultDistributionWeights() DistributionWeights {
	return DistributionWeights{
		ValidatorRewardBps:	8_500,
		TreasuryBps:		1_000,
		ProtectionBps:		0,
		BurnBps:		0,
		EcosystemBps:		500,
	}
}

// DefaultParams seeds the ADAPTIVE emission controller. CurrentInflationBps is
// the 4.00% start; MinAnnualInflationBps / MaxAnnualInflationBps open the band
// to [1.50%, 8.00%] so ComputeInflationBps can steer with the bonded ratio.
//
// ComputeInflationBps is an INTEGRATOR --
// next = current + (target - actual)*resp/10^4 -- whose output is written back
// as the next epoch's input (keeper.go FinalizeEmissionEpoch). With the band
// open this is exactly the intended behaviour: a bonded ratio persistently
// below the 65% target walks the rate UP toward the 8% ceiling to attract
// stake, and one persistently above walks it DOWN toward the 1.5% floor. The
// real bonded ratio reaches this function through the epoch EndBlocker
// (app/native_economy.go realStakingRatioBps -> StakingKeeper.BondedRatio), not
// a hardcoded target, so the steer tracks committed state.
//
// NET supply growth is therefore no longer a fixed band; it floats with stake
// (see appparams.DefaultTargetInflationBps). ConstitutionalMaxInflationBps is
// the hard mint ceiling, equal to the top of the band, leaving governance room
// to widen it later.
func DefaultParams() Params {
	return Params{
		BaseDenom:			BaseDenom,
		CurrentInflationBps:		uint32(appparams.DefaultTargetInflationBps),
		TargetStakingRatioBps:		uint32(appparams.DefaultTargetStakeBps),
		MinAnnualInflationBps:		uint32(appparams.MinInflationBps),
		MaxAnnualInflationBps:		uint32(appparams.MaxInflationBps),
		ConstitutionalMaxInflationBps:	uint32(appparams.EmissionConstitutionalMaxInflationBps),
		ResponsivenessBps:		uint32(appparams.DefaultResponsivenessBps),
		AnnualReferenceSupply:		sdk.NewInt64Coin(BaseDenom, appparams.AnnualReferenceSupplyNaet),
		EpochsPerYear:			uint64(appparams.EpochsPerYear),
		DistributionWeights:		DefaultDistributionWeights(),
	}
}

func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:			DefaultParams(),
		EpochHistory:		[]EmissionEpoch{},
		TotalMintedAccounting:	sdk.NewInt64Coin(BaseDenom, 0),
	}
}

func NormalizeParams(params Params) Params {
	if params.BaseDenom == "" {
		params.BaseDenom = BaseDenom
	}
	if params.CurrentInflationBps == 0 {
		params.CurrentInflationBps = uint32(appparams.DefaultTargetInflationBps)
	}
	if params.TargetStakingRatioBps == 0 {
		params.TargetStakingRatioBps = uint32(appparams.DefaultTargetStakeBps)
	}
	// Refill the band from the adaptive bounds appparams.MinInflationBps /
	// MaxInflationBps (150/800). These are the governance-legal band the emission
	// controller steers inside; a genesis that omits the fields inherits the full
	// adaptive band rather than a welded rate.
	if params.MinAnnualInflationBps == 0 {
		params.MinAnnualInflationBps = uint32(appparams.MinInflationBps)
	}
	if params.MaxAnnualInflationBps == 0 {
		params.MaxAnnualInflationBps = uint32(appparams.MaxInflationBps)
	}
	// Refill the constitutional ceiling from EmissionConstitutionalMaxInflationBps
	// (800 bps). It equals the top of the adaptive band today, but is kept a
	// distinct knob so governance can widen MaxAnnualInflationBps up to it without
	// a constitutional amendment.
	if params.ConstitutionalMaxInflationBps == 0 {
		params.ConstitutionalMaxInflationBps = uint32(appparams.EmissionConstitutionalMaxInflationBps)
	}
	if params.ResponsivenessBps == 0 {
		params.ResponsivenessBps = uint32(appparams.DefaultResponsivenessBps)
	}
	if params.AnnualReferenceSupply.Denom == "" && params.AnnualReferenceSupply.Amount.IsNil() {
		params.AnnualReferenceSupply = sdk.NewInt64Coin(params.BaseDenom, appparams.AnnualReferenceSupplyNaet)
	}
	if params.EpochsPerYear == 0 {
		params.EpochsPerYear = uint64(appparams.EpochsPerYear)
	}
	if params.DistributionWeights == (DistributionWeights{}) {
		params.DistributionWeights = DefaultDistributionWeights()
	}
	return params
}

func (p Params) Validate() error {
	if p.BaseDenom != BaseDenom {
		return fmt.Errorf("base_denom must be %s", BaseDenom)
	}
	if p.TargetStakingRatioBps > BasisPoints {
		return fmt.Errorf("target_staking_ratio_bps cannot exceed %d", BasisPoints)
	}
	if p.CurrentInflationBps > p.ConstitutionalMaxInflationBps {
		return fmt.Errorf("current inflation cannot exceed constitutional maximum")
	}
	if p.MinAnnualInflationBps > p.MaxAnnualInflationBps {
		return fmt.Errorf("min annual inflation cannot exceed max")
	}
	if p.MaxAnnualInflationBps > p.ConstitutionalMaxInflationBps {
		return fmt.Errorf("max annual inflation cannot exceed constitutional maximum")
	}
	if p.ResponsivenessBps > BasisPoints {
		return fmt.Errorf("responsiveness_bps cannot exceed %d", BasisPoints)
	}
	if p.EpochsPerYear == 0 {
		return fmt.Errorf("epochs_per_year must be positive")
	}
	if err := validateCoin(p.BaseDenom, p.AnnualReferenceSupply, true); err != nil {
		return fmt.Errorf("annual_reference_supply: %w", err)
	}
	return p.DistributionWeights.Validate()
}

func (w DistributionWeights) Validate() error {
	total := uint64(w.ValidatorRewardBps) + uint64(w.TreasuryBps) + uint64(w.ProtectionBps) + uint64(w.BurnBps) + uint64(w.EcosystemBps)
	if total != uint64(BasisPoints) {
		return fmt.Errorf("distribution weights must sum to %d bps", BasisPoints)
	}
	return nil
}

func (e EmissionEpoch) Validate(params Params) error {
	if e.Epoch == 0 {
		return fmt.Errorf("epoch must be positive")
	}
	if e.StakingRatioBps > BasisPoints {
		return fmt.Errorf("staking_ratio_bps cannot exceed %d", BasisPoints)
	}
	if e.InflationBps > params.ConstitutionalMaxInflationBps {
		return fmt.Errorf("inflation cannot exceed constitutional maximum")
	}
	for _, entry := range []struct {
		name string
		coin sdk.Coin
	}{
		{name: "emission_amount", coin: e.EmissionAmount},
		{name: "validator_reward", coin: e.ValidatorReward},
		{name: "treasury", coin: e.Treasury},
		{name: "protection_fund", coin: e.ProtectionFund},
		{name: "burn", coin: e.Burn},
		{name: "ecosystem", coin: e.Ecosystem},
		{name: "rounding_remainder", coin: e.RoundingRemainder},
	} {
		if err := validateCoin(params.BaseDenom, entry.coin, false); err != nil {
			return fmt.Errorf("%s: %w", entry.name, err)
		}
	}
	sum := e.ValidatorReward.Amount.Add(e.Treasury.Amount).Add(e.ProtectionFund.Amount).Add(e.Burn.Amount).Add(e.Ecosystem.Amount).Add(e.RoundingRemainder.Amount)
	if !sum.Equal(e.EmissionAmount.Amount) {
		return fmt.Errorf("emission amount %s does not match distribution accounting %s", e.EmissionAmount, sum.String())
	}
	return nil
}

func (gs GenesisState) Validate() error {
	params := NormalizeParams(gs.Params)
	if err := params.Validate(); err != nil {
		return err
	}
	if err := validateCoin(params.BaseDenom, gs.TotalMintedAccounting, false); err != nil {
		return fmt.Errorf("total_minted_accounting: %w", err)
	}
	seen := map[uint64]struct{}{}
	total := sdkmath.ZeroInt()
	for _, epoch := range gs.EpochHistory {
		if _, ok := seen[epoch.Epoch]; ok {
			return fmt.Errorf("duplicate emission epoch %d", epoch.Epoch)
		}
		seen[epoch.Epoch] = struct{}{}
		if err := epoch.Validate(params); err != nil {
			return err
		}
		total = total.Add(epoch.EmissionAmount.Amount)
	}
	if !total.Equal(gs.TotalMintedAccounting.Amount) {
		return fmt.Errorf("minted accounting %s does not match epoch total %s", gs.TotalMintedAccounting.Amount, total)
	}
	return nil
}

func SortEmissionEpochs(in []EmissionEpoch) []EmissionEpoch {
	out := append([]EmissionEpoch(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Epoch < out[j].Epoch })
	return out
}

func ComputeInflationBps(params Params, stakingRatioBps uint32) uint32 {
	current := int64(params.CurrentInflationBps)
	delta := int64(params.TargetStakingRatioBps) - int64(stakingRatioBps)
	adjustment := delta * int64(params.ResponsivenessBps) / int64(BasisPoints)
	next := current + adjustment
	if next < int64(params.MinAnnualInflationBps) {
		next = int64(params.MinAnnualInflationBps)
	}
	if next > int64(params.MaxAnnualInflationBps) {
		next = int64(params.MaxAnnualInflationBps)
	}
	if next > int64(params.ConstitutionalMaxInflationBps) {
		next = int64(params.ConstitutionalMaxInflationBps)
	}
	return uint32(next)
}

// ComputeEpochEmission applies this epoch's inflation to params.AnnualReferenceSupply.
//
// That param is a genesis BOOTSTRAP anchor, not the live supply. A running
// chain must use ComputeEpochEmissionWithSupply so inflation is a rate on the
// real circulating supply rather than a fixed amount.
func ComputeEpochEmission(params Params, epoch, stakingRatioBps uint64, height int64) (EmissionEpoch, error) {
	return ComputeEpochEmissionWithSupply(params, epoch, stakingRatioBps, height, params.AnnualReferenceSupply.Amount)
}

// ComputeEpochEmissionWithSupply is ComputeEpochEmission against a caller-supplied
// supply anchor. It stays a pure function: the caller reads the anchor from the
// bank keeper and passes it in, so the same value can drive both the
// mint-authority cap pre-check and the committed record.
func ComputeEpochEmissionWithSupply(params Params, epoch, stakingRatioBps uint64, height int64, referenceSupply sdkmath.Int) (EmissionEpoch, error) {
	if stakingRatioBps > uint64(BasisPoints) {
		return EmissionEpoch{}, ErrInvalidEpoch.Wrap("staking_ratio_bps cannot exceed basis points")
	}
	if referenceSupply.IsNil() || !referenceSupply.IsPositive() {
		referenceSupply = params.AnnualReferenceSupply.Amount
	}
	inflationBps := ComputeInflationBps(params, uint32(stakingRatioBps))
	annual := referenceSupply.MulRaw(int64(inflationBps)).QuoRaw(int64(BasisPoints))
	amount := annual.QuoRaw(int64(params.EpochsPerYear))
	emission := sdk.NewCoin(params.BaseDenom, amount)
	epochRecord := EmissionEpoch{
		Epoch:			epoch,
		StakingRatioBps:	uint32(stakingRatioBps),
		InflationBps:		inflationBps,
		EmissionAmount:		emission,
		ValidatorReward:	sdk.NewCoin(params.BaseDenom, bpsAmount(amount, params.DistributionWeights.ValidatorRewardBps)),
		Treasury:		sdk.NewCoin(params.BaseDenom, bpsAmount(amount, params.DistributionWeights.TreasuryBps)),
		ProtectionFund:		sdk.NewCoin(params.BaseDenom, bpsAmount(amount, params.DistributionWeights.ProtectionBps)),
		Burn:			sdk.NewCoin(params.BaseDenom, bpsAmount(amount, params.DistributionWeights.BurnBps)),
		Ecosystem:		sdk.NewCoin(params.BaseDenom, bpsAmount(amount, params.DistributionWeights.EcosystemBps)),
		FinalizedHeight:	height,
	}
	distributed := epochRecord.ValidatorReward.Amount.Add(epochRecord.Treasury.Amount).Add(epochRecord.ProtectionFund.Amount).Add(epochRecord.Burn.Amount).Add(epochRecord.Ecosystem.Amount)
	epochRecord.RoundingRemainder = sdk.NewCoin(params.BaseDenom, amount.Sub(distributed))
	if err := epochRecord.Validate(params); err != nil {
		return EmissionEpoch{}, ErrInvalidEpoch.Wrap(err.Error())
	}
	return epochRecord, nil
}

func validateCoin(denom string, coin sdk.Coin, allowPositive bool) error {
	if coin.Denom != denom {
		return fmt.Errorf("denom must be %s", denom)
	}
	if coin.Amount.IsNil() || coin.Amount.IsNegative() {
		return fmt.Errorf("amount cannot be negative")
	}
	if allowPositive && coin.Amount.IsZero() {
		return fmt.Errorf("amount must be positive")
	}
	return nil
}

func bpsAmount(amount sdkmath.Int, bps uint32) sdkmath.Int {
	if amount.IsZero() || bps == 0 {
		return sdkmath.ZeroInt()
	}
	return amount.MulRaw(int64(bps)).QuoRaw(int64(BasisPoints))
}
