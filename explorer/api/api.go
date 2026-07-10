// Package api serves the explorer's read-only JSON HTTP API over an indexed
// store (blocks/txs/accounts) plus a live ChainQuerier for module state that
// is cheaper to read straight from the node than to index (contracts,
// validators, supply). Every response is plain JSON with permissive CORS so a
// static explorer frontend on any origin can consume it.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sovereign-l1/l1/explorer/model"
	"github.com/sovereign-l1/l1/explorer/store"
)

// ChainQuerier reads live module state from the node (gRPC). It is optional:
// a nil ChainQuerier disables the /contracts, /validators, and /supply routes
// (they return 501) but the block/tx index still serves.
type ChainQuerier interface {
	Contracts(ctx context.Context, limit uint32) (any, error)
	Contract(ctx context.Context, address string) (any, error)
	Address(ctx context.Context, address string) (any, error)
	Validators(ctx context.Context) (any, error)
	Supply(ctx context.Context) (any, error)
}

// StatusProvider supplies the live chain-tip fields the store does not hold.
type StatusProvider func(ctx context.Context) (model.Status, error)

// Server is the explorer HTTP handler.
type Server struct {
	store  store.Store
	chain  ChainQuerier
	status StatusProvider
}

func New(s store.Store, chain ChainQuerier, status StatusProvider) *Server {
	return &Server{store: s, chain: chain, status: status}
}

// Handler returns the routed http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/blocks", s.handleBlocks)
	mux.HandleFunc("/blocks/", s.handleBlockByID)
	mux.HandleFunc("/txs", s.handleTxs)
	mux.HandleFunc("/txs/", s.handleTxByHash)
	mux.HandleFunc("/accounts/", s.handleAccount)
	mux.HandleFunc("/address/", s.handleAddress)
	mux.HandleFunc("/contracts", s.handleContracts)
	mux.HandleFunc("/contracts/", s.handleContractByAddr)
	mux.HandleFunc("/validators", s.handleValidators)
	mux.HandleFunc("/supply", s.handleSupply)
	mux.HandleFunc("/search", s.handleSearch)
	return cors(mux)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "aetra-explorer",
		"routes": []string{
			"/status", "/blocks", "/blocks/{height|hash}", "/txs", "/txs/{hash}",
			"/accounts/{addr}/txs", "/contracts", "/contracts/{addr}",
			"/validators", "/supply", "/search?q=",
		},
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "indexed_height": s.store.LatestHeight()})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var st model.Status
	if s.status != nil {
		if live, err := s.status(r.Context()); err == nil {
			st = live
		}
	}
	blocks, txs := s.store.Counts()
	st.IndexedHeight = s.store.LatestHeight()
	st.IndexedBlocks = blocks
	st.IndexedTxs = txs
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleBlocks(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	items, total := s.store.RecentBlocks(limit, offset)
	writeJSON(w, http.StatusOK, model.Paged[model.BlockSummary]{Items: items, Total: total, Limit: limit, Offset: offset})
}

func (s *Server) handleBlockByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/blocks/")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "block id required")
		return
	}
	if h, err := strconv.ParseInt(id, 10, 64); err == nil {
		if b, ok := s.store.Block(h); ok {
			writeJSON(w, http.StatusOK, b)
			return
		}
	} else if b, ok := s.store.BlockByHash(strings.ToUpper(id)); ok {
		writeJSON(w, http.StatusOK, b)
		return
	}
	writeErr(w, http.StatusNotFound, "block not found")
}

func (s *Server) handleTxs(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	items, total := s.store.RecentTxs(limit, offset)
	writeJSON(w, http.StatusOK, model.Paged[model.TxSummary]{Items: items, Total: total, Limit: limit, Offset: offset})
}

