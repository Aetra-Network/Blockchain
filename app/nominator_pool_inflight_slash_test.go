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

// requirePoolWithdrawal reads one PendingWithdrawal straight out of committed
// module state, so these tests assert on what the chain actually stored rather
// than on a receipt.
func requirePoolWithdrawal(t *testing.T, app *L1App, ctx sdk.Context, poolID, withdrawalID string) nominatorpooltypes.PendingWithdrawal {
	t.Helper()

	genesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	for _, pool := range genesis.State.Pools {
		if pool.PoolID != poolID {
			continue
		}
		for _, withdrawal := range pool.PendingWithdrawals {
			if withdrawal.WithdrawalID == withdrawalID {
				return withdrawal
			}
		}
	}
	require.FailNowf(t, "withdrawal not found", "pool %q withdrawal %q", poolID, withdrawalID)
	return nominatorpooltypes.PendingWithdrawal{}
}

// requirePool reads one NominatorPool straight out of committed module state.
func requirePool(t *testing.T, app *L1App, ctx sdk.Context, poolID string) nominatorpooltypes.NominatorPool {
	t.Helper()

	genesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	for _, pool := range genesis.State.Pools {
		if pool.PoolID == poolID {
			return pool
		}
	}
	require.FailNowf(t, "pool not found", "pool %q", poolID)
	return nominatorpooltypes.NominatorPool{}
}

// setupInflightSlashPool builds a pool targeting the genesis validator and
// deposits depositAmount into it from a freshly funded delegator, returning the
// delegator, the validator, and the delegator's post-deposit balance.
func setupInflightSlashPool(t *testing.T, app *L1App, ctx sdk.Context, poolID string, ledgerHeight, depositAmount uint64) (sdk.AccAddress, stakingtypes.Validator, sdkmath.Int) {
	t.Helper()

	validator := GetBondedTestValidator(t, app, ctx)

	genesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	authority := genesis.Params.Authority

	const funded = int64(100_000_000_000) // 100 AET
	delegator := AddTestAddrsIncremental(app, ctx, 1, sdkmath.NewInt(funded))[0]

	srv := nominatorpoolkeeper.NewMsgServerImpl(&app.NominatorPoolKeeper)
	_, err = srv.CreateNominatorPool(ctx, &nominatorpooltypes.MsgCreateNominatorPool{
		Authority:         authority,
		PoolID:            poolID,
		PoolOperator:      authority,
		ValidatorTarget:   validator.GetOperator(),
		PoolCommissionBps: 500,
		Height:            ledgerHeight,
		ValidatorStatus:   "active",
	})
	require.NoError(t, err)

	_, err = srv.DepositToPool(ctx, &nominatorpooltypes.MsgDepositToPool{
		Authority: authority,
		PoolID:    poolID,
		Delegator: delegator.String(),
		Amount:    depositAmount,
		Height:    ledgerHeight,
	})
	require.NoError(t, err)

	afterDeposit := app.BankKeeper.GetBalance(ctx, delegator, appparams.BaseDenom)
	require.Equal(t, funded-int64(depositAmount), afterDeposit.Amount.Int64())
	return delegator, validator, afterDeposit.Amount
}

