// Package store holds the indexed explorer data. The default implementation
// is an in-memory index (blocks + txs, keyed by height/hash/address) with a
// bounded retention window, so the explorer needs no external database to
// run — matching the project rule that off-chain services stay
// database-optional. A Postgres-backed Store can be added later behind this
// same interface without touching ingest or api.
package store

import (
	"sort"
	"sync"

	"github.com/sovereign-l1/l1/explorer/ingest"
	"github.com/sovereign-l1/l1/explorer/model"
)

// Store is the explorer's read/write index.
type Store interface {
	PutBlock(b model.Block, txs []model.Tx)
	LatestHeight() int64
	Counts() (blocks int, txs int)

	Block(height int64) (model.Block, bool)
	BlockByHash(hash string) (model.Block, bool)
	RecentBlocks(limit, offset int) ([]model.BlockSummary, int)

	Tx(hash string) (model.Tx, bool)
	RecentTxs(limit, offset int) ([]model.TxSummary, int)
	TxsByAddress(addr string, limit, offset int) ([]model.TxSummary, int)
}

// Memory is a thread-safe, bounded, in-memory Store.
type Memory struct {
	mu sync.RWMutex

	// maxBlocks bounds retained blocks (and their txs). 0 means unbounded.
	maxBlocks int

	blocks    map[int64]model.Block
	blockHash map[string]int64
	order     []int64 // ascending heights, for eviction + recent lists
	latest    int64

	txs     map[string]model.Tx
	txOrder []string // append order (ascending height/index)
	byAddr  map[string][]string
}

// NewMemory returns an in-memory store retaining the most recent maxBlocks
// blocks (0 = unbounded). A public explorer typically pairs a large window
// with periodic snapshotting; the default binary keeps a generous window.
func NewMemory(maxBlocks int) *Memory {
	return &Memory{
		maxBlocks: maxBlocks,
		blocks:    map[int64]model.Block{},
		blockHash: map[string]int64{},
		txs:       map[string]model.Tx{},
		byAddr:    map[string][]string{},
	}
}

func (m *Memory) PutBlock(b model.Block, txs []model.Tx) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.blocks[b.Height]; !exists {
		m.order = append(m.order, b.Height)
	}
	m.blocks[b.Height] = b
	m.blockHash[b.Hash] = b.Height
	if b.Height > m.latest {
		m.latest = b.Height
	}
	for _, tx := range txs {
		if _, dup := m.txs[tx.Hash]; !dup {
			m.txOrder = append(m.txOrder, tx.Hash)
		}
		m.txs[tx.Hash] = tx
		for _, addr := range tx.Addresses {
			// avoid duplicate hash under the same address
			if !containsLast(m.byAddr[addr], tx.Hash) {
				m.byAddr[addr] = append(m.byAddr[addr], tx.Hash)
			}
		}
	}
	m.evict()
}

// evict drops the oldest blocks (and their txs) beyond maxBlocks.
func (m *Memory) evict() {
	if m.maxBlocks <= 0 || len(m.order) <= m.maxBlocks {
		return
	}
	drop := len(m.order) - m.maxBlocks
	for _, h := range m.order[:drop] {
		blk, ok := m.blocks[h]
		if !ok {
			continue
		}
		for _, txHash := range blk.TxHashes {
			if tx, ok := m.txs[txHash]; ok {
				for _, addr := range tx.Addresses {
					m.byAddr[addr] = removeString(m.byAddr[addr], txHash)
					if len(m.byAddr[addr]) == 0 {
						delete(m.byAddr, addr)
					}
				}
			}
			delete(m.txs, txHash)
			m.txOrder = removeString(m.txOrder, txHash)
		}
		delete(m.blockHash, blk.Hash)
		delete(m.blocks, h)
	}
	m.order = append([]int64(nil), m.order[drop:]...)
}

func (m *Memory) LatestHeight() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.latest
}

func (m *Memory) Counts() (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.blocks), len(m.txs)
}

func (m *Memory) Block(height int64) (model.Block, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.blocks[height]
	return b, ok
}

func (m *Memory) BlockByHash(hash string) (model.Block, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.blockHash[hash]
	if !ok {
		return model.Block{}, false
	}
	b, ok := m.blocks[h]
	return b, ok
}

func (m *Memory) RecentBlocks(limit, offset int) ([]model.BlockSummary, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := len(m.order)
	// newest first
	heights := make([]int64, total)
	copy(heights, m.order)
	sort.Slice(heights, func(i, j int) bool { return heights[i] > heights[j] })
	out := []model.BlockSummary{}
	for _, h := range page(heights, limit, offset) {
		out = append(out, m.blocks[h].BlockSummary)
	}
	return out, total
}

func (m *Memory) Tx(hash string) (model.Tx, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.txs[hash]
	return t, ok
}

func (m *Memory) RecentTxs(limit, offset int) ([]model.TxSummary, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := len(m.txOrder)
	// newest first
	hashes := make([]string, total)
	for i, h := range m.txOrder {
		hashes[total-1-i] = h
	}
	out := []model.TxSummary{}
	for _, h := range pageStr(hashes, limit, offset) {
		out = append(out, ingest.SummarizeTx(m.txs[h]))
	}
	return out, total
}

func (m *Memory) TxsByAddress(addr string, limit, offset int) ([]model.TxSummary, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := m.byAddr[addr]
	total := len(all)
	// newest first
	rev := make([]string, total)
	for i, h := range all {
		rev[total-1-i] = h
	}
	out := []model.TxSummary{}
	for _, h := range pageStr(rev, limit, offset) {
		if t, ok := m.txs[h]; ok {
			out = append(out, ingest.SummarizeTx(t))
		}
	}
	return out, total
}

func page(s []int64, limit, offset int) []int64 {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(s) {
		return nil
	}
	end := offset + clampLimit(limit)
	if end > len(s) {
		end = len(s)
	}
	return s[offset:end]
}

func pageStr(s []string, limit, offset int) []string {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(s) {
		return nil
	}
	end := offset + clampLimit(limit)
	if end > len(s) {
		end = len(s)
	}
	return s[offset:end]
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func containsLast(s []string, v string) bool {
	return len(s) > 0 && s[len(s)-1] == v
}

func removeString(s []string, v string) []string {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}
