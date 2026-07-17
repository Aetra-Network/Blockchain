package app

import (
	"encoding/hex"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
	nominatorpoolkeeper "github.com/sovereign-l1/l1/x/nominator-pool/keeper"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// This file measures what a nominator-pool message actually costs, as a
// function of how many depositors and how many pools already exist. It exists
// because every previous estimate of that cost was wrong: the module's gas was
// assumed to be x/staking Delegate (it is ~95k and flat, under 10%), then
// assumed to be fixed by ef198d13 (it moved the wall from ~7 depositors to
// ~195, it did not remove it). The rule these tests encode is that the shape of
// the cost curve is a measurement, never an argument.
//
// The numbers below were measured on this commit; see the report in each test.
// They are asserted with margin so the tests guard against REGRESSION without
// pinning digits that legitimately drift by a byte or two.

// maxTxGas is the ante's hard ceiling. A message that costs more than this
// cannot be executed at any gas limit: under the cap it dies in FinalizeBlock,
// over it the ante rejects the tx. Every "wall" in this file is the depositor
// count at which a fitted cost line crosses it.
const maxTxGas = float64(feestypes.DefaultMaxTxGas)

// gasScalingWallet mints a distinct, fundable plain wallet address for index i.
// 20 bytes, because a depositor must be a real bank account.
func gasScalingWallet(t *testing.T, idx int) (string, sdk.AccAddress) {
	t.Helper()
	bz, err := hex.DecodeString(fmt.Sprintf("%040x", 0xD0000000+idx))
	require.NoError(t, err)
	acc := sdk.AccAddress(bz)
	return addressing.FormatAccAddress(acc), acc
}

// fitLine returns the least-squares slope and intercept of the sample points.
func fitLine(xs []float64, ys []float64) (slope, intercept float64) {
	n := float64(len(xs))
	var meanX, meanY float64
	for i := range xs {
		meanX += xs[i] / n
		meanY += ys[i] / n
	}
	var num, den float64
	for i := range xs {
		num += (xs[i] - meanX) * (ys[i] - meanY)
		den += (xs[i] - meanX) * (xs[i] - meanX)
	}
	slope = num / den
	return slope, meanY - slope*meanX
}

// depositGasFixture stands up a real app with one official liquid staking pool
// and N distinct depositors already in it, then reports what the NEXT message
// costs in real metered store gas.
type depositGasFixture struct {
	app    *L1App
	ctx    sdk.Context
	srv    nominatorpooltypes.MsgServer
	poolID string
	height uint64
}

func newDepositGasFixture(t *testing.T, poolID string, existing int) *depositGasFixture {
	t.Helper()
	app := Setup(t, false)
	ctx := app.NewContext(false)
	validator := GetBondedTestValidator(t, app, ctx)

	poolGenesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)

	srv := nominatorpoolkeeper.NewMsgServerImpl(&app.NominatorPoolKeeper)
	contractUser, contractRaw := nominatorPoolAddressPair(t, "52")
	_, err = srv.CreateOfficialLiquidStakingPool(ctx, &nominatorpooltypes.MsgCreateOfficialLiquidStakingPool{
		Authority:           poolGenesis.Params.Authority,
		PoolID:              poolID,
		ContractAddressUser: contractUser,
		ContractAddressRaw:  contractRaw,
		PoolOperator:        nominatorPoolRawAddress("53"),
		PoolCommissionBps:   100,
		Height:              2,
		ValidatorTarget:     validator.OperatorAddress,
	})
	require.NoError(t, err)

	f := &depositGasFixture{app: app, ctx: ctx, srv: srv, poolID: poolID, height: 3}
	for i := 0; i < existing; i++ {
		f.depositAs(t, f.fund(t, i), poolID)
	}
	return f
}

func (f *depositGasFixture) authority(t *testing.T) string {
	t.Helper()
	poolGenesis, err := f.app.NominatorPoolKeeper.ExportGenesisState(f.ctx)
	require.NoError(t, err)
	return poolGenesis.Params.Authority
}

