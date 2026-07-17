package app

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
	nominatorpoolkeeper "github.com/sovereign-l1/l1/x/nominator-pool/keeper"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// crossPoolFixture is two pools on two different validators, one depositor
// each, both unbonding in the same block. That is the smallest arrangement in
// which "whose money is this" has a wrong answer -- with one pool the shared
// account and the pool's own money are the same thing, which is exactly why
// this bug survived every existing test.
type crossPoolFixture struct {
	app       *L1App
	ctx       sdk.Context
	poolA     string
	poolB     string
	delegator map[string]sdk.AccAddress
	validator map[string]stakingtypes.Validator
	baseline  map[string]sdkmath.Int
	// spare is a funded wallet belonging to no pool, for tests that need a
	// third party (see the donation test). It must come from the fixture's own
	// one allocation -- asking AddTestAddrsIncremental for another address
	// would hand back a depositor's.
	spare sdk.AccAddress
}

const (
	crossPoolUnbondHeight = uint64(10)
	crossPoolDeposit      = uint64(50_000_000_000) // 50 AET
	crossPoolFunded       = int64(100_000_000_000)
)

// newCrossPoolFixture builds pools poolA and poolB on two distinct bonded
// validators, deposits into each from its own wallet, and unbonds both in the
// same block. Pool ids are caller-supplied so the same scenario can be run with
// the names swapped -- see TestNominatorPoolSettlementIsIndependentOfPoolID.
func newCrossPoolFixture(t *testing.T, poolA, poolB string) *crossPoolFixture {
	t.Helper()

	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(int64(crossPoolUnbondHeight)).WithBlockTime(time.Now().UTC())

	validatorA := GetBondedTestValidator(t, app, ctx)
	// A second REAL bonded validator: the whole point is that pool B's money is
	// somewhere pool A's slash cannot reach.
	_, validatorB := createFundedValidator(t, app, ctx, "cross-pool-validator-b", sdk.TokensFromConsensusPower(10, sdk.DefaultPowerReduction))

	// All wallets in ONE call, and index 0 is skipped on purpose.
	// AddTestAddrsIncremental is deterministic and restarts at the same base
	// address on EVERY call, so asking for one address twice hands back the
	// same account twice -- both pools would share a depositor and their
	// payouts would silently add up instead of being told apart, which is the
	// one thing this test exists to do. Index 0 is the address
	// createFundedValidator just took for validator B's operator.
	wallets := AddTestAddrsIncremental(app, ctx, 4, sdkmath.NewInt(crossPoolFunded))
	depositors := map[string]sdk.AccAddress{poolA: wallets[1], poolB: wallets[2]}
	require.NotEqual(t, depositors[poolA], depositors[poolB], "the two pools must have genuinely different depositors")

	genesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	authority := genesis.Params.Authority
	srv := nominatorpoolkeeper.NewMsgServerImpl(&app.NominatorPoolKeeper)

	f := &crossPoolFixture{
		app:       app,
		ctx:       ctx,
		poolA:     poolA,
		poolB:     poolB,
		delegator: map[string]sdk.AccAddress{},
		validator: map[string]stakingtypes.Validator{poolA: validatorA, poolB: validatorB},
		baseline:  map[string]sdkmath.Int{},
		spare:     wallets[3],
	}

	for _, poolID := range []string{poolA, poolB} {
		validator := f.validator[poolID]
		_, err = srv.CreateNominatorPool(ctx, &nominatorpooltypes.MsgCreateNominatorPool{
			Authority:         authority,
			PoolID:            poolID,
			PoolOperator:      authority,
			ValidatorTarget:   validator.GetOperator(),
			PoolCommissionBps: 500,
			Height:            crossPoolUnbondHeight,
			ValidatorStatus:   "active",
		})
		require.NoError(t, err)

		delegator := depositors[poolID]
		f.delegator[poolID] = delegator
		_, err = srv.DepositToPool(ctx, &nominatorpooltypes.MsgDepositToPool{
			Authority: authority,
			PoolID:    poolID,
			Delegator: delegator.String(),
			Amount:    crossPoolDeposit,
			Height:    crossPoolUnbondHeight,
		})
		require.NoError(t, err)
		f.baseline[poolID] = app.BankKeeper.GetBalance(ctx, delegator, appparams.BaseDenom).Amount

		// Both unbond in the SAME block, so neither can be said to be "first".
		_, err = srv.RequestPoolWithdrawal(ctx, &nominatorpooltypes.MsgRequestPoolWithdrawal{
			Authority:    authority,
			PoolID:       poolID,
			WithdrawalID: poolID + "-wd",
			Delegator:    delegator.String(),
			Shares:       crossPoolDeposit,
			Height:       crossPoolUnbondHeight,
		})
		require.NoError(t, err)
	}
	return f
}

