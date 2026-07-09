package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSyncPoolRewardsCreditsRewardsOnceNotTwice is the regression guard for
// SEC-CRIT: pool reward double-credit. A delegator's total entitlement is the
// withdrawal value of their shares (principal, via ShareValue) PLUS their
// claimable reward (via AccruedReward/RewardIndex). Before the fix, net
// rewards were folded into TotalBondedStake as well, so ShareValue also carried
// the reward — paying it a second time and rendering the pool insolvent.
func TestSyncPoolRewardsCreditsRewardsOnceNotTwice(t *testing.T) {
	params := DefaultParams()
	const principal = uint64(1_000)

	pool := NominatorPool{
		PoolID:            "solvency-pool",
		TotalShares:       principal,
		TotalBondedStake:  principal,
		PoolCommissionBps: 0,
		Status:            PoolStatusActive,
	}
	// Single delegator owning 100% of the pool, joined before any rewards.
	delegator := DelegatorShare{
		Delegator:             chat3AEAddress(0x42),
		Shares:                principal,
		RewardIndexCheckpoint: pool.RewardIndex,
	}

	next, summary, err := SyncPoolRewards(params, pool, MsgSyncPoolRewards{
		Authority:          params.Authority,
		PoolID:             pool.PoolID,
		Epoch:              1,
		Height:             10,
		RewardRateBps:      1_000,
		EmissionsAllocated: 1_000_000,
		Allocations: []ValidatorRewardAllocation{{
			Validator:          chat3AEAddress(0x77),
			PoolAllocatedStake: principal,
			ValidatorSelfStake: 500,
			PerformanceBps:     MaxBasisPoints,
			CommissionBps:      500,
		}},
	})
	require.NoError(t, err)

	netReward := summary.PoolUserRewards
	require.Equal(t, uint64(95), netReward)

	// Principal is NOT inflated by the reward: the reward lives only in the index.
	require.Equal(t, principal, next.TotalBondedStake, "TotalBondedStake must stay at principal")
	require.NotZero(t, next.RewardIndex)

	withdrawValue, err := ShareValue(next, delegator.Shares)
	require.NoError(t, err)
	claimable, err := AccruedReward(delegator, next.RewardIndex)
	require.NoError(t, err)

	require.Equal(t, principal, withdrawValue, "share value must equal principal, not principal+reward")
	require.Equal(t, netReward, claimable, "claimable must equal the net reward exactly once")
	// Solvency: total paid out = principal + reward once (not principal + 2*reward).
	require.Equal(t, principal+netReward, withdrawValue+claimable)
}

// TestShareValueAndAccruedRewardDoNotWrapAtLargeMagnitudes is the regression
// guard for SEC-HIGH: unchecked uint64 mul in ShareValue/AccruedReward. With
// 1e9 base units per token, pools reach ~1e10 magnitudes where the raw
// shares*TotalBondedStake product exceeds 2^64 and silently wraps, underpaying
// a withdrawal by ~90%. The checked (big.Int) path must return the exact value.
func TestShareValueAndAccruedRewardDoNotWrapAtLargeMagnitudes(t *testing.T) {
	mag := uint64(5_000_000_000) // 5e9; 5e9*5e9 = 2.5e19 > 2^64 (var, so mul is runtime)

	// Sanity: the naive uint64 product really does wrap to a wrong small value.
	require.NotEqual(t, mag, mag*mag/mag, "precondition: raw uint64 mul wraps here")

	pool := NominatorPool{TotalShares: mag, TotalBondedStake: mag}
	value, err := ShareValue(pool, mag)
	require.NoError(t, err)
	require.Equal(t, mag, value, "full-pool share value must be exact, not wrapped")

	// AccruedReward: shares*(indexDelta)/IndexScale must also be exact.
	delegator := DelegatorShare{Shares: mag, RewardIndexCheckpoint: 0}
	reward, err := AccruedReward(delegator, mag) // rewardIndex = 5e9, IndexScale = 1e9
	require.NoError(t, err)
	// 5e9 * 5e9 / 1e9 = 25e9 (computed via big.Int, so no intermediate wrap).
	require.Equal(t, uint64(25_000_000_000), reward)
}
