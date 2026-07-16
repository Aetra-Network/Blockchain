package keeper

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/internal/prototype"
	"github.com/sovereign-l1/l1/x/nominator-pool/types"
	validatorregistrytypes "github.com/sovereign-l1/l1/x/validator-registry/types"
)

// TestDepositToPoolRejectsShareOverflowInsteadOfWrapping is a regression
// guard for F-11: DepositToPool accumulated delegator.Shares,
// pool.TotalShares and pool.TotalBondedStake with bare uint64 += instead of
// the module's own CheckedAddUint64 (already used correctly elsewhere, e.g.
// SyncPoolRewards). A deposit that would wrap TotalShares past
// math.MaxUint64 used to succeed silently, corrupting the pool's share
// accounting -- and the invariant meant to catch it (TotalShares !=
// sumShares(DelegatorShares)) used the same unchecked accumulation, so both
// sides wrapped identically and the check passed on the corrupted state.
//
// This is harmless today only because the pool holds no real bank custody
// (#2/SA2-N01/F-04): fixing this before wiring custody is what keeps the
// overflow from becoming a real fund-loss bug the moment it is wired.
func TestDepositToPoolRejectsShareOverflowInsteadOfWrapping(t *testing.T) {
	k := NewKeeper()
	poolID := "overflow-pool"
	_, err := k.CreateNominatorPool(types.MsgCreateNominatorPool{
		Authority:         prototype.DefaultAuthority,
		PoolID:            poolID,
		PoolOperator:      rawPoolAddressFromInt(1),
		ValidatorTarget:   rawPoolAddressFromInt(2),
		PoolCommissionBps: 100,
		Height:            1,
		ValidatorStatus:   validatorregistrytypes.StatusActive,
	})
	require.NoError(t, err)

	firstDelegator := rawPoolAddress("10")
	// First deposit at the 1:1 genesis share price sets Shares == Amount, so
	// depositing just over half of MaxUint64 pushes TotalShares within one
	// more equal deposit of wrapping past MaxUint64.
	hugeAmount := uint64(math.MaxUint64/2 + 1)
	_, err = k.DepositToPool(types.MsgDepositToPool{
		Authority: prototype.DefaultAuthority,
		PoolID:    poolID,
		Delegator: firstDelegator,
		Amount:    hugeAmount,
		Height:    2,
	})
	require.NoError(t, err)

	secondDelegator := rawPoolAddress("11")
	_, err = k.DepositToPool(types.MsgDepositToPool{
		Authority: prototype.DefaultAuthority,
		PoolID:    poolID,
		Delegator: secondDelegator,
		Amount:    hugeAmount,
		Height:    3,
	})
	require.Error(t, err, "a deposit that would overflow pool.TotalShares/TotalBondedStake must be rejected, not silently wrapped")
	require.Contains(t, err.Error(), "overflow")

	// The rejected deposit must not have partially applied: the pool's state
	// after the failed call must still match what it was after only the
	// first deposit.
	pool, found := k.NominatorPool(poolID)
	require.True(t, found)
	require.Equal(t, hugeAmount, pool.TotalShares)
	require.Equal(t, hugeAmount, pool.TotalBondedStake)
	_, found = k.PoolDelegator(poolID, secondDelegator)
	require.False(t, found, "the second delegator must not have been credited any shares")
}
