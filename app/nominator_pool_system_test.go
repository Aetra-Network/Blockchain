package app

import (
	"encoding/json"
	"testing"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sims "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
	nominatorpoolkeeper "github.com/sovereign-l1/l1/x/nominator-pool/keeper"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

func TestNominatorPoolSystemModuleWiringAndGenesis(t *testing.T) {
	app, genesis := setup(true, 5)
	_ = genesis

	require.NoError(t, app.ValidateAetraCoreWiringGate())
	require.Contains(t, app.ModuleManager.Modules, nominatorpooltypes.ModuleName)
	require.Contains(t, app.keys, nominatorpooltypes.StoreKey)
	require.Contains(t, genesis, nominatorpooltypes.ModuleName)
	// The pool now custodies real deposits and delegates them to validators
	// directly (previously a bookkeeping-only ledger with no bank custody,
	// #2/SA2-N01) -- it is registered as its own module account custodian.
	require.Contains(t, GetMaccPerms(), nominatorpooltypes.ModuleName)

	var poolGenesis nominatorpoolkeeper.GenesisState
	require.NoError(t, json.Unmarshal(genesis[nominatorpooltypes.ModuleName], &poolGenesis))
	require.NoError(t, poolGenesis.Validate())
}

func TestNominatorPoolStateSurvivesFinalizeBlockRestart(t *testing.T) {
	db := dbm.NewMemDB()
	appOptions := sims.AppOptionsMap{flags.FlagHome: DefaultNodeHome}
	source := NewL1App(log.NewNopLogger(), db, true, appOptions)
	genesis := GenesisStateWithSingleValidator(t, source)
	poolGenesis := nominatorpoolkeeper.DefaultGenesis()
	poolGenesis.State.Pools = []nominatorpooltypes.NominatorPool{{
		PoolID:			"app-pool-1",
		PoolOperator:		nominatorPoolRawAddress("11"),
		ValidatorTarget:	nominatorPoolRawAddress("12"),
		TotalShares:		1_000,
		TotalBondedStake:	1_100,
		RewardIndex:		100 * nominatorpooltypes.IndexScale / 1_000,
		PoolCommissionBps:	100,
		Status:			nominatorpooltypes.PoolStatusActive,
		DelegatorShares: []nominatorpooltypes.DelegatorShare{{
			Delegator:		nominatorPoolRawAddress("22"),
			Shares:			1_000,
			RewardIndexCheckpoint:	0,
		}},
	}}
	poolGenesis.State = poolGenesis.State.Normalize(poolGenesis.Params)
	require.NoError(t, poolGenesis.Validate())
	poolGenesisBytes, err := json.Marshal(poolGenesis)
	require.NoError(t, err)
	genesis[nominatorpooltypes.ModuleName] = poolGenesisBytes
	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)

	_, err = source.InitChain(&abci.RequestInitChain{
		Validators:		[]abci.ValidatorUpdate{},
		ConsensusParams:	sims.DefaultConsensusParams,
		AppStateBytes:		stateBytes,
	})
	require.NoError(t, err)

	_, err = source.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:	1,
		Hash:	source.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = source.Commit()
	require.NoError(t, err)

	restarted := NewL1App(log.NewNopLogger(), db, true, appOptions)
	restartedCtx := restarted.NewUncachedContext(false, cmtproto.Header{Height: restarted.LastBlockHeight()})
	exported, err := restarted.NominatorPoolKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	require.Len(t, exported.State.Pools, 1)
	require.Equal(t, poolGenesis.State.Pools[0].RewardIndex, exported.State.Pools[0].RewardIndex)
}

