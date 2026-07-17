package keeper

import (
	"context"
)

// BeginBlocker swaps a pending routing table into the active one at its
// ActivationHeight.
//
// BEGIN, not END. The placement is not a preference -- an EndBlocker here would
// make ActivationHeight mean something other than what it says:
//
//   - BeginBlock at height H runs before any transaction at H
//     (app/block_lifecycle.go). A table stamped ActivationHeight = H is
//     therefore the table every transaction at H resolves against, and all of
//     block H sees exactly ONE table. That is the property
//     SetPendingRoutingTable's "strictly in the future" rule exists to protect.
//   - EndBlock at H runs after every transaction at H. A table stamped
//     ActivationHeight = H would first be observable by transactions at H+1, so
//     the committed activation height would be off by one from the height the
//     table actually takes effect -- a lie the epoch-boundary guard would then
//     validate.
//
// The ordering neighbours confirm it. app/wiring/aetracore/order.go's
// BeginBlockerOrder already lists aez AFTER config/config-voting/aetracore/load/
// routing and BEFORE mesh, payments, the schedulers, actor-registry, contracts,
// storage-rent and identity-root -- i.e. the table is swapped before any module
// that could plausibly consult it runs. No change to order.go was needed. In
// EndBlockerOrder aez sits after gov and staking, so a table swapped there would
// first be seen by the NEXT block's BeginBlockers, which run before that block's
// aez EndBlocker: an inverted, harder-to-audit ordering.
//
// Interaction with governance: gov proposals execute in gov's EndBlocker, so a
// MsgUpdateRoutingTable passing at height H writes the pending table at EndBlock
// H, and the earliest BeginBlock that can activate it is H+1. That is consistent
// with the "strictly future, exact epoch boundary" staging rule -- the two
// cannot collide.
//
// Determinism: MaybeActivatePendingRoutingTable reads only committed store
// values and ctx.BlockHeight(). No wall clock, no randomness, no map iteration
// (I-22). With no pending table it is one store read and a nil return, so a
// chain that never touches the routing table pays a single Get per block and can
// never fail one (I-23).
func (k Keeper) BeginBlocker(ctx context.Context) error {
	_, err := k.MaybeActivatePendingRoutingTable(ctx)
	return err
}
