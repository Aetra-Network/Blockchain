package upgrades

import (
	"context"

	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
)

// AEZKeeper is the minimal structural interface FixupAEZMessageQuota needs:
// exactly the two methods x/aez/keeper.Keeper already exposes (GetParams/
// SetParams). It is declared here, not imported from x/aez/keeper, so this
// package does not need to depend on the keeper package just to call two
// methods it already implements -- the same reason app/upgrades/
// native_account.go's helpers take a plan value rather than a keeper handle.
type AEZKeeper interface {
	GetParams(ctx context.Context) (aeztypes.Params, error)
	SetParams(ctx context.Context, params aeztypes.Params) error
}

// FixupAEZMessageQuota is the Layer-1 migration-safety helper from
// docs/architecture/aez-throughput-preservation-design.md §5.4: a ready,
// exported, unit-tested fixup for the hazard of an upgraded binary reading
// back an old-shape x/aez Params blob whose MessageQuota field unmarshals to
// its Go zero value (TotalMessageGasPerBlock: 0, Quotas: nil) rather than
// erroring, because json.Unmarshal does not fail on a missing field.
//
// It reads committed Params; if MessageQuota already validates, it does
// nothing (fixed=false, err=nil) -- this call is always safe to make even on
// an already-upgraded chain. If MessageQuota fails Validate(), it replaces it
// with types.DefaultMessageQuotaParams() (the spec split: Core reserves
// 4,000,000, each elastic zone caps at 1,000,000) and commits the result
// through SetParams, which re-validates the whole Params struct before
// writing.
//
// This is NOT wired into the live "v053-to-v054" SetUpgradeHandler closure in
// app/upgrades/upgrades.go. That closure is an explicit reference
// implementation for an unrelated Cosmos SDK version bump (its own doc
// comment says so), and -- per the design doc's Review 1 Finding 3, confirmed
// by grepping the tree -- the only production call to x/aez.Keeper.SetParams
// today is InitGenesisState: x/aez ships no params-update Msg at all, so
// there is no live upgrade plan for this helper to hang off of yet. A real
// future named upgrade plan (one that ships its own
// SetUpgradeHandler(Name, ...), e.g. because it also touches store keys or
// module versions) calls this from its own handler, at that plan's own
// boundary -- exactly the convention native_account.go already established:
// NativeAccountVersionUpgradePlan/ValidateNativeAccountVersionUpgradeHandler
// are likewise exported, unit-tested, and not force-wired into
// "v053-to-v054" either.
//
// x/aez/keeper/drain.go's drainLegacyGlobalBudget (design doc §5.4 Layer 2)
// is the unconditional defense-in-depth that does not depend on this
// function ever running: DrainWith degrades to the exact pre-Phase-6b
// algorithm on its own, forever, whether or not any upgrade plan ever calls
// FixupAEZMessageQuota. This helper only shortens how long a chain spends on
// that fallback before governance/an upgrade plan restores the intended
// per-zone split.
func FixupAEZMessageQuota(ctx context.Context, k AEZKeeper) (fixed bool, err error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return false, err
	}
	if params.MessageQuota.Validate() == nil {
		return false, nil
	}
	params.MessageQuota = aeztypes.DefaultMessageQuotaParams()
	if err := k.SetParams(ctx, params); err != nil {
		return false, err
	}
	return true, nil
}
