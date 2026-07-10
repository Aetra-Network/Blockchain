package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	l1app "github.com/sovereign-l1/l1/app"
	"github.com/sovereign-l1/l1/x/internal/prototype"
	loadkeeper "github.com/sovereign-l1/l1/x/load/keeper"
)

// TestFeesEndBlockerFeedsLoadScorerWhenEnabled proves the live load signal:
// every fees EndBlock pushes the finalized block metrics into the x/load
// scorer, whose EMA/band output is the input the zone/routing layer consumes
// for load distribution. This is the wiring that was missing — the scorer
// existed but nothing fed it from consensus.
func TestFeesEndBlockerFeedsLoadScorerWhenEnabled(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false)

	gs := loadkeeper.DefaultGenesis()
	gs.Params = prototypeTestnetParams()
	require.NoError(t, app.LoadKeeper.InitGenesisState(ctx, gs))

	require.NoError(t, app.FeesKeeper.EndBlocker(ctx))
	require.NoError(t, app.FeesKeeper.EndBlocker(ctx))

	history, _, err := app.LoadKeeper.History(nil)
	require.NoError(t, err)
	require.Len(t, history, 2, "each EndBlock must append one load result")
}

// TestFeesEndBlockerIsSilentNoOpWhileLoadDisabled pins the safety property:
// with x/load at its disabled default, the fees EndBlocker must neither fail
// the block nor write load state.
func TestFeesEndBlockerIsSilentNoOpWhileLoadDisabled(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false)

	require.NoError(t, app.FeesKeeper.EndBlocker(ctx))

	history, _, err := app.LoadKeeper.History(nil)
	require.NoError(t, err)
	require.Empty(t, history)
}

// TestLoadHistoryStaysBounded pins the state-growth bound on the per-block
// feed: the retained history never exceeds MaxHistoryEntries.
func TestLoadHistoryStaysBounded(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false)

	gs := loadkeeper.DefaultGenesis()
	gs.Params = prototypeTestnetParams()
	require.NoError(t, app.LoadKeeper.InitGenesisState(ctx, gs))

	for i := 0; i < loadkeeper.MaxHistoryEntries+16; i++ {
		require.NoError(t, app.FeesKeeper.EndBlocker(ctx))
	}
	history, _, err := app.LoadKeeper.History(nil)
	require.NoError(t, err)
	require.LessOrEqual(t, len(history), loadkeeper.MaxHistoryEntries)
}

func prototypeTestnetParams() prototype.Params {
	return prototype.TestnetParams()
}