func TestNominatorPoolRuntimeMutationPersistsToKVStore(t *testing.T) {
	db := dbm.NewMemDB()
	appOptions := sims.AppOptionsMap{flags.FlagHome: DefaultNodeHome}
	source := NewL1App(log.NewNopLogger(), db, true, appOptions)
	genesis := GenesisStateWithSingleValidator(t, source)
	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)

	_, err = source.InitChain(&abci.RequestInitChain{
		Validators:		[]abci.ValidatorUpdate{},
		ConsensusParams:	sims.DefaultConsensusParams,
		AppStateBytes:		stateBytes,
	})
	require.NoError(t, err)

	// Commit genesis before opening the next block's context. NewNextBlockContext
	// re-derives the finalize state from the COMMITTED store, so without this the
	// genesis validator and bank balances InitChain wrote are discarded -- and the
	// deposit below, which now really collects coins and delegates them, would
	// have no validator to delegate to and no coins to collect.
	_, err = source.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:	1,
		Hash:	source.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = source.Commit()
	require.NoError(t, err)

	sourceCtx := source.NewNextBlockContext(cmtproto.Header{Height: 2})
	initial, err := source.NominatorPoolKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	contractUser, contractRaw := nominatorPoolAddressPair(t, "77")

	// The pool delegates what it takes in, so it must target a REAL bonded
	// validator rather than a synthetic address.
	validator := GetBondedTestValidator(t, source, sourceCtx)
	valAddr := parseValidatorAddress(t, source, validator.OperatorAddress)

	// The wallet deposits with -- and is funded at -- its own PLAIN address, the
	// one it signs with and really holds coins at. Share ownership is recorded
	// under that same address, so money and ledger key agree.
	userAddress, _ := nominatorPoolAddressPair(t, "44")
	depositor, err := addressing.ParseAccAddress(userAddress)
	require.NoError(t, err)
	FundTestAddr(t, source, sourceCtx, depositor, sdk.NewCoins(sdk.NewCoin(BondDenom, sdkmath.NewIntFromUint64(4*nominatorpooltypes.DefaultMinPoolDeposit))))
	balanceBefore := source.BankKeeper.GetBalance(sourceCtx, depositor, BondDenom)

	poolID := "runtime-kv-official-pool"
	nominatorPoolMsg(t, source, sourceCtx, &nominatorpooltypes.MsgCreateOfficialLiquidStakingPool{
		Authority:		initial.Params.Authority,
		PoolID:			poolID,
		ContractAddressUser:	contractUser,
		ContractAddressRaw:	contractRaw,
		PoolOperator:		nominatorPoolRawAddress("11"),
		PoolCommissionBps:	100,
		Height:			2,
		ValidatorTarget:	validator.OperatorAddress,
	})
	// D3: the activation gate is live, so the depositor must be activated.
	activatePoolWalletAE(t, source, sourceCtx, userAddress, 4400, nativeaccounttypes.AccountStatusActive)
	nominatorPoolMsg(t, source, sourceCtx, &nominatorpooltypes.MsgDepositToStakingPool{
		PoolID:		poolID,
		WalletAddress:	userAddress,
		Amount:		nominatorpooltypes.DefaultMinPoolDeposit,
		Height:		3,
	})

	// The runtime mutation this test persists must be backed by money that
	// really moved: the depositor's balance must have dropped...
	require.Equal(t, balanceBefore.Amount.SubRaw(int64(nominatorpooltypes.DefaultMinPoolDeposit)), source.BankKeeper.GetBalance(sourceCtx, depositor, BondDenom).Amount,
		"the deposit must actually leave the depositor's spendable balance")

	// ... and a REAL x/staking delegation worth the deposit must now be held by
	// the pool's own module account. (Genesis validators bond at a 1,000,000:1
	// token/share rate, hence TokensFromShares.)
	delegation, err := source.StakingKeeper.GetDelegation(sourceCtx, nominatorpoolkeeper.PoolModuleAddress(), valAddr)
	require.NoError(t, err, "the pool must hold a real delegation for the deposit it credited")
	validatorAfterDeposit, err := source.StakingKeeper.GetValidator(sourceCtx, valAddr)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewIntFromUint64(nominatorpooltypes.DefaultMinPoolDeposit), validatorAfterDeposit.TokensFromShares(delegation.Shares).TruncateInt(),
		"the deposit must become real bonded stake, not a ledger entry")

	source.SimWriteState()
	_, err = source.Commit()
	require.NoError(t, err)

	restarted := NewL1App(log.NewNopLogger(), db, true, appOptions)
	restartedCtx := restarted.NewUncachedContext(false, cmtproto.Header{Height: restarted.LastBlockHeight()})
	exported, err := restarted.NominatorPoolKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	require.Len(t, exported.State.Pools, 1)
	require.Equal(t, poolID, exported.State.Pools[0].PoolID)
	require.Len(t, exported.State.PoolShares, 1)
	require.Equal(t, userAddress, exported.State.PoolShares[0].Owner)
	require.Equal(t, nominatorpooltypes.DefaultMinPoolDeposit, exported.State.PoolShares[0].Shares)

	// The pool's custody is real committed chain state, not this process's
	// memory: the delegation backing those shares must survive the restart too.
	restartedDelegation, err := restarted.StakingKeeper.GetDelegation(restartedCtx, nominatorpoolkeeper.PoolModuleAddress(), valAddr)
	require.NoError(t, err, "the real delegation backing the pool's shares must survive a restart")
	require.Equal(t, delegation.Shares, restartedDelegation.Shares)
}