// slash applies a partial slash to one pool's validator for an infraction at
// the unbond height, hitting that validator's IN-FLIGHT unbonding entry and
// nothing else. Returns the post-slash balance x/staking will actually deliver.
func (f *crossPoolFixture) slash(t *testing.T, poolID string, fraction sdkmath.LegacyDec) sdkmath.Int {
	t.Helper()

	validator := f.validator[poolID]
	valAddr, err := f.app.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
	require.NoError(t, err)
	consBytes, err := validator.GetConsAddr()
	require.NoError(t, err)

	slashCtx := f.ctx.WithBlockHeight(int64(crossPoolUnbondHeight) + 1)
	_, err = f.app.StakingKeeper.Slash(slashCtx, sdk.ConsAddress(consBytes), int64(crossPoolUnbondHeight),
		validator.ConsensusPower(sdk.DefaultPowerReduction), fraction)
	require.NoError(t, err)

	ubd, err := f.app.StakingKeeper.GetUnbondingDelegation(slashCtx, nominatorpoolkeeper.PoolModuleAddress(), sdk.ValAddress(valAddr))
	require.NoError(t, err)
	require.Len(t, ubd.Entries, 1)
	require.True(t, ubd.Entries[0].Balance.LT(ubd.Entries[0].InitialBalance),
		"x/staking must really have slashed the in-flight entry, or this test proves nothing")

	// Run the pool's EndBlocker for the slash block, because a real chain does:
	// it runs at EVERY height, and this is the block in which the slash became
	// visible. Skipping it would leave the module's snapshot of the entry
	// showing the pre-slash balance and would test a sequence no chain can
	// produce. This is the observation the attribution proof rests on -- an
	// entry completed at EndBlock N provably was not slashed at BeginBlock N
	// (SlashUnbondingDelegation skips mature entries; CompleteUnbonding
	// requires exactly maturity), so the snapshot taken in the block before
	// completion is what x/staking pays.
	require.NoError(t, f.app.NominatorPoolKeeper.EndBlocker(slashCtx))
	return ubd.Entries[0].Balance
}

// mature runs x/staking's EndBlocker (which credits the shared pool account)
// and then the pool's own (which attributes and settles), in the same real
// order app/wiring/aetracore/order.go runs them.
func (f *crossPoolFixture) mature(t *testing.T) sdk.Context {
	t.Helper()

	unbondingTime, err := f.app.StakingKeeper.UnbondingTime(f.ctx)
	require.NoError(t, err)
	poolParams, err := f.app.NominatorPoolKeeper.ExportGenesisState(f.ctx)
	require.NoError(t, err)
	futureCtx := f.ctx.
		WithBlockHeight(int64(crossPoolUnbondHeight + poolParams.Params.UnbondingBlocks)).
		WithBlockTime(f.ctx.BlockTime().Add(unbondingTime + time.Minute))

	_, err = f.app.StakingKeeper.EndBlocker(futureCtx)
	require.NoError(t, err)
	require.NoError(t, f.app.NominatorPoolKeeper.EndBlocker(futureCtx))
	return futureCtx
}

// paid is what a pool's depositor actually received, i.e. their balance delta
// since the deposit settled.
func (f *crossPoolFixture) paid(t *testing.T, ctx sdk.Context, poolID string) sdkmath.Int {
	t.Helper()
	now := f.app.BankKeeper.GetBalance(ctx, f.delegator[poolID], appparams.BaseDenom).Amount
	return now.Sub(f.baseline[poolID])
}

