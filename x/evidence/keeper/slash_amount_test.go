package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/evidence/types"
)

// TestSlashAmountDoesNotOverSlashOnOverflow is the regression guard for
// SEC-MED: slashAmount overflow guard slashes 100%. When stake*fractionBps
// would overflow uint64 the previous code returned the ENTIRE stake — a 100%
// confiscation — instead of the configured fraction. The overflow-safe split
// product must return the correct partial amount.
func TestSlashAmountDoesNotOverSlashOnOverflow(t *testing.T) {
	// stake*fraction overflows uint64: 4e16 * 500 = 2e19 > 1.8447e19.
	const stake = uint64(40_000_000_000_000_000) // 4e16
	const fractionBps = uint32(500)              // 5%

	got := slashAmount(stake, fractionBps)
	// 5% of 4e16 = 2e15, NOT the full 4e16.
	require.Equal(t, uint64(2_000_000_000_000_000), got)
	require.Less(t, got, stake, "must slash the fraction, not 100% of stake")
}

// TestSlashAmountExactForSmallStake confirms the split formula matches the
// naive computation in the non-overflow range.
func TestSlashAmountExactForSmallStake(t *testing.T) {
	stake := uint64(1_000_000)
	for _, bps := range []uint32{1, 250, 500, 2000, uint32(types.MaxBasisPoints)} {
		want := stake * uint64(bps) / uint64(types.MaxBasisPoints)
		if want == 0 && bps > 0 {
			want = 1
		}
		require.Equal(t, want, slashAmount(stake, bps), "bps=%d", bps)
	}
}