func TestFinalAppWiringOfficialStakingPoolFlowExportImportRestart(t *testing.T) {
	appOptions := sims.AppOptionsMap{flags.FlagHome: DefaultNodeHome}
	source := NewL1App(log.NewNopLogger(), dbm.NewMemDB(), true, appOptions)

	// This flow reaches real custody: the deposit below collects actual coins and
	// delegates them. That needs a real chain state to run against -- a genesis
	// validator to delegate to and a bank to collect from -- so bring the app up
	// through InitChain and commit it before opening the block context, since
	// NewNextBlockContext re-derives the finalize state from the committed store.
	genesis := GenesisStateWithSingleValidator(t, source)
	stateBytes, err := json.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)
	_, err = source.InitChain(&abci.RequestInitChain{
		Validators:		[]abci.ValidatorUpdate{},
		ConsensusParams:	sims.DefaultConsensusParams,
		AppStateBytes:		stateBytes,
	})
	require.NoError(t, err)
	_, err = source.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:	1,
		Hash:	source.LastCommitID().Hash,
	})
	require.NoError(t, err)
	_, err = source.Commit()
	require.NoError(t, err)

	sourceCtx := source.NewNextBlockContext(cmtproto.Header{Height: 2})

	initial, err := source.NominatorPoolKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	contractUser, contractRaw := nominatorPoolAddressPair(t, "66")

	// The pool delegates what it takes in, so it must target a REAL bonded
	// validator rather than a synthetic address.
	validator := GetBondedTestValidator(t, source, sourceCtx)
	valAddr := parseValidatorAddress(t, source, validator.OperatorAddress)

	// The deposit is signed/carried with, funded at, and recorded under the
	// caller's own PLAIN address -- one address for money and ledger alike.
	userAddress, userRaw := nominatorPoolAddressPair(t, "33")
	depositor, err := addressing.ParseAccAddress(userAddress)
	require.NoError(t, err)
	FundTestAddr(t, source, sourceCtx, depositor, sdk.NewCoins(sdk.NewCoin(BondDenom, sdkmath.NewIntFromUint64(4*nominatorpooltypes.DefaultMinPoolDeposit))))
	balanceBefore := source.BankKeeper.GetBalance(sourceCtx, depositor, BondDenom)

	poolID := "final-app-official-pool"
	nominatorPoolMsg(t, source, sourceCtx, &nominatorpooltypes.MsgCreateOfficialLiquidStakingPool{
		Authority:		initial.Params.Authority,
		PoolID:			poolID,
		ContractAddressUser:	contractUser,
		ContractAddressRaw:	contractRaw,
		PoolOperator:		nominatorPoolRawAddress("11"),
		PoolCommissionBps:	100,
		Height:			2,
		ValidatorTarget:	validator.OperatorAddress,
	})

	// D3: the activation gate is live, so the depositor must be activated.
	activatePoolWalletAE(t, source, sourceCtx, userAddress, 4500, nativeaccounttypes.AccountStatusActive)
	nominatorPoolMsg(t, source, sourceCtx, &nominatorpooltypes.MsgDepositToStakingPool{
		PoolID:		poolID,
		WalletAddress:	userAddress,
		Amount:		nominatorpooltypes.DefaultMinPoolDeposit,
		Height:		3,
	})
	pool, found := source.NominatorPoolKeeper.NominatorPool(poolID)
	require.True(t, found)
	require.True(t, pool.OfficialLiquidStaking)
	require.Equal(t, contractUser, pool.ContractAddressUser)
	require.Equal(t, contractRaw, pool.ContractAddressRaw)
	share, found := source.NominatorPoolKeeper.PoolShare(nominatorpooltypes.QueryPoolShareRequest{PoolID: poolID, Delegator: userRaw})
	require.True(t, found)
	require.Equal(t, nominatorpooltypes.DefaultMinPoolDeposit, share.Share.Shares)

	// The state this test exports and re-imports must describe real custody:
	// the depositor's balance must have actually dropped...
	require.Equal(t, balanceBefore.Amount.SubRaw(int64(nominatorpooltypes.DefaultMinPoolDeposit)), source.BankKeeper.GetBalance(sourceCtx, depositor, BondDenom).Amount,
		"the deposit must actually leave the depositor's spendable balance")

	// ... and the shares above must be backed by a REAL x/staking delegation
	// held by the pool's own module account, worth exactly the deposit.
	delegation, err := source.StakingKeeper.GetDelegation(sourceCtx, nominatorpoolkeeper.PoolModuleAddress(), valAddr)
	require.NoError(t, err, "the pool must hold a real delegation for the deposit it credited")
	validatorAfterDeposit, err := source.StakingKeeper.GetValidator(sourceCtx, valAddr)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewIntFromUint64(nominatorpooltypes.DefaultMinPoolDeposit), validatorAfterDeposit.TokensFromShares(delegation.Shares).TruncateInt(),
		"the deposit must become real bonded stake, not a ledger entry")

	exported, err := source.NominatorPoolKeeper.ExportGenesisState(sourceCtx)
	require.NoError(t, err)
	require.Len(t, exported.State.LiquidStakingPools, 1)
	require.Len(t, exported.State.PoolShares, 1)
	require.Equal(t, userAddress, exported.State.PoolShares[0].Owner)
	require.Equal(t, nominatorpooltypes.DefaultMinPoolDeposit, exported.State.PoolShares[0].Shares)

	restarted := NewL1App(log.NewNopLogger(), dbm.NewMemDB(), true, appOptions)
	restartedCtx := restarted.NewUncachedContext(false, cmtproto.Header{Height: 4})
	require.NoError(t, restarted.NominatorPoolKeeper.InitGenesisState(restartedCtx, exported))
	reexported, err := restarted.NominatorPoolKeeper.ExportGenesisState(restartedCtx)
	require.NoError(t, err)
	require.Equal(t, exported, reexported)
}

func nominatorPoolRawAddress(hexByte string) string {
	return legacyByteRawAddress(hexByte)
}

func nominatorPoolAddressPair(t *testing.T, hexByte string) (string, string) {
	t.Helper()
	raw := nominatorPoolRawAddress(hexByte)
	bz, err := addressing.Parse(raw)
	require.NoError(t, err)
	user := addressing.FormatAccAddress(sdk.AccAddress(bz))
	return user, raw
}

func nominatorPoolMsg(t *testing.T, app *L1App, ctx sdk.Context, msg sdk.Msg) interface{} {
	t.Helper()
	handler := app.MsgServiceRouter().Handler(msg)
	require.NotNil(t, handler)
	res, err := handler(ctx, msg)
	require.NoError(t, err)
	return res
}