// TestNominatorPoolWithdrawalSettlesAfterInFlightSlash is the F-1 regression.
//
// Shares are burned when the unbond starts, BEFORE the principal comes back.
// x/staking slashes an in-flight unbonding entry for infractions committed
// before that unbond began -- that is the entire point of the unbonding period
// -- so after such a slash strictly LESS than the pool's frozen
// withdrawal.Amount claim ever arrives.
//
// Settlement used to be all-or-nothing (`if !spendable.IsAllGTE(coins) { return
// false, nil }`), so it could never succeed against a short balance: the
// EndBlocker's `if !paid { continue }` retried forever while the depositor's
// shares were already gone. A routine ~0.01% downtime slash stranded 100% of
// the principal.
//
// Against the pre-fix keeper this test fails at the final assertions: the
// delegator's balance delta is 0 and the withdrawal is still Pending, forever.
func TestNominatorPoolWithdrawalSettlesAfterInFlightSlash(t *testing.T) {
	app := Setup(t, false)

	const unbondHeight = uint64(10)
	ctx := app.NewContext(false).WithBlockHeight(int64(unbondHeight)).WithBlockTime(time.Now().UTC())

	const poolID = "inflight-slash-pool"
	const depositAmount = uint64(50_000_000_000) // 50 AET
	delegator, validator, afterDeposit := setupInflightSlashPool(t, app, ctx, poolID, unbondHeight, depositAmount)

	valAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
	require.NoError(t, err)
	consBytes, err := validator.GetConsAddr()
	require.NoError(t, err)
	consAddr := sdk.ConsAddress(consBytes)

	srv := nominatorpoolkeeper.NewMsgServerImpl(&app.NominatorPoolKeeper)
	genesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)

	const withdrawalID = "inflight-slash-wd"
	_, err = srv.RequestPoolWithdrawal(ctx, &nominatorpooltypes.MsgRequestPoolWithdrawal{
		Authority:    genesis.Params.Authority,
		PoolID:       poolID,
		WithdrawalID: withdrawalID,
		Delegator:    delegator.String(),
		Shares:       depositAmount,
		Height:       unbondHeight,
	})
	require.NoError(t, err)

	poolModuleAddr := nominatorpoolkeeper.PoolModuleAddress()
	ubd, err := app.StakingKeeper.GetUnbondingDelegation(ctx, poolModuleAddr, sdk.ValAddress(valAddr))
	require.NoError(t, err)
	require.Len(t, ubd.Entries, 1)
	require.Equal(t, int64(unbondHeight), ubd.Entries[0].CreationHeight)

	withdrawal := requirePoolWithdrawal(t, app, ctx, poolID, withdrawalID)
	require.Equal(t, unbondHeight, withdrawal.UnbondHeight, "the withdrawal must record the REAL x/staking creation height")
	// Snapshotted, not read back off the pool: ChangePoolValidator can rewrite
	// the pool's target without redelegating, and settlement must keep looking
	// at the validator that actually holds this withdrawal's money.
	require.NotEmpty(t, withdrawal.UnbondValidator, "the withdrawal must record which validator it is unbonding from")
	require.Equal(t, requirePool(t, app, ctx, poolID).ValidatorTarget, withdrawal.UnbondValidator)

	// Slash for an infraction committed AT the height the unbond began. Both of
	// x/staking's gates are then satisfied: Slash only scans unbonding
	// delegations when infractionHeight < current height, and
	// SlashUnbondingDelegation skips entries with CreationHeight <
	// infractionHeight. This is precisely the "infraction committed before the
	// unbond began" case the unbonding period exists to catch.
	//
	// Slash's return value is deliberately NOT asserted on: it reports the
	// power-derived slash budget left over after the unbonding entries took
	// their cut, which is zero here. What an entry actually loses is
	// slashFactor*entry.InitialBalance, computed inside SlashUnbondingDelegation
	// independently of `power`. The balance comparison below is the real proof.
	slashCtx := ctx.WithBlockHeight(int64(unbondHeight) + 1)
	power := validator.ConsensusPower(sdk.DefaultPowerReduction)
	_, err = app.StakingKeeper.Slash(slashCtx, consAddr, int64(unbondHeight), power, sdkmath.LegacyNewDecWithPrec(1, 2))
	require.NoError(t, err)

	ubd, err = app.StakingKeeper.GetUnbondingDelegation(slashCtx, poolModuleAddr, sdk.ValAddress(valAddr))
	require.NoError(t, err)
	require.Len(t, ubd.Entries, 1)
	require.True(t, ubd.Entries[0].Balance.LT(ubd.Entries[0].InitialBalance), "x/staking must really have slashed the IN-FLIGHT unbonding entry")

	postSlashBalance := ubd.Entries[0].Balance
	require.True(t, postSlashBalance.IsPositive(), "the slash must be partial, otherwise this proves nothing about a short payout")
	// This is F-1's trigger: what x/staking will hand back is strictly less
	// than the pool's frozen claim, so an all-or-nothing settle can never pass.
	require.True(t, postSlashBalance.LT(sdkmath.NewIntFromUint64(withdrawal.Amount)))

	unbondingTime, err := app.StakingKeeper.UnbondingTime(ctx)
	require.NoError(t, err)
	futureCtx := ctx.WithBlockHeight(int64(withdrawal.CompleteHeight)).WithBlockTime(ctx.BlockTime().Add(unbondingTime + time.Minute))

	poolBalanceBefore := app.BankKeeper.GetBalance(futureCtx, poolModuleAddr, appparams.BaseDenom)
	_, err = app.StakingKeeper.EndBlocker(futureCtx)
	require.NoError(t, err)
	poolBalanceAfter := app.BankKeeper.GetBalance(futureCtx, poolModuleAddr, appparams.BaseDenom)
	require.Equal(t, postSlashBalance, poolBalanceAfter.Amount.Sub(poolBalanceBefore.Amount),
		"x/staking credits the pool the POST-slash balance, never the pool's pre-slash claim")

	require.NoError(t, app.NominatorPoolKeeper.EndBlocker(futureCtx))

	// The whole point: the depositor is paid what actually came back. Their
	// shares are burned either way, so leaving this at 0 is a total loss of
	// principal for a partial slash.
	afterWithdrawal := app.BankKeeper.GetBalance(futureCtx, delegator, appparams.BaseDenom)
	require.Equal(t, postSlashBalance, afterWithdrawal.Amount.Sub(afterDeposit),
		"depositor must actually be paid the post-slash proceeds, not stranded")

	settled := requirePoolWithdrawal(t, app, futureCtx, poolID, withdrawalID)
	require.Equal(t, nominatorpooltypes.WithdrawalStatusCompleted, settled.Status,
		"the withdrawal must reach a terminal status instead of retrying forever")
	require.Equal(t, postSlashBalance, sdkmath.NewIntFromUint64(settled.SettledAmount),
		"SettledAmount must record what was really paid")
	require.Less(t, settled.SettledAmount, settled.Amount,
		"Amount stays the pre-slash claim -- overwriting it with 0 would fail Validate and halt the EndBlocker")
}