// fund gives wallet idx enough to deposit many times over, and activates it so
// the pool's activation gate (D3) lets it in. Both are always done OUTSIDE a
// measured window so the number reported is the message's.
//
// The activation record itself is NOT free at deposit time: ensureActiveWallet
// reads it on every gated message. That read is one account record, flat in the
// number of depositors, so it moves the intercept and not the slope -- which is
// what the fits below are about. It is included in every measurement here
// precisely because a real deposit pays it.
func (f *depositGasFixture) fund(t *testing.T, idx int) string {
	t.Helper()
	user, acc := gasScalingWallet(t, idx)
	FundTestAddr(t, f.app, f.ctx, acc, sdk.NewCoins(sdk.NewCoin(
		appparams.BaseDenom, sdkmath.NewIntFromUint64(20*nominatorpooltypes.DefaultMinPoolDeposit))))
	if _, found, err := f.app.NativeAccountKeeper.AccountByUser(f.ctx, poolWalletIdentity(t, user).User); err == nil && !found {
		activatePoolWalletAE(t, f.app, f.ctx, user, uint64(100_000+idx), nativeaccounttypes.AccountStatusActive)
	}
	return user
}

func (f *depositGasFixture) depositAs(t *testing.T, user string, poolID string) {
	t.Helper()
	_, err := f.srv.DepositToStakingPool(f.ctx, &nominatorpooltypes.MsgDepositToStakingPool{
		PoolID:        poolID,
		WalletAddress: user,
		Amount:        nominatorpooltypes.DefaultMinPoolDeposit,
		Height:        f.height,
	})
	require.NoError(t, err)
	f.height++
}

// measureDeposit runs one deposit under its own finite gas meter and returns
// the gas that deposit alone consumed.
func (f *depositGasFixture) measureDeposit(t *testing.T, idx int) uint64 {
	t.Helper()
	user := f.fund(t, idx)
	metered := f.ctx.WithGasMeter(storetypes.NewGasMeter(500_000_000))
	before := metered.GasMeter().GasConsumed()
	_, err := f.srv.DepositToStakingPool(metered, &nominatorpooltypes.MsgDepositToStakingPool{
		PoolID:        f.poolID,
		WalletAddress: user,
		Amount:        nominatorpooltypes.DefaultMinPoolDeposit,
		Height:        f.height,
	})
	require.NoError(t, err)
	f.height++
	return metered.GasMeter().GasConsumed() - before
}

// rawStore returns the module's substore WITHOUT gas metering, so inspecting
// record sizes does not perturb the numbers being explained.
func (f *depositGasFixture) rawStore(t *testing.T) storetypes.KVStore {
	t.Helper()
	return f.ctx.MultiStore().GetKVStore(f.app.keys[nominatorpooltypes.StoreKey])
}

// TestDepositGasSlopePerDepositor measures a deposit's cost against the number
// of depositors already in the pool, and states the wall that follows.
//
// Measured on this commit (existing depositors -> gas):
//
//	  0 ->   228,896
//	  9 ->   270,234
//	 49 ->   426,942
//	 99 ->   623,169
//	150 ->   819,498
//
// Least squares over the last four: gas = 234,813 + 3,923.5 * depositors, with
// a maximum residual of 123 gas (0.02%) -- the cost is linear to measurement
// precision, so the wall is arithmetic, not extrapolation:
//
//	(1,000,000 - 234,813) / 3,923.5 = 195
//
// i.e. the 196th wallet cannot join a pool at any gas limit. Params today allow
// MaxDelegators = 1,000,000 per pool (types/state.go), which over-states the
// real capacity by a factor of ~5,100.
func TestDepositGasSlopePerDepositor(t *testing.T) {
	if testing.Short() {
		t.Skip("gas scaling measurement is slow")
	}
	counts := []int{0, 9, 49, 99, 149}
	xs, ys := []float64{}, []float64{}
	for _, existing := range counts {
		f := newDepositGasFixture(t, "gas-scaling-pool", existing)
		gas := f.measureDeposit(t, 100_000+existing)
		t.Logf("MEASURE existingDepositors=%d depositGas=%d", existing, gas)
		if existing == 0 {
			// The empty pool is off the line: with no share records there is
			// no prefix scan to charge for at all. Fitting it in would flatter
			// the slope.
			continue
		}
		xs = append(xs, float64(existing))
		ys = append(ys, float64(gas))
	}
	slope, intercept := fitLine(xs, ys)
	wall := (maxTxGas - intercept) / slope
	t.Logf("FIT depositGas = %.0f + %.1f * depositors ; wall at %.0f depositors", intercept, slope, wall)

	// Guard the slope rather than the digits: this is the term that decides
	// how many people can ever use a pool, and it must never grow again.
	require.Less(t, slope, 4_300.0,
		"per-depositor gas slope regressed: every depositor already pays for every other depositor "+
			"in the pool on every deposit, and a bigger slope means a smaller pool bricks")
	require.Greater(t, wall, 150.0,
		"the depositor count at which deposits stop fitting in a block moved DOWN -- a pool that "+
			"cannot be deposited into cannot be unbonded from either, which traps principal")
}

