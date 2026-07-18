package keeperwiring

import (
	"context"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"

	aetraeconomicskeeper "github.com/sovereign-l1/l1/x/aetra-economics/keeper"
	aetraeconomicstypes "github.com/sovereign-l1/l1/x/aetra-economics/types"
	aetrastakingpolicykeeper "github.com/sovereign-l1/l1/x/aetra-staking-policy/keeper"
	aetrastakingpolicytypes "github.com/sovereign-l1/l1/x/aetra-staking-policy/types"
	aetravalidatorscorekeeper "github.com/sovereign-l1/l1/x/aetra-validator-score/keeper"
	aetravalidatorscoretypes "github.com/sovereign-l1/l1/x/aetra-validator-score/types"
	burnkeeper "github.com/sovereign-l1/l1/x/burn/keeper"
	burntypes "github.com/sovereign-l1/l1/x/burn/types"
	delegatorprotectionkeeper "github.com/sovereign-l1/l1/x/delegator-protection/keeper"
	delegatorprotectiontypes "github.com/sovereign-l1/l1/x/delegator-protection/types"
	dynamiccommissionkeeper "github.com/sovereign-l1/l1/x/dynamic-commission/keeper"
	dynamiccommissiontypes "github.com/sovereign-l1/l1/x/dynamic-commission/types"
	emissionskeeper "github.com/sovereign-l1/l1/x/emissions/keeper"
	emissionstypes "github.com/sovereign-l1/l1/x/emissions/types"
	feecollectorkeeper "github.com/sovereign-l1/l1/x/fee-collector/keeper"
	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
	feeskeeper "github.com/sovereign-l1/l1/x/fees/keeper"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	mintauthoritykeeper "github.com/sovereign-l1/l1/x/mint-authority/keeper"
	mintauthoritytypes "github.com/sovereign-l1/l1/x/mint-authority/types"
	performancekeeper "github.com/sovereign-l1/l1/x/performance/keeper"
	performancetypes "github.com/sovereign-l1/l1/x/performance/types"
	reputationkeeper "github.com/sovereign-l1/l1/x/reputation/keeper"
	reputationtypes "github.com/sovereign-l1/l1/x/reputation/types"
	stakeconcentrationkeeper "github.com/sovereign-l1/l1/x/stake-concentration/keeper"
	stakeconcentrationtypes "github.com/sovereign-l1/l1/x/stake-concentration/types"
	treasurykeeper "github.com/sovereign-l1/l1/x/treasury/keeper"
	treasurytypes "github.com/sovereign-l1/l1/x/treasury/types"
)

// identityReputationScorer reads a raw identity reputation score. The reputation
// module keeper satisfies it; keeping it an interface makes the gate below
// unit-testable without a live store.
type identityReputationScorer interface {
	GetIdentityReputationScore(ctx context.Context, addr sdk.AccAddress) (uint32, bool, error)
}

// DomainOwnershipReader reports whether an account currently holds an attached
// domain -- the ANS Phase B reputation fee gate. x/identity-root's keeper
// satisfies it via AccountHoldsDomain (an O(1) committed-store read).
type DomainOwnershipReader interface {
	AccountHoldsDomain(ctx context.Context, addr sdk.AccAddress) (bool, error)
}

// ValidatorPresenceReader reports whether an account is a validator operator --
// the other half of the reputation fee gate. An x/staking adapter satisfies it.
type ValidatorPresenceReader interface {
	IsValidator(ctx context.Context, addr sdk.AccAddress) bool
}

// reputationReaderAdapter wraps the reputation module keeper as a
// feestypes.ReputationReader, GATED (ANS Phase B, owner's rule) on CURRENT
// domain ownership or validator status. GetIdentityReputationScore returns
// found=false (neutral, no fee scaling) UNLESS the account currently holds a
// domain OR is a validator; only then does the multiplicative reputation fee
// engage. A plain wallet's fee is thus unaffected by whatever reputation record
// it may happen to have. Reputation itself still lives on the native account in
// x/reputation -- this adapter only decides whether it is allowed to move a fee.
type reputationReaderAdapter struct {
	scorer		identityReputationScorer
	domainReader	DomainOwnershipReader
	validatorReader	ValidatorPresenceReader
}

func (a reputationReaderAdapter) GetIdentityReputationScore(ctx context.Context, addr sdk.AccAddress) (uint32, bool, error) {
	score, _, err := a.scorer.GetIdentityReputationScore(ctx, addr)
	if err != nil {
		// Degrade to neutral: a reputation read error must never block a tx.
		return score, false, nil
	}
	// Gate. A domain holder or a validator engages reputation scaling; anyone
	// else is neutral regardless of their score. Read errors degrade to false.
	gated := false
	if a.domainReader != nil {
		if holds, herr := a.domainReader.AccountHoldsDomain(ctx, addr); herr == nil && holds {
			gated = true
		}
	}
	if !gated && a.validatorReader != nil && a.validatorReader.IsValidator(ctx, addr) {
		gated = true
	}
	if !gated {
		return score, false, nil
	}
	// Gated: engage scaling. found=true even when the underlying record is
	// absent (a freshly-attached wallet, whose score defaults to the reputation
	// module's IdentityScoreDefault) so a domain grants the multiplier
	// immediately rather than only after other reputation activity.
	return score, true, nil
}

// validatorReputationAdapter wraps the reputation keeper as a dynamiccommissiontypes.ReputationKeeper.
type validatorReputationAdapter struct {
	Keeper reputationkeeper.Keeper
}

func (a validatorReputationAdapter) GetValidatorTotalScore(ctx context.Context, addr string) (uint32, bool, error) {
	vs, err := a.Keeper.GetValidatorReputation(ctx, addr)
	if err != nil {
		return 0, false, err
	}
	if vs == nil {
		return 0, false, nil
	}
	return vs.TotalScore, vs.IsJailed || vs.IsSlashed, nil
}