// TestNominatorPoolWithdrawalCohortDoesNotStealLaterProceeds pins the FIFO
// hazard, and it is the test that rules out the tempting one-line "fix" of
// paying min(amount, spendable).
//
// SortWithdrawals orders PendingWithdrawals by WithdrawalID LEXICOGRAPHICALLY,
// which is arbitrary with respect to maturity. So the IDs here are chosen to
// defeat it: "wd-aaa" unbonds LATER than "wd-zzz" but iterates FIRST. Only
// wd-zzz's money has actually come back from x/staking, while both are matured
// by the pool's own block-height clock.
//
// Against the pre-fix keeper this fails immediately: settlement walks wd-aaa
// first, finds the pool's spendable balance (which is wd-zzz's money) covers
// wd-aaa's claim in full, and pays wd-aaa out of it -- leaving wd-zzz, whose
// coins those were, Pending with an empty account. That is a live theft path
// that needs no slash at all.
func TestNominatorPoolWithdrawalCohortDoesNotStealLaterProceeds(t *testing.T) {
	app := Setup(t, false)

	const firstHeight = uint64(10)
	const secondHeight = uint64(11)
	baseTime := time.Now().UTC()
	ctx := app.NewContext(false).WithBlockHeight(int64(firstHeight)).WithBlockTime(baseTime)

	const poolID = "cohort-fifo-pool"
	const depositAmount = uint64(50_000_000_000) // 50 AET
	const withdrawEach = uint64(25_000_000_000)  // 25 AET each, so either claim is exactly covered by the other's proceeds
	delegator, validator, afterDeposit := setupInflightSlashPool(t, app, ctx, poolID, firstHeight, depositAmount)

	valAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
	require.NoError(t, err)

	genesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	authority := genesis.Params.Authority
	srv := nominatorpoolkeeper.NewMsgServerImpl(&app.NominatorPoolKeeper)

	// Sorts LAST, unbonds FIRST -- its proceeds are the ones on the table.
	const earlyID = "wd-zzz"
	_, err = srv.RequestPoolWithdrawal(ctx, &nominatorpooltypes.MsgRequestPoolWithdrawal{
		Authority:    authority,
		PoolID:       poolID,
		WithdrawalID: earlyID,
		Delegator:    delegator.String(),
		Shares:       withdrawEach,
		Height:       firstHeight,
	})
	require.NoError(t, err)

	// Sorts FIRST, unbonds LATER (a block later, and an hour later on the
	// wall clock x/staking actually keys completion off).
	laterCtx := ctx.WithBlockHeight(int64(secondHeight)).WithBlockTime(baseTime.Add(time.Hour))
	const lateID = "wd-aaa"
	_, err = srv.RequestPoolWithdrawal(laterCtx, &nominatorpooltypes.MsgRequestPoolWithdrawal{
		Authority:    authority,
		PoolID:       poolID,
		WithdrawalID: lateID,
		Delegator:    delegator.String(),
		Shares:       withdrawEach,
		Height:       secondHeight,
	})
	require.NoError(t, err)

	poolModuleAddr := nominatorpoolkeeper.PoolModuleAddress()
	ubd, err := app.StakingKeeper.GetUnbondingDelegation(laterCtx, poolModuleAddr, sdk.ValAddress(valAddr))
	require.NoError(t, err)
	require.Len(t, ubd.Entries, 2, "different creation heights must NOT merge into one entry")

	early := requirePoolWithdrawal(t, app, laterCtx, poolID, earlyID)
	late := requirePoolWithdrawal(t, app, laterCtx, poolID, lateID)
	require.Equal(t, firstHeight, early.UnbondHeight)
	require.Equal(t, secondHeight, late.UnbondHeight)
	require.Equal(t, withdrawEach, early.Amount)
	require.Equal(t, withdrawEach, late.Amount)

	unbondingTime, err := app.StakingKeeper.UnbondingTime(ctx)
	require.NoError(t, err)

	// Height is past BOTH withdrawals' CompleteHeight, so both are matured by
	// the pool's clock and both are in the settlement cohort. But the wall
	// clock has only passed wd-zzz's staking completion time, so only wd-zzz's
	// coins have physically arrived.
	partialCtx := ctx.WithBlockHeight(int64(late.CompleteHeight)).WithBlockTime(baseTime.Add(unbondingTime + time.Minute))
	require.GreaterOrEqual(t, uint64(partialCtx.BlockHeight()), early.CompleteHeight)
	require.GreaterOrEqual(t, uint64(partialCtx.BlockHeight()), late.CompleteHeight)

	_, err = app.StakingKeeper.EndBlocker(partialCtx)
	require.NoError(t, err)

	ubd, err = app.StakingKeeper.GetUnbondingDelegation(partialCtx, poolModuleAddr, sdk.ValAddress(valAddr))
	require.NoError(t, err)
	require.Len(t, ubd.Entries, 1, "only wd-zzz's entry should have completed")
	require.Equal(t, int64(secondHeight), ubd.Entries[0].CreationHeight, "wd-aaa's money is still in flight")

	poolFloat := app.BankKeeper.GetBalance(partialCtx, poolModuleAddr, appparams.BaseDenom)
	require.Equal(t, sdkmath.NewIntFromUint64(withdrawEach), poolFloat.Amount,
		"the pool holds exactly wd-zzz's proceeds -- enough to cover wd-aaa's claim in full, which is the trap")

	require.NoError(t, app.NominatorPoolKeeper.EndBlocker(partialCtx))

	// wd-aaa must NOT be paid: its own unbonding is still in flight, so any
	// coin it received would be wd-zzz's.
	late = requirePoolWithdrawal(t, app, partialCtx, poolID, lateID)
	require.Equal(t, nominatorpooltypes.WithdrawalStatusPending, late.Status,
		"wd-aaa must not be settled out of wd-zzz's proceeds")
	require.Zero(t, late.SettledAmount)
	require.Equal(t, afterDeposit, app.BankKeeper.GetBalance(partialCtx, delegator, appparams.BaseDenom).Amount,
		"nothing may be paid out while any of the cohort's own money is still unbonding")

	// ... and once wd-aaa's money really does arrive, BOTH settle in full. The
	// gate delays settlement, it never deadlocks it.
	finalCtx := ctx.WithBlockHeight(int64(late.CompleteHeight)).WithBlockTime(baseTime.Add(time.Hour + unbondingTime + time.Minute))
	_, err = app.StakingKeeper.EndBlocker(finalCtx)
	require.NoError(t, err)
	require.NoError(t, app.NominatorPoolKeeper.EndBlocker(finalCtx))

	early = requirePoolWithdrawal(t, app, finalCtx, poolID, earlyID)
	late = requirePoolWithdrawal(t, app, finalCtx, poolID, lateID)
	require.Equal(t, nominatorpooltypes.WithdrawalStatusCompleted, early.Status)
	require.Equal(t, nominatorpooltypes.WithdrawalStatusCompleted, late.Status)
	require.Equal(t, withdrawEach, early.SettledAmount)
	require.Equal(t, withdrawEach, late.SettledAmount)
	require.Equal(t, afterDeposit.AddRaw(int64(2*withdrawEach)), app.BankKeeper.GetBalance(finalCtx, delegator, appparams.BaseDenom).Amount,
		"both withdrawals must be paid in full once nothing is in flight")
}