// TestDepositGasDecomposition attributes the slope to the two terms that make
// it up, so it is explained rather than merely observed.
//
// Measured on this commit:
//
//	depositors   readGas   poolRecordBytes  shareValueBytes  shareKeyBytes  residualStateBytes
//	         1    22,457               836              261             81                 863
//	        10    36,278             1,549            2,619            810                 867
//	        50    97,850             4,709           13,179          4,050                 868
//	       100   174,851             8,661           26,388          8,100                 872
//	       150   252,251            12,611           39,738         12,150                 872
//
// Per additional depositor: the pool record grows 79.0 bytes (one JSON
// DelegatorShare), the share record family grows 265.6 value + 81.0 key bytes,
// and the residual State blob does not grow at all. With the SDK's default
// KVGasConfig (write 30/byte, read 3/byte, IterNextCostFlat 30) that predicts:
//
//	pool record write   79.0 * 30                     = 2,371  (60.4%)
//	pool record read    79.0 * 3 * 2                  =   474  (12.1%)
//	share prefix scan  (265.6 + 81.0) * 3 + 30        = 1,070  (27.3%)
//	                                            total = 3,915  vs 3,923.5 measured
//
// The pool record is read TWICE because gaskv's iterator charges a seek on
// construction AND on the Next() that advances past the first record; a
// one-record scan pays for that record twice. The 8.5 gas/depositor difference
// from the measured slope (0.2%) is unattributed.
//
// The measured read slope (252,251 - 97,850)/100 = 1,544/depositor matches
// 474 + 1,070 exactly, which is what makes the attribution above a fact rather
// than a plausible story.
func TestDepositGasDecomposition(t *testing.T) {
	if testing.Short() {
		t.Skip("gas decomposition measurement is slow")
	}
	xs, reads, residuals := []float64{}, []float64{}, []int{}
	for _, existing := range []int{1, 10, 50, 100, 150} {
		f := newDepositGasFixture(t, "gas-decomp-pool", existing)

		// The read side, isolated: ExportGenesisState is readGenesisState plus
		// Validate, i.e. exactly what loadForBlock does on every message.
		metered := f.ctx.WithGasMeter(storetypes.NewGasMeter(500_000_000))
		before := metered.GasMeter().GasConsumed()
		_, err := f.app.NominatorPoolKeeper.ExportGenesisState(metered)
		require.NoError(t, err)
		readGas := metered.GasMeter().GasConsumed() - before

		store := f.rawStore(t)
		poolBytes := len(store.Get(nominatorpooltypes.PoolKey(f.poolID)))

		shareBytes, shareKeyBytes, shareCount := 0, 0, 0
		iter := storetypes.KVStorePrefixIterator(store, []byte(nominatorpooltypes.PoolShareKeyPrefix))
		for ; iter.Valid(); iter.Next() {
			shareBytes += len(iter.Value())
			shareKeyBytes += len(iter.Key())
			shareCount++
		}
		require.NoError(t, iter.Close())
		require.Equal(t, existing, shareCount)

		residualBytes := len(store.Get([]byte("prefix_genesis/state")))
		require.Positive(t, residualBytes, "the residual State field must exist at the prefixgenesis key")

		t.Logf("DECOMP depositors=%d readGas=%d poolRecordBytes=%d shareValueBytes=%d shareKeyBytes=%d residualStateBytes=%d",
			existing, readGas, poolBytes, shareBytes, shareKeyBytes, residualBytes)

		xs = append(xs, float64(existing))
		reads = append(reads, float64(readGas))
		residuals = append(residuals, residualBytes)
	}
	readSlope, _ := fitLine(xs, reads)
	t.Logf("FIT readGas slope = %.1f gas/depositor", readSlope)

	// The whole point of ef198d13's split was that per-entity records leave the
	// blob. If the residual State starts growing with the depositor count, the
	// split has sprung a leak and every write is amplified again.
	require.Less(t, residuals[len(residuals)-1]-residuals[0], 64,
		"the residual prefixgenesis State blob must not grow with the depositor count -- "+
			"pools and shares are supposed to live in their own records")
}

