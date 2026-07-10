// Package model defines the stable JSON shapes an Aetra block-explorer
// frontend consumes. These are read-only projections of chain data — never a
// source of truth — so every field maps directly onto CometBFT RPC or the
// app's gRPC query responses. Keeping them in one place lets the ingest,
// store, and api layers agree on a single contract with the frontend.
package model

// Status is the explorer/chain health summary (GET /status).
type Status struct {
	ChainID         string `json:"chain_id"`
	LatestHeight    int64  `json:"latest_height"`
	LatestBlockTime string `json:"latest_block_time"`
	LatestBlockHash string `json:"latest_block_hash"`
	IndexedHeight   int64  `json:"indexed_height"`
	IndexedBlocks   int    `json:"indexed_blocks"`
	IndexedTxs      int    `json:"indexed_txs"`
	CatchingUp      bool   `json:"catching_up"`
	NodeMoniker     string `json:"node_moniker,omitempty"`
	AppVersion      string `json:"app_version,omitempty"`
	CometBFTVersion string `json:"cometbft_version,omitempty"`
}

// BlockSummary is a compact block row for lists (GET /blocks).
type BlockSummary struct {
	Height   int64  `json:"height"`
	Time     string `json:"time"`
	Hash     string `json:"hash"`
	Proposer string `json:"proposer"`
	NumTxs   int    `json:"num_txs"`
}

// Block is a full block detail (GET /blocks/{height}).
type Block struct {
	BlockSummary
	AppHash  string   `json:"app_hash"`
	DataHash string   `json:"data_hash"`
	TxHashes []string `json:"tx_hashes"`
}

// Coin is a denom/amount pair.
type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// MsgSummary describes one message inside a tx.
type MsgSummary struct {
	TypeURL string   `json:"type_url"`
	Signers []string `json:"signers,omitempty"`
	// Summary is a short human string when the explorer can cheaply derive one
	// (e.g. "send 5 AET"); empty otherwise.
	Summary string `json:"summary,omitempty"`
}

// EventAttr is one key/value on a tx event.
type EventAttr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Event is a typed ABCI event with its attributes.
type Event struct {
	Type       string      `json:"type"`
	Attributes []EventAttr `json:"attributes"`
}

// Tx is a transaction detail (GET /txs/{hash}).
type Tx struct {
	Hash      string       `json:"hash"`
	Height    int64        `json:"height"`
	Time      string       `json:"time"`
	Index     int          `json:"index"`
	Success   bool         `json:"success"`
	Code      uint32       `json:"code"`
	Codespace string       `json:"codespace,omitempty"`
	RawLog    string       `json:"raw_log,omitempty"`
	GasWanted int64        `json:"gas_wanted"`
	GasUsed   int64        `json:"gas_used"`
	Fee       []Coin       `json:"fee,omitempty"`
	Memo      string       `json:"memo,omitempty"`
	Messages  []MsgSummary `json:"messages"`
	Events    []Event      `json:"events,omitempty"`
	// Addresses is every account/contract address touched by this tx (signers,
	// event participants). It backs the per-address tx index.
	Addresses []string `json:"addresses,omitempty"`
}

// TxSummary is a compact tx row for lists (GET /txs).
type TxSummary struct {
	Hash     string `json:"hash"`
	Height   int64  `json:"height"`
	Time     string `json:"time"`
	Success  bool   `json:"success"`
	NumMsgs  int    `json:"num_msgs"`
	FirstMsg string `json:"first_msg,omitempty"`
}

// Paged wraps a list result with basic pagination metadata.
type Paged[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}
