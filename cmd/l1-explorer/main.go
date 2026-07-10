// Command l1-explorer is the Aetra block-explorer data source: it indexes a
// node's blocks and transactions over CometBFT RPC and serves a read-only
// JSON HTTP API (blocks, txs, accounts, contracts, validators, supply) that a
// block-explorer frontend consumes. It is database-optional — the default
// store is an in-memory bounded index — so a single binary against any RPC +
// gRPC endpoint is enough to stand up an explorer backend.
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cosmossdk.io/log/v2"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"

	l1app "github.com/sovereign-l1/l1/app"
	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/explorer/api"
	"github.com/sovereign-l1/l1/explorer/chainquery"
	"github.com/sovereign-l1/l1/explorer/ingest"
	"github.com/sovereign-l1/l1/explorer/service"
	"github.com/sovereign-l1/l1/explorer/store"
)

func main() {
	var (
		rpcEndpoint  = flag.String("rpc", "http://127.0.0.1:26657", "CometBFT RPC endpoint of the node to index")
		grpcEndpoint = flag.String("grpc", "127.0.0.1:9090", "node gRPC endpoint for live contract/validator/supply queries (empty to disable)")
		listen       = flag.String("listen", "0.0.0.0:8080", "explorer HTTP API listen address")
		startHeight  = flag.Int64("start-height", 0, "first block height to index (0 = earliest available)")
		maxBlocks    = flag.Int("retain-blocks", 100_000, "max blocks to retain in the in-memory index (0 = unbounded)")
		poll         = flag.Duration("poll", time.Second, "block polling interval")
	)
	flag.Parse()

	logger := log.NewLogger(os.Stderr)

	// Build the app's tx decoder and address formatter from a throwaway app
	// instance — the same trick the CLI root uses to get a codec without a
	// running node. The explorer never runs a state machine; it only needs
	// the decoder and the interface registry.
	tempApp := l1app.NewL1App(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(l1app.DefaultNodeHome))
	sdkDecoder := tempApp.TxConfig().TxDecoder()
	txDecoder := ingest.TxDecoder(func(b []byte) (sdk.Tx, error) { return sdkDecoder(b) })
	fmtAddr := ingest.AddressFormatter(func(b []byte) string { return aetraaddress.FormatAccAddress(b) })

	memStore := store.NewMemory(*maxBlocks)

	indexer, err := service.New(service.Options{
		RPCEndpoint: *rpcEndpoint,
		Poll:        *poll,
		StartHeight: *startHeight,
	}, memStore, txDecoder, fmtAddr, logger)
	if err != nil {
		logger.Error("explorer: failed to start indexer", "err", err)
		os.Exit(1)
	}

	var chain api.ChainQuerier
	if *grpcEndpoint != "" {
		cq, err := chainquery.Dial(*grpcEndpoint)
		if err != nil {
			logger.Error("explorer: failed to dial gRPC", "err", err)
			os.Exit(1)
		}
		defer cq.Close()
		chain = cq
	}

	srv := api.New(memStore, chain, indexer.StatusProvider)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := indexer.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("explorer: indexer stopped", "err", err)
		}
	}()

	httpSrv := &http.Server{Addr: *listen, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	logger.Info("explorer: serving", "listen", *listen, "rpc", *rpcEndpoint, "grpc", *grpcEndpoint)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("explorer: http server error", "err", err)
		os.Exit(1)
	}
	logger.Info("explorer: stopped")
}
