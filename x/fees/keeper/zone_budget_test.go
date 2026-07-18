package keeper_test

import (
	"bytes"
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	l1app "github.com/sovereign-l1/l1/app"
	"github.com/sovereign-l1/l1/x/fees/types"
)

// fakeZoneResolver drives AdmitTx through a chosen elastic zone without needing
// a live multi-zone routing table. It maps the exact address STRING that AdmitTx
// passes (FormatUserFriendly of the sender) to a zone, and a zone to its cap.
type fakeZoneResolver struct {
	zoneByAddr	map[string]uint32
	capByZone	map[uint32]uint64
}

func (f fakeZoneResolver) ZoneOfAddress(_ context.Context, address string) (uint32, error) {
	return f.zoneByAddr[address], nil // absent -> 0 (Core)
}

func (f fakeZoneResolver) GasQuotaForZone(_ context.Context, zoneID uint32) (uint64, error) {
	return f.capByZone[zoneID], nil // absent -> 0 (uncapped)
}

// TestElasticZoneBudgetExhaustsThenResets exercises the real per-zone counter
// plumbing end to end through AdmitTx:
//
//   - three max-gas txs fill an elastic zone's 3,000,000 cap and are admitted;
//   - the fourth is rejected with a per-zone error while the GLOBAL budget still
//     has ~17M of room;
//   - a Core-zone tx still admits with the elastic zone saturated (the Core
//     reserve is untouchable by elastic load);
//   - at the next block height the elastic zone's counter self-resets, so the
//     sender admits again.
func TestElasticZoneBudgetExhaustsThenResets(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	elasticSender := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	coreSender := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	elasticText := validRawAddress(0x11)

	resolver := fakeZoneResolver{
		zoneByAddr:	map[string]uint32{elasticText: 2},
		capByZone:	map[uint32]uint64{2: 3_000_000},
	}
	k := app.FeesKeeper.WithZoneResolver(resolver)

	// A max-gas (1,000,000) tx with a fee that clears the full-formula
	// requirement for that gas.
	fee := sdk.NewCoins(sdk.NewCoin(types.BondDenom, requiredFullFee(t, 1_000_000, 0)))
	elasticTx := feeTx{fees: fee, payer: elasticSender, gas: 1_000_000}

	// Three fit exactly into the 3,000,000 cap.
	for i := 0; i < 3; i++ {
		if _, err := k.AdmitTx(ctx, elasticTx, elasticSender, false); err != nil {
			t.Fatalf("elastic tx %d must admit within the zone cap: %v", i+1, err)
		}
	}
	// The fourth exceeds the zone cap even though the global budget is nearly
	// empty.
	_, err := k.AdmitTx(ctx, elasticTx, elasticSender, false)
	require.Error(t, err, "fourth elastic tx must be rejected by the per-zone budget")
	require.ErrorIs(t, err, types.ErrInvalidFee)
	require.Contains(t, err.Error(), "zone 2 gas limit")

	// A Core-zone sender still admits with the elastic zone saturated: the Core
	// reserve is never consumed by elastic load.
	coreFee := sdk.NewCoins(sdk.NewCoin(types.BondDenom, requiredFullFee(t, 1_000_000, 0)))
	coreTx := feeTx{fees: coreFee, payer: coreSender, gas: 1_000_000}
	if _, err := k.AdmitTx(ctx, coreTx, coreSender, false); err != nil {
		t.Fatalf("core tx must admit while an elastic zone is saturated: %v", err)
	}

	// Next block: the height-keyed counter self-resets, so the elastic sender
	// admits again without any explicit reset write.
	next := ctx.WithBlockHeight(2)
	if _, err := k.AdmitTx(next, elasticTx, elasticSender, false); err != nil {
		t.Fatalf("elastic zone budget must reset at the next height: %v", err)
	}
}

// TestElasticBudgetNotWrittenOnSimulate proves a simulated admission never
// reserves zone gas (mirrors the block/sender counters), so a dry run cannot
// starve a zone.
func TestElasticBudgetNotWrittenOnSimulate(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	elasticSender := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	elasticText := validRawAddress(0x11)
	resolver := fakeZoneResolver{
		zoneByAddr:	map[string]uint32{elasticText: 2},
		capByZone:	map[uint32]uint64{2: 3_000_000},
	}
	k := app.FeesKeeper.WithZoneResolver(resolver)
	fee := sdk.NewCoins(sdk.NewCoin(types.BondDenom, requiredFullFee(t, 1_000_000, 0)))
	elasticTx := feeTx{fees: fee, payer: elasticSender, gas: 1_000_000}

	// Simulate many times: none should reserve gas.
	for i := 0; i < 10; i++ {
		if _, err := k.AdmitTx(ctx, elasticTx, elasticSender, true); err != nil {
			t.Fatalf("simulated elastic tx must admit: %v", err)
		}
	}
	// After all those simulations, three real admissions still fit.
	for i := 0; i < 3; i++ {
		if _, err := k.AdmitTx(ctx, elasticTx, elasticSender, false); err != nil {
			t.Fatalf("real elastic tx %d must admit after simulations: %v", i+1, err)
		}
	}
}
