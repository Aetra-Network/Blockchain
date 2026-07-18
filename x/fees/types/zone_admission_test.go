package types

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// adequateFee returns a fee that clears the dynamic requirement for the given
// gas at zero block utilization, so admission fails (or passes) on the gas
// checks under test rather than on the fee amount.
func adequateFee(t *testing.T, params Params, gas uint64) sdk.Coins {
	t.Helper()
	maxFee, err := params.MaxFeeInt()
	if err != nil {
		t.Fatalf("max fee: %v", err)
	}
	return sdk.NewCoins(sdk.NewCoin(BondDenom, maxFee))
}

// TestZoneGateCoreSentinelIsInert proves the Core / disabled path (ZoneMaxGas
// == 0) is a pure no-op: the outcome and the returned quote are identical to the
// pre-Phase-6 call whose zone fields were all zero, for ANY ZoneGasConsumed.
func TestZoneGateCoreSentinelIsInert(t *testing.T) {
	params := DefaultParams()
	base := AdmissionInput{
		Fee:			adequateFee(t, params, 100_000),
		GasLimit:		100_000,
		BlockGasConsumed:	0,
		BlockTxCount:		1,
		SenderTxCount:		1,
		SenderStake:		sdkmath.ZeroInt(),
	}
	want, wantErr := ValidateAdmission(params, base)
	if wantErr != nil {
		t.Fatalf("baseline admission must pass: %v", wantErr)
	}

	// Core zone, uncapped, with a nonsense-large per-zone "consumed" value that
	// would reject if the gate ran. It must be ignored entirely.
	for _, consumed := range []uint64{0, 1, 19_000_000, ^uint64(0)} {
		in := base
		in.ZoneID = 0
		in.ZoneMaxGas = 0
		in.ZoneGasConsumed = consumed
		got, err := ValidateAdmission(params, in)
		if err != nil {
			t.Fatalf("core sentinel must admit (consumed=%d): %v", consumed, err)
		}
		if !got.RequiredFee.IsEqual(want.RequiredFee) || got.UtilizationBps != want.UtilizationBps {
			t.Fatalf("core sentinel quote differs from baseline (consumed=%d): got %+v want %+v", consumed, got, want)
		}
	}
}

// TestZoneGateRejectsExhaustedElasticZone: a tx in an elastic zone is rejected
// when that zone's remaining budget is exhausted, EVEN THOUGH the global block
// budget still has ample room.
func TestZoneGateRejectsExhaustedElasticZone(t *testing.T) {
	params := DefaultParams()
	in := AdmissionInput{
		Fee:			adequateFee(t, params, 1_000_000),
		GasLimit:		1_000_000, // == MaxTxGas
		BlockGasConsumed:	1_000_000, // global barely used: 19M free
		BlockTxCount:		1,
		SenderTxCount:		1,
		SenderStake:		sdkmath.ZeroInt(),
		ZoneID:			2,
		ZoneMaxGas:		3_000_000,
		ZoneGasConsumed:	2_500_000, // 2.5M + 1M = 3.5M > 3M cap
	}
	if _, err := ValidateAdmission(params, in); err == nil {
		t.Fatal("expected rejection: elastic zone budget exhausted")
	} else if !errIsInvalidFee(err) {
		t.Fatalf("expected ErrInvalidFee, got %v", err)
	}

	// The SAME global state, but the tx is a Core tx (ZoneMaxGas == 0): it must
	// admit, because Core is gated only by the untouched global budget and 19M
	// is free. This is the Core-reservation guarantee in action.
	core := in
	core.ZoneID = 0
	core.ZoneMaxGas = 0
	core.ZoneGasConsumed = 0
	if _, err := ValidateAdmission(params, core); err != nil {
		t.Fatalf("core tx must admit while an elastic zone is saturated: %v", err)
	}
}

// TestZoneGateAdmitsWithinElasticBudget: a tx that exactly fills the remaining
// elastic budget is admitted (the bound is inclusive, mirroring the global
// check).
func TestZoneGateAdmitsWithinElasticBudget(t *testing.T) {
	params := DefaultParams()
	in := AdmissionInput{
		Fee:			adequateFee(t, params, 1_000_000),
		GasLimit:		1_000_000,
		BlockGasConsumed:	0,
		BlockTxCount:		1,
		SenderTxCount:		1,
		SenderStake:		sdkmath.ZeroInt(),
		ZoneID:			3,
		ZoneMaxGas:		3_000_000,
		ZoneGasConsumed:	2_000_000, // 2M + 1M = 3M == cap
	}
	if _, err := ValidateAdmission(params, in); err != nil {
		t.Fatalf("tx exactly filling the elastic budget must admit: %v", err)
	}
}

// TestZoneGateDoesNotChangeFeeAmount: for an elastic tx that passes the gate,
// the required fee is identical to the same input with the zone fields zeroed --
// proving the per-zone gate is admission-only and never feeds QuoteFee.
func TestZoneGateDoesNotChangeFeeAmount(t *testing.T) {
	params := DefaultParams()
	base := AdmissionInput{
		Fee:			adequateFee(t, params, 500_000),
		GasLimit:		500_000,
		BlockGasConsumed:	4_000_000,
		BlockTxCount:		1,
		SenderTxCount:		1,
		SenderStake:		sdkmath.ZeroInt(),
	}
	noZone, err := ValidateAdmission(params, base)
	if err != nil {
		t.Fatalf("baseline must admit: %v", err)
	}
	withZone := base
	withZone.ZoneID = 1
	withZone.ZoneMaxGas = 3_000_000
	withZone.ZoneGasConsumed = 100_000
	got, err := ValidateAdmission(params, withZone)
	if err != nil {
		t.Fatalf("elastic tx must admit: %v", err)
	}
	if !got.RequiredFee.IsEqual(noZone.RequiredFee) {
		t.Fatalf("per-zone gate changed the fee: got %s want %s", got.RequiredFee, noZone.RequiredFee)
	}
}

// TestZoneGateOverflowGuard: a ZoneGasConsumed near uint64 max must reject on
// overflow rather than wrapping to a small sum that would spuriously admit.
func TestZoneGateOverflowGuard(t *testing.T) {
	params := DefaultParams()
	in := AdmissionInput{
		Fee:			adequateFee(t, params, 1),
		GasLimit:		1,
		BlockGasConsumed:	0,
		BlockTxCount:		1,
		SenderTxCount:		1,
		SenderStake:		sdkmath.ZeroInt(),
		ZoneID:			4,
		ZoneMaxGas:		^uint64(0),
		ZoneGasConsumed:	^uint64(0),
	}
	if _, err := ValidateAdmission(params, in); err == nil {
		t.Fatal("expected overflow rejection in the per-zone gate")
	}
}

func errIsInvalidFee(err error) bool {
	return err != nil && ErrInvalidFee.Is(err)
}
