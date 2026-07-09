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

	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
)

func BenchmarkEmptyBlockFinalizeCommit(b *testing.B) {
	_, genesis, valSet := deterministicGenesisWithValidator(b)
	genesisBytes, err := cmtjson.MarshalIndent(genesis, "", " ")
	require.NoError(b, err)

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = DefaultNodeHome
	app := NewL1App(log.NewNopLogger(), dbm.NewMemDB(), true, appOptions)

	_, err = app.InitChain(&abci.RequestInitChain{
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   genesisBytes,
	})
	require.NoError(b, err)

	nextValidatorsHash := valSet.Hash()
	b.ResetTimer()
	for height := int64(1); height <= int64(b.N); height++ {
		_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height:             height,
			Hash:               app.LastCommitID().Hash,
			NextValidatorsHash: nextValidatorsHash,
		})
		require.NoError(b, err)
		_, err = app.Commit()
		require.NoError(b, err)
	}
}

func BenchmarkTPS(b *testing.B) {
	const txsPerBlock = 100

	_, genesis, valSet := deterministicGenesisWithValidator(b)
	genesisBytes, err := cmtjson.MarshalIndent(genesis, "", " ")
	require.NoError(b, err)

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = DefaultNodeHome
	app := NewL1App(log.NewNopLogger(), dbm.NewMemDB(), true, appOptions)

	_, err = app.InitChain(&abci.RequestInitChain{
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   genesisBytes,
	})
	require.NoError(b, err)

	nextValidatorsHash := valSet.Hash()
	chainID := app.ChainID()

	senderPrivKey := secp256k1.GenPrivKeyFromSecret([]byte("aetra-deterministic-account"))
	senderAddr := sdk.AccAddress(senderPrivKey.PubKey().Address())
	recipientAddr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	ctx := app.NewContext(false)
	acc := app.AccountKeeper.GetAccount(ctx, senderAddr)
	require.NotNil(b, acc, "sender account must exist after InitChain")
	accNum := acc.GetAccountNumber()

	msg := banktypes.NewMsgSend(senderAddr, recipientAddr, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)))

	seq := uint64(0)
	b.ResetTimer()
	for height := int64(1); height <= int64(b.N); height++ {
		b.StopTimer()
		txs := make([][]byte, txsPerBlock)
		for j := 0; j < txsPerBlock; j++ {
			tx, err := simtestutil.GenSignedMockTx(
				rand.New(rand.NewSource(int64(seq))),
				app.TxConfig(),
				[]sdk.Msg{msg},
				sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 0)),
				500_000,
				chainID,
				[]uint64{accNum},
				[]uint64{seq},
				senderPrivKey,
			)
			require.NoError(b, err)
			txBytes, err := app.TxConfig().TxEncoder()(tx)
			require.NoError(b, err)
			txs[j] = txBytes
			seq++
		}
		b.StartTimer()

		_, err = app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height:             height,
			Hash:               app.LastCommitID().Hash,
			NextValidatorsHash: nextValidatorsHash,
			Txs:                txs,
		})
		require.NoError(b, err)
		_, err = app.Commit()
		require.NoError(b, err)
	}

	totalTxs := float64(b.N) * txsPerBlock
	elapsed := float64(b.Elapsed().Nanoseconds()) / 1e9
	b.ReportMetric(totalTxs/elapsed, "tx/sec")
	blockTimeMs := elapsed / float64(b.N) * 1000
	b.ReportMetric(blockTimeMs, "block/ms")
}

func BenchmarkAVMRuntimeRepeatedActivityBoundedGrowth(b *testing.B) {
	const rounds = 24

	var fixture avmRuntimeGrowthFixture
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		fixture = newAVMRuntimeGrowthFixture(b)
		b.StartTimer()
		for step := 0; step < rounds; step++ {
			runAVMRuntimeGrowthStep(b, fixture, step)
		}
	}
	b.StopTimer()

	storage, err := fixture.app.ContractsKeeper.ContractStorage(contractstypes.QueryContractStorageRequest{
		ContractAddress: fixture.contract.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: 10},
	})
	require.NoError(b, err)
	queue, err := fixture.app.ContractsKeeper.ContractQueue(contractstypes.QueryContractQueueRequest{
		ContractAddress: fixture.contract.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: contractstypes.MaxContractQueryLimit},
	})
	require.NoError(b, err)
	receipts, err := fixture.app.ContractsKeeper.ContractReceipts(contractstypes.QueryContractReceiptsRequest{
		ContractAddress: fixture.contract.ContractAddressUser,
		Pagination:      contractstypes.PageRequest{Limit: contractstypes.MaxContractQueryLimit},
	})
	require.NoError(b, err)
	exported, err := fixture.app.ContractsKeeper.ExportGenesisState(fixture.ctx)
	require.NoError(b, err)

	b.ReportMetric(float64(rounds), "rounds")
	b.ReportMetric(float64(len(storage)), "storage_entries")
	b.ReportMetric(float64(len(queue)), "queue_entries")
	b.ReportMetric(float64(len(receipts)), "receipt_entries")
	b.ReportMetric(float64(len(exported.State.Contracts)), "contracts")
	b.ReportMetric(float64(len(exported.State.InternalMessages)), "internal_messages")
	b.ReportMetric(float64(len(exported.State.Receipts)), "exported_receipts")
}