// TestNominatorPoolSettlementCannotSpendAnotherPoolsProceeds is the D2
// regression, and it is the one that moves money.
//
// EVERY pool shares ONE bank account and ONE x/staking delegator identity:
// keeper.PoolModuleAddress() takes no pool argument. Settlement used to compute
//
//	available = min(SpendableCoins(PoolModuleAddress()), expected)
//
// which contains no pool term at all -- every pool read the same number and
// treated the whole shared balance as its own money.
//
// So: slash pool A's validator 50% while A's principal is in flight. x/staking
// delivers 25 AET for A and a full 50 AET for B into the one shared account.
// Pool A settles first (the EndBlocker walks pools in PoolID order), sees 75
// AET, and pays its depositor the FULL 50 AET pre-slash claim -- 25 AET of it
// pool B's principal. Pool B then finds 25 AET left, pays its depositor 25, and
// marks the withdrawal Completed, which is terminal: the EndBlocker only ever
// settles Pending rows, so B's depositor never gets the rest. A slash on a
// validator B never delegated to, on a pool B never joined, is charged to B.
//
// Against the pre-fix tree this test fails with the two balances EXACTLY
// INVERTED -- A paid 50 and B paid 25. That inversion is the proof.
func TestNominatorPoolSettlementCannotSpendAnotherPoolsProceeds(t *testing.T) {
	f := newCrossPoolFixture(t, "aaa-pool", "zzz-pool")

	// Only pool A's validator is slashed. Pool B is a bystander.
	deliveredA := f.slash(t, f.poolA, sdkmath.LegacyNewDecWithPrec(50, 2))
	require.True(t, deliveredA.IsPositive(), "a partial slash, so a short payout is distinguishable from none")
	require.True(t, deliveredA.LT(sdkmath.NewIntFromUint64(crossPoolDeposit)))

	futureCtx := f.mature(t)

	// Pool A's depositor absorbs pool A's slash, in full and alone.
	require.Equal(t, deliveredA, f.paid(t, futureCtx, f.poolA),
		"pool A's depositor must be paid exactly what pool A's own validator returned after the slash")

	// Pool B is untouched. This is the assertion the old code inverted: B's
	// principal was still in the same account, and A took it.
	require.Equal(t, sdkmath.NewIntFromUint64(crossPoolDeposit), f.paid(t, futureCtx, f.poolB),
		"pool B's depositor must be paid in full: pool B was never slashed, and its proceeds are not pool A's to spend")

	settledA := requirePoolWithdrawal(t, f.app, futureCtx, f.poolA, f.poolA+"-wd")
	settledB := requirePoolWithdrawal(t, f.app, futureCtx, f.poolB, f.poolB+"-wd")
	require.Equal(t, nominatorpooltypes.WithdrawalStatusCompleted, settledA.Status)
	require.Equal(t, nominatorpooltypes.WithdrawalStatusCompleted, settledB.Status)
	require.Equal(t, deliveredA.Uint64(), settledA.SettledAmount)
	require.Equal(t, crossPoolDeposit, settledB.SettledAmount,
		"pool B settled its full claim, so nothing of B's was diverted")

	requirePoolSolvency(t, f.app, futureCtx)
}

// TestNominatorPoolSettlementIsIndependentOfPoolID pins the priority half of
// the same bug, and it is the cheapest possible witness for it.
//
// The EndBlocker walks next.State.Pools in order, and types.SortPools orders by
// PoolID ASCENDING. While settlement read the shared balance, that made the
// lexicographically smallest PoolID permanently senior: it got first claim on
// every coin in the account, every block. Pool creation is governance-gated, so
// this was not permissionless -- but a governance proposal choosing a pool's
// NAME should not thereby be choosing who gets paid first out of everyone
// else's money.
//
// The same scenario is run twice with the names swapped. The payouts must be
// identical. Pre-fix they invert, because the slashed pool wins whenever it
// sorts first and loses whenever it does not.
func TestNominatorPoolSettlementIsIndependentOfPoolID(t *testing.T) {
	half := sdkmath.LegacyNewDecWithPrec(50, 2)

	// Slashed pool sorts FIRST.
	first := newCrossPoolFixture(t, "aaa-slashed", "zzz-healthy")
	deliveredFirst := first.slash(t, first.poolA, half)
	ctxFirst := first.mature(t)
	slashedPaidWhenFirst := first.paid(t, ctxFirst, first.poolA)
	healthyPaidWhenFirst := first.paid(t, ctxFirst, first.poolB)

	// Same scenario, slashed pool sorts LAST. Note the slash target follows the
	// pool, not the name: poolB is the slashed one here.
	second := newCrossPoolFixture(t, "aaa-healthy", "zzz-slashed")
	deliveredSecond := second.slash(t, second.poolB, half)
	ctxSecond := second.mature(t)
	slashedPaidWhenLast := second.paid(t, ctxSecond, second.poolB)
	healthyPaidWhenLast := second.paid(t, ctxSecond, second.poolA)

	require.Equal(t, deliveredFirst, deliveredSecond, "the two runs must be the same scenario, only renamed")
	require.Equal(t, slashedPaidWhenFirst, slashedPaidWhenLast,
		"the slashed pool must be paid the same whether its PoolID sorts first or last")
	require.Equal(t, healthyPaidWhenFirst, healthyPaidWhenLast,
		"the healthy pool must be paid the same whether its PoolID sorts first or last")
	require.Equal(t, sdkmath.NewIntFromUint64(crossPoolDeposit), healthyPaidWhenFirst,
		"and the healthy pool is paid in full in both orderings")

	requirePoolSolvency(t, first.app, ctxFirst)
	requirePoolSolvency(t, second.app, ctxSecond)
}

