// Package service is the explorer's live ingestion loop: it polls a node's
// CometBFT RPC for new blocks, decodes each through the ingest layer, and
// writes them to the store. It also exposes a StatusProvider that reads the
// chain tip from RPC for the /status endpoint.
package service

import (
	"context"
	"strings"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"

	"cosmossdk.io/log/v2"

	"github.com/sovereign-l1/l1/explorer/ingest"
	"github.com/sovereign-l1/l1/explorer/model"
	"github.com/sovereign-l1/l1/explorer/store"
)

// Indexer polls a node and fills a store.
type Indexer struct {
	rpc     *rpchttp.HTTP
	store   store.Store
	decode  ingest.TxDecoder
	fmtAddr ingest.AddressFormatter
	logger  log.Logger
	poll    time.Duration
	// startHeight is the first height to index; 0 means "earliest available".
	startHeight int64
	// maxCatchup bounds how many blocks a single tick ingests so a far-behind
	// indexer makes steady progress without blocking status/serving.
	maxCatchup int64
	next       int64
}

// Options configures the indexer.
type Options struct {
	RPCEndpoint string // e.g. "http://127.0.0.1:26657"
	Poll        time.Duration
	StartHeight int64
	MaxCatchup  int64
}

// New creates an indexer. decode and fmtAddr come from the app (TxConfig
// decoder and the address formatter) so ingest can render txs and addresses.
func New(opts Options, s store.Store, decode ingest.TxDecoder, fmtAddr ingest.AddressFormatter, logger log.Logger) (*Indexer, error) {
	rpc, err := rpchttp.New(opts.RPCEndpoint, "")
	if err != nil {
		return nil, err
	}
	if opts.Poll <= 0 {
		opts.Poll = time.Second
	}
	if opts.MaxCatchup <= 0 {
		opts.MaxCatchup = 200
	}
	return &Indexer{
		rpc: rpc, store: s, decode: decode, fmtAddr: fmtAddr, logger: logger,
		poll: opts.Poll, startHeight: opts.StartHeight, maxCatchup: opts.MaxCatchup,
	}, nil
}

// Run blocks until ctx is cancelled, ingesting new blocks each tick.
func (ix *Indexer) Run(ctx context.Context) error {
	ticker := time.NewTicker(ix.poll)
	defer ticker.Stop()
	ix.tick(ctx) // ingest immediately on start
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ix.tick(ctx)
		}
	}
}

func (ix *Indexer) tick(ctx context.Context) {
	status, err := ix.rpc.Status(ctx)
	if err != nil {
		ix.logger.Debug("explorer: rpc status failed", "err", err)
		return
	}
	tip := status.SyncInfo.LatestBlockHeight
	earliest := status.SyncInfo.EarliestBlockHeight

	if ix.next == 0 {
		ix.next = ix.startHeight
		if ix.next < earliest {
			ix.next = earliest
		}
		if ix.next < 1 {
			ix.next = 1
		}
	}

	limit := ix.next + ix.maxCatchup
	for h := ix.next; h <= tip && h < limit; h++ {
		if err := ix.ingestHeight(ctx, h); err != nil {
			ix.logger.Debug("explorer: ingest failed", "height", h, "err", err)
			return // retry same height next tick
		}
		ix.next = h + 1
	}
}

func (ix *Indexer) ingestHeight(ctx context.Context, height int64) error {
	blockRes, err := ix.rpc.Block(ctx, &height)
	if err != nil {
		return err
	}
	results, err := ix.rpc.BlockResults(ctx, &height)
	if err != nil {
		return err
	}
	blk, txs := ingest.Block(blockRes.Block, results.TxsResults, ix.decode, ix.fmtAddr)
	ix.store.PutBlock(blk, txs)
	return nil
}

// StatusProvider reads the live chain tip for the /status endpoint.
func (ix *Indexer) StatusProvider(ctx context.Context) (model.Status, error) {
	status, err := ix.rpc.Status(ctx)
	if err != nil {
		return model.Status{}, err
	}
	return model.Status{
		ChainID:         status.NodeInfo.Network,
		LatestHeight:    status.SyncInfo.LatestBlockHeight,
		LatestBlockTime: status.SyncInfo.LatestBlockTime.UTC().Format(time.RFC3339Nano),
		LatestBlockHash: strings.ToUpper(status.SyncInfo.LatestBlockHash.String()),
		CatchingUp:      status.SyncInfo.CatchingUp,
		NodeMoniker:     status.NodeInfo.Moniker,
		CometBFTVersion: status.NodeInfo.Version,
	}, nil
}
