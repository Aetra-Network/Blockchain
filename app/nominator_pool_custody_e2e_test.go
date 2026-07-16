package app

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
	nominatorpoolkeeper "github.com/sovereign-l1/l1/x/nominator-pool/keeper"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// TestNominatorPoolCustodyEndToEndDepositDelegatesAndWithdrawalPaysOutReal
// live-verifies the plain-pool custody flow this session wired with real
// bank+staking+distribution keepers: DepositToPool must actually move real
// naet out of the delegator's spendable balance into a real x/staking
// delegation on the pool's own module account (not a ledger-only entry),
// and once both x/staking's own unbonding and the pool's separate
// UnbondingBlocks window mature, RequestPoolWithdrawal's settlement must
// actually pay the delegator back from the pool module account's real bank
// balance.
//
// Broadcasting this exact flow on a live 4-node testnet (see
// THIRD-AUDIT-REPORT.md) confirmed the deposit->delegate leg but could not
// observe the withdrawal payout: both x/staking's unbonding_time and this
// module's own UnbondingBlocks default to 14-21 days
// (appparams.ValidateStakingUnbondingBlocks rejects anything shorter, even
// in a genesis built only for testing), so waiting for real maturity on a
// live network is impractical. Advancing ctx's block time/height directly
// and calling both EndBlockers once is the same technique x/staking's own
// test suite uses for the identical problem.
func TestNominatorPoolCustodyEndToEndDepositDelegatesAndWithdrawalPaysOutReal(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	validator := GetBondedTestValidator(t, app, ctx)
	valAddr, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
	require.NoError(t, err)

	poolGenesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	authority := poolGenesis.Params.Authority

	const funded = int64(100_000_000_000) // 100 AET
	delegator := AddTestAddrsIncremental(app, ctx, 1, sdkmath.NewInt(funded))[0]
	beforeDeposit := app.BankKeeper.GetBalance(ctx, delegator, appparams.BaseDenom)
	require.Equal(t, funded, beforeDeposit.Amount.Int64())

	srv := nominatorpoolkeeper.NewMsgServerImpl(&app.NominatorPoolKeeper)

	requestHeight := uint64(ctx.BlockHeight())
	if requestHeight == 0 {
		requestHeight = 1
	}
	poolID := "custody-e2e-pool"
	_, err = srv.CreateNominatorPool(ctx, &nominatorpooltypes.MsgCreateNominatorPool{
		Authority:		authority,
		PoolID:			poolID,
		PoolOperator:		authority,
		ValidatorTarget:	validator.GetOperator(),
		PoolCommissionBps:	500,
		Height:			requestHeight,
		ValidatorStatus:	"active",
	})
	require.NoError(t, err)

	const depositAmount = uint64(50_000_000_000) // 50 AET
	_, err = srv.DepositToPool(ctx, &nominatorpooltypes.MsgDepositToPool{
		Authority:	authority,
		PoolID:		poolID,
		Delegator:	delegator.String(),
		Amount:		depositAmount,
		Height:		requestHeight,
	})
	require.NoError(t, err)

	// The deposit must have actually left the delegator's real spendable
	// balance -- not just incremented a ledger number with no bank effect.
	afterDeposit := app.BankKeeper.GetBalance(ctx, delegator, appparams.BaseDenom)
	require.Equal(t, beforeDeposit.Amount.SubRaw(int64(depositAmount)), afterDeposit.Amount)

	// ... and it must have landed as a REAL x/staking delegation from the
	// pool's own module account to the target validator, worth exactly the
	// deposited amount of underlying tokens. This test's genesis validator
	// (built by simtestutil.GenesisStateWithValSet) is bonded with a
	// 1,000,000:1 token/share exchange rate, not 1:1, so the assertion goes
	// through TokensFromShares rather than comparing Shares directly.
	poolModuleAddr := nominatorpoolkeeper.PoolModuleAddress()
	delegation, err := app.StakingKeeper.GetDelegation(ctx, poolModuleAddr, sdk.ValAddress(valAddr))
	require.NoError(t, err)
	validatorAfterDeposit, err := app.StakingKeeper.GetValidator(ctx, sdk.ValAddress(valAddr))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewIntFromUint64(depositAmount), validatorAfterDeposit.TokensFromShares(delegation.Shares).TruncateInt())

	const withdrawalID = "custody-e2e-wd-1"
	_, err = srv.RequestPoolWithdrawal(ctx, &nominatorpooltypes.MsgRequestPoolWithdrawal{
		Authority:	authority,
		PoolID:		poolID,
		WithdrawalID:	withdrawalID,
		Delegator:	delegator.String(),
		Shares:		depositAmount,
		Height:		requestHeight,
	})
	require.NoError(t, err)

	// The real x/staking delegation must be gone (undelegated), replaced by
	// a real unbonding delegation entry -- confirming withdrawalCustody
	// actually called x/staking's Undelegate rather than only touching the
	// pool's own ledger.
	_, err = app.StakingKeeper.GetDelegation(ctx, poolModuleAddr, sdk.ValAddress(valAddr))
	require.Error(t, err)
	ubd, err := app.StakingKeeper.GetUnbondingDelegation(ctx, poolModuleAddr, sdk.ValAddress(valAddr))
	require.NoError(t, err)
	require.Len(t, ubd.Entries, 1)

	poolAfterWithdrawalRequest, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	var completeHeight uint64
	for _, pool := range poolAfterWithdrawalRequest.State.Pools {
		if pool.PoolID != poolID {
			continue
		}
		for _, wd := range pool.PendingWithdrawals {
			if wd.WithdrawalID == withdrawalID {
				completeHeight = wd.CompleteHeight
			}
		}
	}
	require.NotZero(t, completeHeight)

	unbondingTime, err := app.StakingKeeper.UnbondingTime(ctx)
	require.NoError(t, err)

	// Fast-forward both clocks the two independent maturity gates read:
	// x/staking's own unbonding completion keys off BlockTime, the pool's
	// settlement keys off BlockHeight.
	futureCtx := ctx.WithBlockHeight(int64(completeHeight)).WithBlockTime(ctx.BlockTime().Add(unbondingTime + time.Minute))

	_, err = app.StakingKeeper.EndBlocker(futureCtx)
	require.NoError(t, err)

	require.NoError(t, app.NominatorPoolKeeper.EndBlocker(futureCtx))

	afterWithdrawal := app.BankKeeper.GetBalance(futureCtx, delegator, appparams.BaseDenom)
	require.Equal(t, beforeDeposit.Amount, afterWithdrawal.Amount, "delegator must get their real principal back once both maturity windows pass")

	poolFinal, err := app.NominatorPoolKeeper.ExportGenesisState(futureCtx)
	require.NoError(t, err)
	var settledStatus string
	for _, pool := range poolFinal.State.Pools {
		if pool.PoolID != poolID {
			continue
		}
		for _, wd := range pool.PendingWithdrawals {
			if wd.WithdrawalID == withdrawalID {
				settledStatus = wd.Status
			}
		}
	}
	require.Equal(t, nominatorpooltypes.WithdrawalStatusCompleted, settledStatus)
}
