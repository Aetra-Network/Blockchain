package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/explorer/model"
	"github.com/sovereign-l1/l1/explorer/store"
)

type fakeChain struct{}

func (fakeChain) Contracts(context.Context, uint32) (any, error) {
	return map[string]any{"contracts": []any{map[string]any{"address": "AEcontract1"}}, "count": 1}, nil
}
func (fakeChain) Contract(_ context.Context, addr string) (any, error) {
	return map[string]any{"found": true, "address": addr}, nil
}
func (fakeChain) Address(_ context.Context, addr string) (any, error) {
	return map[string]any{"valid": true, "address": addr, "kind": "wallet"}, nil
}
func (fakeChain) Validators(context.Context) (any, error) {
	return map[string]any{"validators": []any{}, "count": 0}, nil
}
func (fakeChain) Supply(context.Context) (any, error) {
	return map[string]any{"supply": []any{map[string]string{"denom": "naet", "amount": "100"}}}, nil
}

func seedServer(t *testing.T) *Server {
	t.Helper()
	s := store.NewMemory(0)
	s.PutBlock(
		model.Block{BlockSummary: model.BlockSummary{Height: 10, Hash: "BLKHASH10", NumTxs: 1}, TxHashes: []string{"TXA"}},
		[]model.Tx{{Hash: "TXA", Height: 10, Success: true, Addresses: []string{"ae1alice"},
			Messages: []model.MsgSummary{{TypeURL: "/l1.contracts.v1.MsgExecuteExternal"}}}},
	)
	return New(s, fakeChain{}, func(context.Context) (model.Status, error) {
		return model.Status{ChainID: "19", LatestHeight: 10}, nil
	})
}

func get(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestAPIRoutes(t *testing.T) {
	h := seedServer(t).Handler()

	code, body := get(t, h, "/status")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "19", body["chain_id"])
	require.EqualValues(t, 10, body["indexed_height"])
	require.EqualValues(t, 1, body["indexed_txs"])

	code, body = get(t, h, "/blocks/10")
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 10, body["height"])

	code, _ = get(t, h, "/blocks/999")
	require.Equal(t, http.StatusNotFound, code)

	code, body = get(t, h, "/txs/TXA")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, body["success"])

	code, body = get(t, h, "/accounts/ae1alice/txs")
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 1, body["total"])

	code, body = get(t, h, "/contracts")
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 1, body["count"])

	code, body = get(t, h, "/contracts/AEcontract1")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, body["found"])

	code, body = get(t, h, "/supply")
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, body["supply"])

	code, body = get(t, h, "/search?q=10")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "block", body["kind"])

	code, body = get(t, h, "/search?q=ae1alice")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "account", body["kind"])
}

func TestAPIDisablesChainRoutesWhenNil(t *testing.T) {
	s := store.NewMemory(0)
	h := New(s, nil, nil).Handler()
	code, _ := get(t, h, "/contracts")
	require.Equal(t, http.StatusNotImplemented, code)
	code, _ = get(t, h, "/validators")
	require.Equal(t, http.StatusNotImplemented, code)
	// block/tx index still serves
	code, _ = get(t, h, "/blocks")
	require.Equal(t, http.StatusOK, code)
}
