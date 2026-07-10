package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// stubDecoder always fails: this test focuses on the block/result/event
// ingestion that must work even when a tx body cannot be decoded (a rejected
// or malformed tx still gets an indexed record).
func stubDecoder(_ []byte) (sdk.Tx, error) { return nil, errors.New("stub: no decode") }

func hexAddr(b []byte) string { return "addr:" + hex.EncodeToString(b) }

func TestBlockIngestsSummaryTxHashesResultsAndEventAddresses(t *testing.T) {
	rawTx := []byte("raw-transaction-bytes")
	block := &cmttypes.Block{
		Header: cmttypes.Header{
			Height:          42,
			Time:            time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
			ProposerAddress: cmttypes.Address{0x01, 0x02, 0x03},
			AppHash:         []byte{0xaa, 0xbb},
			DataHash:        []byte{0xcc, 0xdd},
		},
		Data: cmttypes.Data{Txs: []cmttypes.Tx{rawTx}},
	}
	results := []*abci.ExecTxResult{{
		Code:      0,
		GasWanted: 900000,
		GasUsed:   238540,
		Events: []abci.Event{{
			Type: "transfer",
			Attributes: []abci.EventAttribute{
				{Key: "sender", Value: "AEJkAsjF88vDN6tz9eHrhUyeP2TgB0FkLMPvsdUnSUGQ"},
				{Key: "recipient", Value: "ae1alicexxxxxxxxxxxxxxxxxxxxxxxx"},
				{Key: "amount", Value: "5naet"},
			},
		}},
	}}

	blk, txs := Block(block, results, stubDecoder, hexAddr)

	require.Equal(t, int64(42), blk.Height)
	require.Equal(t, 1, blk.NumTxs)
	require.Equal(t, "addr:010203", blk.Proposer)
	require.Equal(t, "AABB", blk.AppHash)
	require.Equal(t, "2026-07-10T12:00:00Z", blk.Time)

	wantHash := strings.ToUpper(func() string { s := sha256.Sum256(rawTx); return hex.EncodeToString(s[:]) }())
	require.Equal(t, []string{wantHash}, blk.TxHashes)

	require.Len(t, txs, 1)
	tx := txs[0]
	require.Equal(t, wantHash, tx.Hash)
	require.Equal(t, int64(42), tx.Height)
	require.True(t, tx.Success)
	require.Equal(t, int64(238540), tx.GasUsed)
	require.Len(t, tx.Events, 1)
	require.Equal(t, "transfer", tx.Events[0].Type)

	// address-like event values are indexed; the "5naet" amount is not.
	require.Contains(t, tx.Addresses, "AEJkAsjF88vDN6tz9eHrhUyeP2TgB0FkLMPvsdUnSUGQ")
	require.Contains(t, tx.Addresses, "ae1alicexxxxxxxxxxxxxxxxxxxxxxxx")
	require.NotContains(t, tx.Addresses, "5naet")
}

func TestBlockRecordsFailedTxWithRawLog(t *testing.T) {
	block := &cmttypes.Block{
		Header: cmttypes.Header{Height: 7, Time: time.Unix(0, 0).UTC()},
		Data:   cmttypes.Data{Txs: []cmttypes.Tx{[]byte("bad")}},
	}
	results := []*abci.ExecTxResult{{Code: 5, Codespace: "sdk", Log: "insufficient fee", GasUsed: 10}}

	_, txs := Block(block, results, stubDecoder, hexAddr)
	require.Len(t, txs, 1)
	require.False(t, txs[0].Success)
	require.Equal(t, uint32(5), txs[0].Code)
	require.Equal(t, "sdk", txs[0].Codespace)
	require.Equal(t, "insufficient fee", txs[0].RawLog)
}

// keep cmtproto import referenced (Header proto vs value type parity guard)
var _ = cmtproto.Header{}
