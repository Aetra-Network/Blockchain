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
	s := newFaucetService(client.Context{}, nil, sdk.NewCoins(sdk.NewInt64Coin("naet", 1_000_000)), newFaucetRateLimiter(cooldown), log.NewNopLogger(), nil)
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

// --- security-audit FINDING-015: reverse-proxy IP rate-limit regression tests ---

func TestClientIPFromRequestUsesDirectPeerWhenNoTrustedProxyConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/faucet", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("X-Real-IP", "198.51.100.7")

	require.Equal(t, "203.0.113.9", clientIPFromRequest(req, nil),
		"with no trusted-proxy configuration, forwarded headers must never be honored")
}

func TestClientIPFromRequestIgnoresHeadersFromUntrustedPeer(t *testing.T) {
	trusted, err := parseTrustedProxies([]string{"203.0.113.9/32"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/faucet", nil)
	req.RemoteAddr = "192.0.2.55:1234" // not in the trusted list
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	require.Equal(t, "192.0.2.55", clientIPFromRequest(req, trusted),
		"a peer outside the trusted-proxy allowlist must not have its forwarded header honored")
}

func TestClientIPFromRequestHonorsForwardedForFromTrustedProxy(t *testing.T) {
	trusted, err := parseTrustedProxies([]string{"203.0.113.9/32"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/faucet", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	require.Equal(t, "198.51.100.7", clientIPFromRequest(req, trusted))
}

func TestClientIPFromRequestPrefersXRealIPOverForwardedFor(t *testing.T) {
	trusted, err := parseTrustedProxies([]string{"203.0.113.9"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/faucet", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.Header.Set("X-Real-IP", "198.51.100.42")
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	require.Equal(t, "198.51.100.42", clientIPFromRequest(req, trusted))
}

func TestClientIPFromRequestUsesRightmostForwardedForEntry(t *testing.T) {
	trusted, err := parseTrustedProxies([]string{"203.0.113.9"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/faucet", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	// The left-most entry can be attacker-supplied; only the entry the
	// trusted (nearest) hop appended is authoritative.
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.9")

	require.Equal(t, "203.0.113.9", clientIPFromRequest(req, trusted))
}

func TestParseTrustedProxiesRejectsInvalidValues(t *testing.T) {
	_, err := parseTrustedProxies([]string{"not-an-ip"})
	require.Error(t, err)
}

// TestFaucetHandlerBehindTrustedProxyRateLimitsPerRealClientNotPerProxy is the
// finding's recommended regression test: simulate requests behind a reverse
// proxy (identical RemoteAddr, distinct recipients/forwarded client IPs) and
// confirm each real client gets its own grant once a trusted-proxy config is
// wired in, instead of the whole cooldown window collapsing to a single
// global grant.
func TestFaucetHandlerBehindTrustedProxyRateLimitsPerRealClientNotPerProxy(t *testing.T) {
	trusted, err := parseTrustedProxies([]string{"203.0.113.9/32"})
	require.NoError(t, err)

	calls := 0
	s := newTestFaucetService(t, time.Hour, func(context.Context, sdk.AccAddress) (string, error) {
		calls++
		return "TXHASH", nil
	})
	s.trustedProxies = trusted

	recipientA, err := aetraaddress.ParseAccAddress(aeAddressForCLI(0x71))
	require.NoError(t, err)
	recipientB, err := aetraaddress.ParseAccAddress(aeAddressForCLI(0x72))
	require.NoError(t, err)

	bodyA, err := json.Marshal(faucetRequest{Address: recipientA.String()})
	require.NoError(t, err)
	reqA := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(bodyA))
	reqA.RemoteAddr = "203.0.113.9:11111" // shared reverse-proxy peer address
	reqA.Header.Set("X-Forwarded-For", "198.51.100.1")
	recA := httptest.NewRecorder()
	s.handleFaucet(recA, reqA)
	require.Equal(t, http.StatusOK, recA.Code)

	bodyB, err := json.Marshal(faucetRequest{Address: recipientB.String()})
	require.NoError(t, err)
	reqB := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(bodyB))
	reqB.RemoteAddr = "203.0.113.9:22222" // same proxy peer, different ephemeral port
	reqB.Header.Set("X-Forwarded-For", "198.51.100.2")
	recB := httptest.NewRecorder()
	s.handleFaucet(recB, reqB)
	require.Equal(t, http.StatusOK, recB.Code,
		"a different real client behind the same trusted proxy must still get its own grant")

	require.Equal(t, 2, calls)
}

// TestFaucetHandlerBehindUntrustedProxyStillCollapsesToOneGrant is the
// safe-by-default counterpart: without --trusted-proxy configuration, two
// distinct recipients arriving through what looks like a proxy (same
// RemoteAddr) must still collapse to a single IP-side grant per cooldown, so
// operators who forget to configure trusted proxies keep today's safe (if
// degraded) behavior rather than silently trusting spoofable headers.
func TestFaucetHandlerBehindUntrustedProxyStillCollapsesToOneGrant(t *testing.T) {
	calls := 0
	s := newTestFaucetService(t, time.Hour, func(context.Context, sdk.AccAddress) (string, error) {
		calls++
		return "TXHASH", nil
	})

	recipientA, err := aetraaddress.ParseAccAddress(aeAddressForCLI(0x71))
	require.NoError(t, err)
	recipientB, err := aetraaddress.ParseAccAddress(aeAddressForCLI(0x72))
	require.NoError(t, err)

	bodyA, err := json.Marshal(faucetRequest{Address: recipientA.String()})
	require.NoError(t, err)
	reqA := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(bodyA))
	reqA.RemoteAddr = "203.0.113.9:11111"
	reqA.Header.Set("X-Forwarded-For", "198.51.100.1")
	recA := httptest.NewRecorder()
	s.handleFaucet(recA, reqA)
	require.Equal(t, http.StatusOK, recA.Code)

	bodyB, err := json.Marshal(faucetRequest{Address: recipientB.String()})
	require.NoError(t, err)
	reqB := httptest.NewRequest(http.MethodPost, "/faucet", bytes.NewReader(bodyB))
	reqB.RemoteAddr = "203.0.113.9:22222"
	reqB.Header.Set("X-Forwarded-For", "198.51.100.2")
	recB := httptest.NewRecorder()
	s.handleFaucet(recB, reqB)
	require.Equal(t, http.StatusTooManyRequests, recB.Code)

	require.Equal(t, 1, calls)
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