// TestUnbondGasCeilingIsBelowDepositCeiling measures the EXIT path, which is
// the one that decides whether the gas wall merely turns depositors away or
// locks in the ones already there.
//
// Measured on this commit (depositors -> unbond gas):
//
//	  1 ->  286,971
//	 10 ->  344,488
//	 50 ->  501,211
//	100 ->  697,045
//	150 ->  893,296
//
// gas = 305,233 + 3,919.7 * depositors, so the unbond wall is at
// (1,000,000 - 305,233)/3,919.7 = 177 depositors -- EIGHTEEN BELOW the deposit
// wall at 195. The exit closes before the entrance does: between the 178th and
// the 195th depositor a pool still accepts money that nobody can ever take out.
func TestUnbondGasCeilingIsBelowDepositCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("gas scaling measurement is slow")
	}
	xs, ys := []float64{}, []float64{}
	for _, existing := range []int{10, 50, 100, 150} {
		f := newDepositGasFixture(t, "gas-unbond-pool", existing)
		user, _ := gasScalingWallet(t, 0)
		metered := f.ctx.WithGasMeter(storetypes.NewGasMeter(500_000_000))
		before := metered.GasMeter().GasConsumed()
		_, err := f.srv.RequestPoolUnbond(metered, &nominatorpooltypes.MsgRequestPoolUnbond{
			PoolID:       f.poolID,
			OwnerAddress: user,
			RequestID:    "unbond-gas-1",
			Shares:       nominatorpooltypes.DefaultMinPoolDeposit,
			Height:       f.height,
		})
		require.NoError(t, err)
		gas := metered.GasMeter().GasConsumed() - before
		t.Logf("UNBOND depositors=%d unbondGas=%d", existing, gas)
		xs = append(xs, float64(existing))
		ys = append(ys, float64(gas))
	}
	slope, intercept := fitLine(xs, ys)
	wall := (maxTxGas - intercept) / slope
	t.Logf("FIT unbondGas = %.0f + %.1f * depositors ; wall at %.0f depositors", intercept, slope, wall)

	require.Greater(t, wall, 150.0,
		"the depositor count at which an unbond stops fitting in a block moved DOWN -- past it, "+
			"every depositor's principal is trapped by gas exhaustion alone")
}

