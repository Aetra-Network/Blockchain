package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
)

var errFaucetTestBroadcast = errors.New("simulated broadcast failure")

func TestFaucetRateLimiterAllowsFirstAndBlocksWithinCooldown(t *testing.T) {
	l := newFaucetRateLimiter(time.Hour)
	require.True(t, l.Allow("addr:x"), "first request must be allowed")
	require.False(t, l.Allow("addr:x"), "second request inside the cooldown must be blocked")
	require.True(t, l.Allow("addr:y"), "a different key is independent of x's cooldown")
}

func TestFaucetRateLimiterAllowsAfterCooldownElapses(t *testing.T) {
	l := newFaucetRateLimiter(10 * time.Millisecond)
	require.True(t, l.Allow("addr:x"))
	require.False(t, l.Allow("addr:x"))
	time.Sleep(20 * time.Millisecond)
	require.True(t, l.Allow("addr:x"), "must be allowed again once the cooldown window has passed")
}

func TestFaucetRateLimiterReleaseUndoesReservation(t *testing.T) {
	l := newFaucetRateLimiter(time.Hour)
	require.True(t, l.Allow("addr:x"))
	l.Release("addr:x")
	require.True(t, l.Allow("addr:x"), "a released key must be immediately available again")
}

func testFaucetRecipient(t *testing.T) sdk.AccAddress {
	t.Helper()
	addr, err := aetraaddress.ParseAccAddress(aeAddressForCLI(0x71))
	require.NoError(t, err)
	return addr
}

func newTestFaucetService(t *testing.T, cooldown time.Duration, broadcastFn func(ctx context.Context, recipient sdk.AccAddress) (string, error)) *faucetService {
	t.Helper()
	s := newFaucetService(client.Context{}, nil, sdk.NewCoins(sdk.NewInt64Coin("naet", 1_000_000)), newFaucetRateLimiter(cooldown), log.NewNopLogger())
	s.broadcastFn = broadcastFn
	return s
}

func TestFaucetHandlerGrantsFundsOnValidRequest(t *testing.T) {
	recipient := testFaucetRecipient(t)
	var gotRecipient sdk.AccAddress
	s := newTestFaucetService(t, time.Hour, func(_ context.Context, r sdk.AccAddress) (string, error) {
		gotRecipient = r
		return "ABCDEF0123", nil
	})

	body, err := json.Marshal(faucetRequest{Address: recipient.String()})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleFaucet(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, recipient.String(), gotRecipient.String())
	var resp faucetResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "ABCDEF0123", resp.TxHash)
}

func TestFaucetHandlerRejectsInvalidAddress(t *testing.T) {
	s := newTestFaucetService(t, time.Hour, func(context.Context, sdk.AccAddress) (string, error) {
		t.Fatal("broadcast must not be called for an invalid address")
		return "", nil
	})

	body, _ := json.Marshal(faucetRequest{Address: "not-a-valid-address"})
	req := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleFaucet(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestFaucetHandlerRejectsNonPostMethod(t *testing.T) {
	s := newTestFaucetService(t, time.Hour, func(context.Context, sdk.AccAddress) (string, error) {
		t.Fatal("broadcast must not be called for a non-POST request")
		return "", nil
	})

	req := httptest.NewRequest(http.MethodGet, "/faucet", nil)
	rec := httptest.NewRecorder()

	s.handleFaucet(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestFaucetHandlerRateLimitsRepeatedAddressRequests(t *testing.T) {
	recipient := testFaucetRecipient(t)
	calls := 0
	s := newTestFaucetService(t, time.Hour, func(context.Context, sdk.AccAddress) (string, error) {
		calls++
		return "TXHASH", nil
	})

	body, _ := json.Marshal(faucetRequest{Address: recipient.String()})

	req1 := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(body))
	rec1 := httptest.NewRecorder()
	s.handleFaucet(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	s.handleFaucet(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)

	require.Equal(t, 1, calls, "broadcast must run exactly once; the second request must be rejected before broadcasting")
}

func TestFaucetHandlerReleasesRateLimitOnBroadcastFailure(t *testing.T) {
	recipient := testFaucetRecipient(t)
	shouldFail := true
	s := newTestFaucetService(t, time.Hour, func(context.Context, sdk.AccAddress) (string, error) {
		if shouldFail {
			return "", errFaucetTestBroadcast
		}
		return "TXHASH", nil
	})

	body, _ := json.Marshal(faucetRequest{Address: recipient.String()})

	req1 := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(body))
	rec1 := httptest.NewRecorder()
	s.handleFaucet(rec1, req1)
	require.Equal(t, http.StatusInternalServerError, rec1.Code)

	shouldFail = false
	req2 := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	s.handleFaucet(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code, "a failed broadcast must not burn the recipient's cooldown window")
}

func TestFaucetHandlerHealthz(t *testing.T) {
	s := newTestFaucetService(t, time.Hour, func(context.Context, sdk.AccAddress) (string, error) {
		return "", nil
	})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.handleHealthz(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
