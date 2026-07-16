package app

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
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

// TestNominatorPoolCustodyEndToEndOfficialPoolDepositWithdrawsAndPaysOutReal is
// the official-liquid-staking twin of the plain-pool test above, and it drives
// the two messages an ordinary wallet actually sends: MsgDepositToStakingPool
// and MsgRequestPoolUnbond.
//
// This is the path that carries real user money. Direct x/staking delegation is
// disabled for users (app/stakingpolicy/msg_server.go), so MsgDepositToStakingPool
// is the ONLY way a wallet's coins become real stake -- which makes its reverse
// leg exactly as load-bearing as the deposit.
//
// Both legs were exempted from custody while the official deposit was
// ledger-only. Once the deposit started really delegating, that exemption became
// a fund-loss bug rather than a scoping decision: the unbond decremented the
// depositor's shares and TotalBondedStake and queued a PendingWithdrawal, but
// never called x/staking's Undelegate -- so the coins stayed delegated,
// settleWithdrawal found a spendable balance of 0, EndBlocker's `if !paid`
// skipped it every block, and the withdrawal stayed Pending forever. Shares
// gone, coins locked, no payout. This test is what fails if either leg is
// exempted again: the final balance assertion is the whole point.
func TestNominatorPoolCustodyEndToEndOfficialPoolDepositWithdrawsAndPaysOutReal(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	validator := GetBondedTestValidator(t, app, ctx)
	valAddr := parseValidatorAddress(t, app, validator.OperatorAddress)

	poolGenesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	authority := poolGenesis.Params.Authority

	// The wallet signs with (and carries in wallet_address/owner_address) its
	// PLAIN address; msgServer resolves it to the account's v2 identity before
	// any keeper bookkeeping. That identity is the balance the deposit is really
	// collected from and the withdrawal is really paid back to, so it is what
	// gets funded and asserted against here.
	userAddress, _ := nominatorPoolAddressPair(t, "51")
	identityUser, _ := normalizeToV2AccountIdentity(t, userAddress)
	depositor, err := addressing.ParseAccAddress(identityUser)
	require.NoError(t, err)

	const depositAmount = 2 * nominatorpooltypes.DefaultMinPoolDeposit
	FundTestAddr(t, app, ctx, depositor, sdk.NewCoins(sdk.NewCoin(appparams.BaseDenom, sdkmath.NewIntFromUint64(4*depositAmount))))
	beforeDeposit := app.BankKeeper.GetBalance(ctx, depositor, appparams.BaseDenom)

	srv := nominatorpoolkeeper.NewMsgServerImpl(&app.NominatorPoolKeeper)
	contractUser, contractRaw := nominatorPoolAddressPair(t, "52")
	poolID := "custody-e2e-official-pool"

	_, err = srv.CreateOfficialLiquidStakingPool(ctx, &nominatorpooltypes.MsgCreateOfficialLiquidStakingPool{
		Authority:		authority,
		PoolID:			poolID,
		ContractAddressUser:	contractUser,
		ContractAddressRaw:	contractRaw,
		PoolOperator:		nominatorPoolRawAddress("53"),
		PoolCommissionBps:	100,
		Height:			2,
		ValidatorTarget:	validator.OperatorAddress,
	})
	require.NoError(t, err)

	_, err = srv.DepositToStakingPool(ctx, &nominatorpooltypes.MsgDepositToStakingPool{
		PoolID:		poolID,
		WalletAddress:	userAddress,
		Amount:		depositAmount,
		Height:		3,
	})
	require.NoError(t, err)

	// The deposit must really have left the wallet...
	require.Equal(t, beforeDeposit.Amount.SubRaw(int64(depositAmount)), app.BankKeeper.GetBalance(ctx, depositor, appparams.BaseDenom).Amount,
		"the deposit must actually leave the depositor's spendable balance")

	// ... and become a REAL x/staking delegation held by the pool's own module
	// account, worth exactly the deposit. (Genesis validators bond at a
	// 1,000,000:1 token/share rate, hence TokensFromShares.)
	poolModuleAddr := nominatorpoolkeeper.PoolModuleAddress()
	delegation, err := app.StakingKeeper.GetDelegation(ctx, poolModuleAddr, valAddr)
	require.NoError(t, err, "the pool must hold a real delegation for the deposit it credited")
	validatorAfterDeposit, err := app.StakingKeeper.GetValidator(ctx, valAddr)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewIntFromUint64(depositAmount), validatorAfterDeposit.TokensFromShares(delegation.Shares).TruncateInt(),
		"the deposit must become real bonded stake, not a ledger entry")

	// Unbond every share, so a correct payout restores the original balance
	// exactly and any shortfall shows up as a plain number mismatch.
	const unbondID = "custody-e2e-official-wd-1"
	_, err = srv.RequestPoolUnbond(ctx, &nominatorpooltypes.MsgRequestPoolUnbond{
		PoolID:		poolID,
		OwnerAddress:	userAddress,
		RequestID:	unbondID,
		Shares:		depositAmount,
		Height:		4,
	})
	require.NoError(t, err)

	// The real delegation must be gone, replaced by a real unbonding entry --
	// this is the assertion that fails if the official pool is exempted from
	// withdrawalCustody again.
	_, err = app.StakingKeeper.GetDelegation(ctx, poolModuleAddr, valAddr)
	require.Error(t, err, "the unbond must really undelegate, not just drop ledger shares")
	ubd, err := app.StakingKeeper.GetUnbondingDelegation(ctx, poolModuleAddr, valAddr)
	require.NoError(t, err, "the unbond must start a real x/staking unbonding")
	require.Len(t, ubd.Entries, 1)

	afterUnbondRequest, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	var completeHeight uint64
	for _, pool := range afterUnbondRequest.State.Pools {
		if pool.PoolID != poolID {
			continue
		}
		for _, wd := range pool.PendingWithdrawals {
			if wd.WithdrawalID == unbondID {
				completeHeight = wd.CompleteHeight
			}
		}
	}
	require.NotZero(t, completeHeight)

	unbondingTime, err := app.StakingKeeper.UnbondingTime(ctx)
	require.NoError(t, err)

	// Fast-forward both clocks the two independent maturity gates read:
	// x/staking's unbonding completion keys off BlockTime, the pool's own
	// settlement keys off BlockHeight.
	futureCtx := ctx.WithBlockHeight(int64(completeHeight)).WithBlockTime(ctx.BlockTime().Add(unbondingTime + time.Minute))

	_, err = app.StakingKeeper.EndBlocker(futureCtx)
	require.NoError(t, err)
	require.NoError(t, app.NominatorPoolKeeper.EndBlocker(futureCtx))

	// The point of the whole exercise: the depositor's real coins came back.
	require.Equal(t, beforeDeposit.Amount, app.BankKeeper.GetBalance(futureCtx, depositor, appparams.BaseDenom).Amount,
		"the official-pool depositor must get their real principal back once both maturity windows pass")

	poolFinal, err := app.NominatorPoolKeeper.ExportGenesisState(futureCtx)
	require.NoError(t, err)
	var settled string
	for _, pool := range poolFinal.State.Pools {
		if pool.PoolID != poolID {
			continue
		}
		for _, wd := range pool.PendingWithdrawals {
			if wd.WithdrawalID == unbondID {
				settled = wd.Status
			}
		}
	}
	require.Equal(t, nominatorpooltypes.WithdrawalStatusCompleted, settled,
		"the withdrawal must settle, not sit Pending forever behind a pool balance that never arrives")
}