// requirePoolSolvency asserts invariant I-A: the sum of every pool's claim on
// the shared module account never exceeds what that account actually holds.
//
// It is <= and deliberately NOT ==. Three reasons, each real:
//   - app/accounts/module_accounts.go removes the pool address from
//     BlockedAddresses() (required, so x/staking payouts can land), so ANY
//     wallet can bank-send to it at any time. Under == that is a chain-halt
//     button priced at 1naet.
//   - MulDivUint64 truncates on every pro-rata split, leaving dust.
//   - Rewards accrued inside x/distribution but not yet withdrawn are owed to
//     pools but are not in this account.
//
// The gap is unattributed surplus. It belongs to no pool and no pool can spend
// it -- which is the property that makes <= sufficient.
func requirePoolSolvency(t *testing.T, app *L1App, ctx sdk.Context) {
	t.Helper()

	genesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	total := sdkmath.ZeroInt()
	for _, pool := range genesis.State.Pools {
		total = total.Add(sdkmath.NewIntFromUint64(pool.Entitlement))
	}
	spendable := app.BankKeeper.SpendableCoins(ctx, nominatorpoolkeeper.PoolModuleAddress()).AmountOf(appparams.BaseDenom)
	require.True(t, total.LTE(spendable),
		"sum of pool entitlements (%s) must never exceed the shared module account's spendable balance (%s): "+
			"the module would be promising money it does not hold", total, spendable)
}

// TestNominatorPoolEntitlementSurvivesADonation is the other half of invariant
// I-A: a donation must not become spendable, and must not break anything.
//
// The pool module account is deliberately unblocked, so anyone can send to it.
// Under an == invariant a stranger's 1naet halts the chain. Under <= it is
// simply surplus that belongs to no pool: this asserts both pools still settle
// exactly their own proceeds and the donation is still sitting there afterwards,
// unclaimed.
func TestNominatorPoolEntitlementSurvivesADonation(t *testing.T) {
	f := newCrossPoolFixture(t, "aaa-donated", "zzz-donated")

	const donation = int64(7_000_000_000)
	require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, f.spare, nominatorpoolkeeper.PoolModuleAddress(),
		sdk.NewCoins(sdk.NewCoin(appparams.BaseDenom, sdkmath.NewInt(donation)))),
		"the pool module account is intentionally not blocked -- if this send fails, the custody model changed")

	deliveredA := f.slash(t, f.poolA, sdkmath.LegacyNewDecWithPrec(50, 2))
	futureCtx := f.mature(t)

	// The donation is NOT a top-up: the slashed pool is still paid only what
	// its own validator returned, even though 7 AET is sitting right there.
	require.Equal(t, deliveredA, f.paid(t, futureCtx, f.poolA),
		"a donation must not top a slashed cohort back up -- unattributed money is nobody's to spend")
	require.Equal(t, sdkmath.NewIntFromUint64(crossPoolDeposit), f.paid(t, futureCtx, f.poolB))

	requirePoolSolvency(t, f.app, futureCtx)

	// And it is still there, unspent and unattributed.
	remaining := f.app.BankKeeper.SpendableCoins(futureCtx, nominatorpoolkeeper.PoolModuleAddress()).AmountOf(appparams.BaseDenom)
	require.True(t, remaining.GTE(sdkmath.NewInt(donation)),
		"the donation must survive settlement unspent, as surplus no pool has a claim on")
}
