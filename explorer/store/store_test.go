package store

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/explorer/model"
)

func mkBlock(h int64, txHashes ...string) model.Block {
	return model.Block{
		BlockSummary: model.BlockSummary{Height: h, Hash: "BLK" + strconv.FormatInt(h, 10), NumTxs: len(txHashes)},
		TxHashes:     txHashes,
	}
}

func mkTx(hash string, h int64, addrs ...string) model.Tx {
	return model.Tx{Hash: hash, Height: h, Success: true, Addresses: addrs,
		Messages: []model.MsgSummary{{TypeURL: "/test.Msg"}}}
}

func TestMemoryIndexesBlocksTxsAndAddresses(t *testing.T) {
	s := NewMemory(0)
	s.PutBlock(mkBlock(1, "TX1"), []model.Tx{mkTx("TX1", 1, "ae1alice", "ae1bob")})
	s.PutBlock(mkBlock(2, "TX2"), []model.Tx{mkTx("TX2", 2, "ae1alice")})

	require.Equal(t, int64(2), s.LatestHeight())
	blocks, txs := s.Counts()
	require.Equal(t, 2, blocks)
	require.Equal(t, 2, txs)

	b, ok := s.Block(2)
	require.True(t, ok)
	require.Equal(t, int64(2), b.Height)

	bh, ok := s.BlockByHash("BLK1")
	require.True(t, ok)
	require.Equal(t, int64(1), bh.Height)

	tx, ok := s.Tx("TX1")
	require.True(t, ok)
	require.Equal(t, int64(1), tx.Height)

	// alice touched both txs, bob only one — newest first.
	aliceTxs, total := s.TxsByAddress("ae1alice", 10, 0)
	require.Equal(t, 2, total)
	require.Equal(t, "TX2", aliceTxs[0].Hash)
	require.Equal(t, "TX1", aliceTxs[1].Hash)

	bobTxs, total := s.TxsByAddress("ae1bob", 10, 0)
	require.Equal(t, 1, total)
	require.Equal(t, "TX1", bobTxs[0].Hash)
}

func TestMemoryRecentListsAreNewestFirst(t *testing.T) {
	s := NewMemory(0)
	for h := int64(1); h <= 5; h++ {
		s.PutBlock(mkBlock(h, "TX"+strconv.FormatInt(h, 10)), []model.Tx{mkTx("TX"+strconv.FormatInt(h, 10), h)})
	}
	blocks, total := s.RecentBlocks(3, 0)
	require.Equal(t, 5, total)
	require.Len(t, blocks, 3)
	require.Equal(t, int64(5), blocks[0].Height)
	require.Equal(t, int64(3), blocks[2].Height)

	// offset paginates further back
	blocks2, _ := s.RecentBlocks(3, 3)
	require.Equal(t, int64(2), blocks2[0].Height)

	txs, txTotal := s.RecentTxs(2, 0)
	require.Equal(t, 5, txTotal)
	require.Equal(t, "TX5", txs[0].Hash)
}

func TestMemoryEvictsBeyondRetention(t *testing.T) {
	s := NewMemory(3) // retain only 3 most recent blocks
	for h := int64(1); h <= 6; h++ {
		s.PutBlock(mkBlock(h, "TX"+strconv.FormatInt(h, 10)), []model.Tx{mkTx("TX"+strconv.FormatInt(h, 10), h, "ae1carol")})
	}
	blocks, txs := s.Counts()
	require.Equal(t, 3, blocks)
	require.Equal(t, 3, txs)

	// evicted blocks/txs are gone
	_, ok := s.Block(1)
	require.False(t, ok)
	_, ok = s.Tx("TX1")
	require.False(t, ok)

	// retained ones remain
	_, ok = s.Block(6)
	require.True(t, ok)

	// address index shrank with eviction (only 3 recent txs left)
	carol, total := s.TxsByAddress("ae1carol", 100, 0)
	require.Equal(t, 3, total)
	require.Equal(t, "TX6", carol[0].Hash)
}

func TestMemoryReindexSameHeightDoesNotDouble(t *testing.T) {
	s := NewMemory(0)
	s.PutBlock(mkBlock(1, "TX1"), []model.Tx{mkTx("TX1", 1, "ae1a")})
	s.PutBlock(mkBlock(1, "TX1"), []model.Tx{mkTx("TX1", 1, "ae1a")}) // duplicate ingest
	blocks, txs := s.Counts()
	require.Equal(t, 1, blocks)
	require.Equal(t, 1, txs)
	aTxs, total := s.TxsByAddress("ae1a", 10, 0)
	require.Equal(t, 1, total)
	require.Len(t, aTxs, 1)
}