// TestDepositGasGrowsWithPoolCountNotJustDepositors probes the OTHER dimension
// of the same read. readGenesisState prefix-scans EVERY pool record in the
// module, not just the one the deposit names, so a pool a depositor has never
// heard of makes their deposit more expensive.
//
// Measured on this commit, depositing into a pool with ONE depositor while N
// EMPTY bystander pools exist (pools -> gas):
//
//	  1 -> 229,592
//	 10 -> 248,108
//	 50 -> 331,628
//	100 -> 436,028
//
// gas = 227,228 + 2,088 * pools, exactly linear. An empty pool that nobody has
// ever deposited into costs every depositor on the chain 2,088 gas per deposit,
// forever. The wall: (1,000,000 - 227,228)/2,088 = 370 pools -- at which point
// NO deposit into ANY pool executes, and every pooled depositor on the chain is
// locked in simultaneously. Params allow MaxPools = 10,000, which over-states
// the real capacity by ~27x.
//
// Pool creation is authority-gated, so this is not an attack -- it is a ceiling
// governance can walk the chain into by approving one pool too many.
func TestDepositGasGrowsWithPoolCountNotJustDepositors(t *testing.T) {
	if testing.Short() {
		t.Skip("gas scaling measurement is slow")
	}
	xs, ys := []float64{}, []float64{}
	for _, pools := range []int{10, 50, 100} {
		f := newDepositGasFixture(t, "gas-poolcount-target", 0)
		validator := GetBondedTestValidator(t, f.app, f.ctx)
		authority := f.authority(t)
		for i := 1; i < pools; i++ {
			_, err := f.srv.CreateNominatorPool(f.ctx, &nominatorpooltypes.MsgCreateNominatorPool{
				Authority:         authority,
				PoolID:            fmt.Sprintf("filler-pool-%05d", i),
				PoolOperator:      nominatorPoolRawAddress("53"),
				ValidatorTarget:   validator.OperatorAddress,
				PoolCommissionBps: 100,
				Height:            2,
				ValidatorStatus:   "active",
			})
			require.NoError(t, err)
		}
		gas := f.measureDeposit(t, 200_000+pools)
		t.Logf("POOLCOUNT pools=%d depositorsInTargetPool=0 depositGas=%d", pools, gas)
		xs = append(xs, float64(pools))
		ys = append(ys, float64(gas))
	}
	slope, intercept := fitLine(xs, ys)
	wall := (maxTxGas - intercept) / slope
	t.Logf("FIT depositGas = %.0f + %.1f * pools ; wall at %.0f pools", intercept, slope, wall)

	require.Less(t, slope, 2_300.0,
		"per-POOL gas slope regressed: an empty bystander pool must not get more expensive for "+
			"every depositor on the chain to ignore")
	require.Greater(t, wall, 300.0,
		"the pool count at which all deposits chain-wide stop fitting in a block moved DOWN")
}

// TestDepositGasCrossTermPoolsTimesDepositors states the consequence of the
// read being GLOBAL: a depositor pays for every depositor on the CHAIN, in
// every pool, not for the pool they are joining.
//
// Measured: a deposit into a pool holding TEN depositors costs 270,234 when
// those ten are the only pooled depositors on the chain, and 596,907 -- 2.2x
// more -- when nine other pools hold ten depositors each. The target pool is
// identical in both cases; the only thing that changed is other people's pools.
//
// That is what makes MaxPools * MaxDelegators (10,000 * 1,000,000 = 10^10) the
// bound that is really being claimed, and it is off by roughly eight orders of
// magnitude.
func TestDepositGasCrossTermPoolsTimesDepositors(t *testing.T) {
	if testing.Short() {
		t.Skip("gas scaling measurement is slow")
	}
	const pools, perPool = 10, 10
	f := newDepositGasFixture(t, "gas-cross-target", perPool)
	validator := GetBondedTestValidator(t, f.app, f.ctx)
	authority := f.authority(t)

	for i := 1; i < pools; i++ {
		fillerID := fmt.Sprintf("cross-filler-%05d", i)
		contractUser, contractAcc := gasScalingWallet(t, 500_000+i)
		_, err := f.srv.CreateOfficialLiquidStakingPool(f.ctx, &nominatorpooltypes.MsgCreateOfficialLiquidStakingPool{
			Authority:           authority,
			PoolID:              fillerID,
			ContractAddressUser: contractUser,
			ContractAddressRaw:  addressing.Format(contractAcc.Bytes()),
			PoolOperator:        nominatorPoolRawAddress("53"),
			PoolCommissionBps:   100,
			Height:              2,
			ValidatorTarget:     validator.OperatorAddress,
		})
		require.NoError(t, err)
		for j := 0; j < perPool; j++ {
			f.depositAs(t, f.fund(t, 600_000+i*1000+j), fillerID)
		}
	}

	gas := f.measureDeposit(t, 700_000)
	t.Logf("CROSS pools=%d perPool=%d totalPooledDepositorsOnChain=%d depositIntoPoolHolding10=%d",
		pools, perPool, pools*perPool, gas)

	require.Less(t, gas, uint64(maxTxGas),
		"ten pools of ten depositors already costs a deposit more than half of MaxTxGas")
}