func (s *Server) handleTxByHash(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/txs/"))
	if hash == "" {
		writeErr(w, http.StatusBadRequest, "tx hash required")
		return
	}
	if t, ok := s.store.Tx(hash); ok {
		writeJSON(w, http.StatusOK, t)
		return
	}
	writeErr(w, http.StatusNotFound, "tx not found")
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	// /accounts/{addr}/txs
	rest := strings.TrimPrefix(r.URL.Path, "/accounts/")
	parts := strings.SplitN(rest, "/", 2)
	addr := parts[0]
	if addr == "" {
		writeErr(w, http.StatusBadRequest, "account address required")
		return
	}
	if len(parts) < 2 || parts[1] != "txs" {
		writeErr(w, http.StatusNotFound, "use /accounts/{addr}/txs")
		return
	}
	limit, offset := pageParams(r)
	items, total := s.store.TxsByAddress(addr, limit, offset)
	writeJSON(w, http.StatusOK, model.Paged[model.TxSummary]{Items: items, Total: total, Limit: limit, Offset: offset})
}

// handleAddress serves the unified account/contract/system view for any
// address form (AE, raw 4:, system -7:). URL-decoded so "4:<hex>" and "-7:<hex>"
// pass through the path segment intact.
func (s *Server) handleAddress(w http.ResponseWriter, r *http.Request) {
	if s.chain == nil {
		writeErr(w, http.StatusNotImplemented, "live chain query disabled")
		return
	}
	addr := strings.TrimPrefix(r.URL.Path, "/address/")
	if decoded, err := url.PathUnescape(addr); err == nil {
		addr = decoded
	}
	if strings.TrimSpace(addr) == "" {
		writeErr(w, http.StatusBadRequest, "address required")
		return
	}
	data, err := s.chain.Address(r.Context(), addr)
	writeChain(w, data, err)
}

func (s *Server) handleContracts(w http.ResponseWriter, r *http.Request) {
	if s.chain == nil {
		writeErr(w, http.StatusNotImplemented, "live chain query disabled")
		return
	}
	limit, _ := pageParams(r)
	data, err := s.chain.Contracts(r.Context(), uint32(limit))
	writeChain(w, data, err)
}

func (s *Server) handleContractByAddr(w http.ResponseWriter, r *http.Request) {
	if s.chain == nil {
		writeErr(w, http.StatusNotImplemented, "live chain query disabled")
		return
	}
	addr := strings.TrimPrefix(r.URL.Path, "/contracts/")
	if addr == "" {
		writeErr(w, http.StatusBadRequest, "contract address required")
		return
	}
	data, err := s.chain.Contract(r.Context(), addr)
	writeChain(w, data, err)
}

func (s *Server) handleValidators(w http.ResponseWriter, r *http.Request) {
	if s.chain == nil {
		writeErr(w, http.StatusNotImplemented, "live chain query disabled")
		return
	}
	data, err := s.chain.Validators(r.Context())
	writeChain(w, data, err)
}

func (s *Server) handleSupply(w http.ResponseWriter, r *http.Request) {
	if s.chain == nil {
		writeErr(w, http.StatusNotImplemented, "live chain query disabled")
		return
	}
	data, err := s.chain.Supply(r.Context())
	writeChain(w, data, err)
}

// handleSearch resolves a query string to a block height, block/tx hash, or
// account so the frontend's single search box maps to one canonical route.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, http.StatusBadRequest, "q required")
		return
	}
	if h, err := strconv.ParseInt(q, 10, 64); err == nil {
		if _, ok := s.store.Block(h); ok {
			writeJSON(w, http.StatusOK, map[string]any{"kind": "block", "target": "/blocks/" + q})
			return
		}
	}
	up := strings.ToUpper(q)
	if _, ok := s.store.BlockByHash(up); ok {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "block", "target": "/blocks/" + up})
		return
	}
	if _, ok := s.store.Tx(up); ok {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "tx", "target": "/txs/" + up})
		return
	}
	// Any address form routes to the unified address page: AE user-friendly,
	// raw 4:<hex>, or system -7:<hex>.
	if strings.HasPrefix(q, "ae1") || strings.HasPrefix(q, "AE") ||
		strings.HasPrefix(q, "4:") || strings.HasPrefix(q, "-7:") {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "account", "target": "/address/" + url.PathEscape(q)})
		return
	}
	writeErr(w, http.StatusNotFound, "no match")
}

// --- helpers ---

func pageParams(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func writeChain(w http.ResponseWriter, data any, err error) {
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
