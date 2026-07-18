package app

import (
	"math/rand"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/sovereign-l1/l1/x/fees/types"
)

// TestPhase6AppHashInertWhenSingleZone is the app-level, real-FinalizeBlock
// inertness proof. It runs an identical sequence of signed bank-send blocks
// through two apps built from the SAME genesis:
//
//   - "wired": the production configuration, with AEZ Phase 6 per-zone quotas
//     wired into the fees admission gate (&app.AEZKeeper);
//   - "control": the same app with the zone resolver removed and the ante chain
//     rebuilt -- i.e. the pre-Phase-6 admission path.
//
// With every bucket on zone 0 every tx's home zone is Core (uncapped), so Phase
// 6 must be behaviourally invisible. The test asserts the per-block AppHash is
// byte-identical between the two apps across the whole run. If the resolver's
// routing-table reads leaked onto the metered gas meter, or a per-zone counter
// were written for a Core tx, the block gas meter / congestion bps / state root
// would diverge and this equality would fail.
func TestPhase6AppHashInertWhenSingleZone(t *testing.T) {
	_, genesis, valSet := deterministicGenesisWithValidator(t)
	genesisBytes, err := cmtjson.MarshalIndent(genesis, "", " ")
	require.NoError(t, err)

	wired := runSignedTransferBlocks(t, genesisBytes, valSet.Hash(), false)
	control := runSignedTransferBlocks(t, genesisBytes, valSet.Hash(), true)

	require.Equal(t, len(control.appHashes), len(wired.appHashes))
	for i := range wired.appHashes {
		require.Equal(t, control.appHashes[i], wired.appHashes[i],
			"AppHash diverged at block %d between the Phase-6-wired app and the resolver-less control", i+1)
	}
	// The run must actually exercise the admit-and-execute path, or the equality
	// above would be vacuous. At least one tx per block must have been delivered
	// successfully (code 0) in the wired app.
	require.Greater(t, wired.deliveredOK, 0, "no transfer admitted; the test would be vacuous")
	require.Equal(t, control.deliveredOK, wired.deliveredOK, "the two apps admitted a different number of txs")
}

type transferRun struct {
	appHashes	[][]byte
	deliveredOK	int
}

func runSignedTransferBlocks(t *testing.T, genesisBytes []byte, nextValidatorsHash []byte, dropResolver bool) transferRun {
	t.Helper()

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = DefaultNodeHome
	// Build UNSEALED (loadLatest=false) so the ante chain can be rebuilt before
	// the baseapp is sealed by LoadLatestVersion. Both apps take the identical
	// construction path; only the control drops the resolver.
	app := NewL1App(log.NewNopLogger(), dbm.NewMemDB(), false, appOptions)
	if dropResolver {
		// Remove the AEZ zone resolver so this app runs the pre-Phase-6
		// admission path. Nothing else changes.
		app.FeesKeeper = app.FeesKeeper.WithZoneResolver(nil)
	}
	app.setAnteHandler(app.txConfig)
	require.NoError(t, app.LoadLatestVersion())

	_, err := app.InitChain(&abci.RequestInitChain{
		Validators:		[]abci.ValidatorUpdate{},
		ConsensusParams:	simtestutil.DefaultConsensusParams,
		AppStateBytes:		genesisBytes,
	})
	require.NoError(t, err)

	chainID := app.ChainID()
	senderPrivKey := secp256k1.GenPrivKeyFromSecret([]byte("aetra-deterministic-account"))
	senderAddr := sdk.AccAddress(senderPrivKey.PubKey().Address())
	recipientAddr := sdk.AccAddress(secp256k1.GenPrivKeyFromSecret([]byte("aetra-phase6-recipient")).PubKey().Address())

	ctx := app.NewContext(false)
	acc := app.AccountKeeper.GetAccount(ctx, senderAddr)
	require.NotNil(t, acc, "sender account must exist after InitChain")
	accNum := acc.GetAccountNumber()

	msg := banktypes.NewMsgSend(senderAddr, recipientAddr, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)))
	// A fee that comfortably clears the full-formula requirement for a ~200k-gas
	// transfer and stays under the 5 AET hard cap.
	fee := sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 2_000_000_000))

	const (
		blocks		= 3
		txsPerBlock	= 3
	)
	run := transferRun{}
	seq := uint64(0)
	for height := int64(1); height <= blocks; height++ {
		txs := make([][]byte, 0, txsPerBlock)
		for j := 0; j < txsPerBlock; j++ {
			tx, err := simtestutil.GenSignedMockTx(
				rand.New(rand.NewSource(int64(seq)+1)),
				app.TxConfig(),
				[]sdk.Msg{msg},
				fee,
				200_000,
				chainID,
				[]uint64{accNum},
				[]uint64{seq},
				senderPrivKey,
			)
			require.NoError(t, err)
			txBytes, err := app.TxConfig().TxEncoder()(tx)
			require.NoError(t, err)
			txs = append(txs, txBytes)
			seq++
		}

		res, err := app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height:			height,
			Hash:			app.LastCommitID().Hash,
			NextValidatorsHash:	nextValidatorsHash,
			Txs:			txs,
		})
		require.NoError(t, err)
		for _, txRes := range res.TxResults {
			if txRes.Code == 0 {
				run.deliveredOK++
			}
		}
		run.appHashes = append(run.appHashes, append([]byte(nil), res.AppHash...))
		_, err = app.Commit()
		require.NoError(t, err)
	}
	return run
}