type NativeKeeperDeps struct {
	AppCodec	codec.Codec
	Keys		map[string]*storetypes.KVStoreKey
	AccountKeeper	authkeeper.AccountKeeper
	BankKeeper	bankkeeper.BaseKeeper
	DistrKeeper	distrkeeper.Keeper
	GovAuthority	string
	// DomainOwnershipReader / ValidatorPresenceReader gate the reputation fee
	// (ANS Phase B). Both optional: a nil reader is treated as "not gated" so a
	// keeper set built without them behaves as pre-Phase-B neutral reputation.
	DomainOwnershipReader	DomainOwnershipReader
	ValidatorPresenceReader	ValidatorPresenceReader
}

type NativeKeepers struct {
	BurnKeeper			burnkeeper.Keeper
	TreasuryKeeper			treasurykeeper.Keeper
	EmissionsKeeper			emissionskeeper.Keeper
	MintAuthorityKeeper		mintauthoritykeeper.Keeper
	DelegatorProtectionKeeper	delegatorprotectionkeeper.Keeper
	ReputationKeeper		reputationkeeper.Keeper
	PerformanceKeeper		performancekeeper.Keeper
	DynamicCommissionKeeper		dynamiccommissionkeeper.Keeper
	StakeConcentrationKeeper	stakeconcentrationkeeper.Keeper
	FeeCollectorKeeper		feecollectorkeeper.Keeper
	FeesKeeper			feeskeeper.Keeper
	AetraStakingPolicyKeeper	aetrastakingpolicykeeper.Keeper
	AetraEconomicsKeeper		aetraeconomicskeeper.Keeper
	AetraValidatorScoreKeeper	aetravalidatorscorekeeper.Keeper
}

func NewNativeKeepers(deps NativeKeeperDeps) NativeKeepers {
	repKeeper := reputationkeeper.NewKeeper(
		runtime.NewKVStoreService(deps.Keys[reputationtypes.StoreKey]),
		deps.GovAuthority,
	)
	fcKeeper := feecollectorkeeper.NewKeeper(
		deps.AppCodec,
		runtime.NewKVStoreService(deps.Keys[feecollectortypes.StoreKey]),
		deps.AccountKeeper,
		deps.BankKeeper,
		deps.GovAuthority,
	)
	return NativeKeepers{
		BurnKeeper: burnkeeper.NewKeeper(
			deps.AppCodec,
			runtime.NewKVStoreService(deps.Keys[burntypes.StoreKey]),
			deps.BankKeeper,
			deps.GovAuthority,
		),
		TreasuryKeeper: treasurykeeper.NewKeeper(
			deps.AppCodec,
			runtime.NewKVStoreService(deps.Keys[treasurytypes.StoreKey]),
			deps.AccountKeeper,
			deps.BankKeeper,
			deps.GovAuthority,
		),
		EmissionsKeeper: emissionskeeper.NewKeeper(
			deps.AppCodec,
			runtime.NewKVStoreService(deps.Keys[emissionstypes.StoreKey]),
			deps.GovAuthority,
		),
		MintAuthorityKeeper: mintauthoritykeeper.NewKeeper(
			runtime.NewKVStoreService(deps.Keys[mintauthoritytypes.StoreKey]),
			deps.BankKeeper,
			deps.GovAuthority,
		),
		DelegatorProtectionKeeper: delegatorprotectionkeeper.NewKeeper(
			runtime.NewKVStoreService(deps.Keys[delegatorprotectiontypes.StoreKey]),
			deps.GovAuthority,
		),
		ReputationKeeper:	repKeeper,
		PerformanceKeeper: performancekeeper.NewKeeper(
			runtime.NewKVStoreService(deps.Keys[performancetypes.StoreKey]),
			deps.GovAuthority,
		),
		DynamicCommissionKeeper: dynamiccommissionkeeper.NewKeeper(
			deps.AppCodec,
			runtime.NewKVStoreService(deps.Keys[dynamiccommissiontypes.StoreKey]),
			deps.GovAuthority,
		).WithReputationKeeper(validatorReputationAdapter{Keeper: repKeeper}),
		StakeConcentrationKeeper: stakeconcentrationkeeper.NewKeeper(
			deps.AppCodec,
			runtime.NewKVStoreService(deps.Keys[stakeconcentrationtypes.StoreKey]),
			deps.GovAuthority,
		),
		FeeCollectorKeeper:	fcKeeper,
		FeesKeeper: feeskeeper.NewKeeper(
			deps.AppCodec,
			runtime.NewKVStoreService(deps.Keys[feestypes.StoreKey]),
			deps.AccountKeeper,
			deps.BankKeeper,
			deps.DistrKeeper,
			deps.GovAuthority,
		).WithReputationReader(reputationReaderAdapter{
			scorer:			repKeeper,
			domainReader:		deps.DomainOwnershipReader,
			validatorReader:	deps.ValidatorPresenceReader,
		}).WithFeeCollector(fcKeeper),
		AetraStakingPolicyKeeper:	aetrastakingpolicykeeper.NewPersistentKeeper(runtime.NewKVStoreService(deps.Keys[aetrastakingpolicytypes.StoreKey]), deps.GovAuthority),
		AetraEconomicsKeeper:		aetraeconomicskeeper.NewPersistentKeeper(runtime.NewKVStoreService(deps.Keys[aetraeconomicstypes.StoreKey]), deps.GovAuthority),
		AetraValidatorScoreKeeper:	aetravalidatorscorekeeper.NewPersistentKeeper(runtime.NewKVStoreService(deps.Keys[aetravalidatorscoretypes.StoreKey]), deps.GovAuthority),
	}
}
