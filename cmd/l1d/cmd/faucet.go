package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
)

// Public testnet faucet: an off-chain HTTP service that signs and broadcasts
// ordinary bank-send transactions from an operator-funded key. There is
// deliberately no on-chain mint path (see docs/public-testnet-preparation.md
// Faucet Plan) — this reuses the exact tx build/sign/broadcast pipeline the
// `tx bank send` CLI uses (client/tx.Factory + client.Context.BroadcastTx),
// just wired to an HTTP handler with per-address/per-IP rate limiting instead
// of a one-shot CLI invocation.

const (
	flagFaucetServeListenAddr = "listen-addr"
	flagFaucetServeAmount     = "amount"
	flagFaucetServeCooldown   = "cooldown"

	defaultFaucetListenAddr = "127.0.0.1:8099"
	// 5 AET per grant: enough for ~10 transfers at the 0.5 AET average fee.
	defaultFaucetAmount   = "5000000000naet"
	defaultFaucetCooldown = 24 * time.Hour
)

func newFaucetServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the off-chain testnet faucet HTTP service (signs and broadcasts bank-send txs from --from)",
		Args:  cobra.NoArgs,
		RunE:  runFaucetServe,
	}
	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().String(flagFaucetServeListenAddr, defaultFaucetListenAddr, "HTTP listen address for the faucet service")
	cmd.Flags().String(flagFaucetServeAmount, defaultFaucetAmount, "fixed grant amount per successful request (coins, e.g. 1000000naet); the request body cannot override this")
	cmd.Flags().Duration(flagFaucetServeCooldown, defaultFaucetCooldown, "minimum time between grants to the same recipient address or client IP")
	return cmd
}

func runFaucetServe(cmd *cobra.Command, _ []string) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}
	// A long-running service can never satisfy an interactive stdin
	// confirmation prompt; skip it the same way `-y`/`--yes` does for the CLI.
	clientCtx = clientCtx.WithSkipConfirmation(true)
	if clientCtx.FromAddress.Empty() {
		return errors.New("faucet serve requires --from naming a funded keyring account")
	}

	listenAddr, err := cmd.Flags().GetString(flagFaucetServeListenAddr)
	if err != nil {
		return err
	}
	amountFlag, err := cmd.Flags().GetString(flagFaucetServeAmount)
	if err != nil {
		return err
	}
	grantAmount, err := sdk.ParseCoinsNormalized(amountFlag)
	if err != nil {
		return fmt.Errorf("invalid --%s: %w", flagFaucetServeAmount, err)
	}
	if grantAmount.IsZero() {
		return fmt.Errorf("--%s must be a positive amount", flagFaucetServeAmount)
	}
	cooldown, err := cmd.Flags().GetDuration(flagFaucetServeCooldown)
	if err != nil {
		return err
	}
	if cooldown <= 0 {
		return fmt.Errorf("--%s must be positive", flagFaucetServeCooldown)
	}

	logger := log.NewLogger(cmd.OutOrStdout())
	service := newFaucetService(clientCtx, cmd.Flags(), grantAmount, newFaucetRateLimiter(cooldown), logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", service.handleHealthz)
	mux.HandleFunc("/faucet", service.handleFaucet)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("faucet listening", "addr", listenAddr, "from", clientCtx.FromAddress.String(), "amount", grantAmount.String(), "cooldown", cooldown.String())
		serveErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

type faucetService struct {
	clientCtx   client.Context
	flagSet     *pflag.FlagSet
	grantAmount sdk.Coins
	limiter     *faucetRateLimiter
	logger      log.Logger
	// broadcastMu serializes broadcasts from the single faucet key so
	// concurrent HTTP requests cannot race on the same account sequence
	// number (each Factory.Prepare call fetches the account's current
	// on-chain sequence; two concurrent broadcasts would otherwise both
	// fetch the same sequence and one would be rejected by the mempool).
	broadcastMu sync.Mutex
	// broadcastFn defaults to s.broadcast; tests override it with a fake to
	// exercise the HTTP handler/rate-limiter without a live chain.
	broadcastFn func(ctx context.Context, recipient sdk.AccAddress) (string, error)
}

func newFaucetService(clientCtx client.Context, flagSet *pflag.FlagSet, grantAmount sdk.Coins, limiter *faucetRateLimiter, logger log.Logger) *faucetService {
	s := &faucetService{
		clientCtx:   clientCtx,
		flagSet:     flagSet,
		grantAmount: grantAmount,
		limiter:     limiter,
		logger:      logger,
	}
	s.broadcastFn = s.broadcast
	return s
}

func (s *faucetService) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

type faucetRequest struct {
	Address string `json:"address"`
}

type faucetResponse struct {
	TxHash string `json:"txhash"`
	Amount string `json:"amount"`
}

type faucetErrorResponse struct {
	Error string `json:"error"`
}

func (s *faucetService) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(faucetErrorResponse{Error: message})
}

func (s *faucetService) handleFaucet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}

	var req faucetRequest
	// Bound the body size so a malicious client cannot force unbounded
	// allocation from this always-on public endpoint.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body: expected {\"address\":\"...\"}")
		return
	}

	recipient, err := aetraaddress.ParseAccAddress(strings.TrimSpace(req.Address))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid recipient address")
		return
	}
	recipientText := recipient.String()

	clientIP := clientIPFromRequest(r)

	// Rate limit by BOTH the recipient address and the client IP: naming a
	// fresh address from the same IP repeatedly, or draining one address from
	// many IPs (e.g. behind a NAT-shared IP, an operator can widen the IP
	// cooldown check by fronting with a proxy that sets a stable per-user
	// header) is still bounded by the address-side check.
	if !s.limiter.Allow("addr:" + recipientText) {
		s.writeError(w, http.StatusTooManyRequests, "recipient address is rate-limited, try again later")
		return
	}
	if !s.limiter.Allow("ip:" + clientIP) {
		// Undo the address-side grant so a rate-limited IP doesn't burn the
		// recipient's cooldown window for a request that never sent funds.
		s.limiter.Release("addr:" + recipientText)
		s.writeError(w, http.StatusTooManyRequests, "client IP is rate-limited, try again later")
		return
	}

	txHash, err := s.broadcastFn(r.Context(), recipient)
	if err != nil {
		s.limiter.Release("addr:" + recipientText)
		s.limiter.Release("ip:" + clientIP)
		s.logger.Error("faucet broadcast failed", "recipient", recipientText, "ip", clientIP, "err", err)
		s.writeError(w, http.StatusInternalServerError, "broadcast failed")
		return
	}

	s.logger.Info("faucet grant sent", "recipient", recipientText, "ip", clientIP, "amount", s.grantAmount.String(), "txhash", txHash)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(faucetResponse{TxHash: txHash, Amount: s.grantAmount.String()})
}

// broadcast signs and sends a bank-send grant using the shared
// signAndBroadcast pipeline (see broadcast.go).
func (s *faucetService) broadcast(ctx context.Context, recipient sdk.AccAddress) (string, error) {
	s.broadcastMu.Lock()
	defer s.broadcastMu.Unlock()

	msg := banktypes.NewMsgSend(s.clientCtx.FromAddress, recipient, s.grantAmount)
	res, err := signAndBroadcast(ctx, s.clientCtx, s.flagSet, msg)
	if err != nil {
		return "", err
	}
	return res.TxHash, nil
}

func clientIPFromRequest(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// faucetRateLimiter tracks the last-granted timestamp per key (an address or
// an IP, distinguished by caller-chosen prefix) and enforces a minimum gap
// between grants. Allow both checks and reserves atomically so two concurrent
// requests for the same key cannot both pass. Release undoes a reservation
// when the request fails downstream for a reason unrelated to rate limiting,
// so a broadcast failure does not silently burn the caller's cooldown window.
type faucetRateLimiter struct {
	mu       sync.Mutex
	lastSeen map[string]time.Time
	cooldown time.Duration
}

func newFaucetRateLimiter(cooldown time.Duration) *faucetRateLimiter {
	return &faucetRateLimiter{
		lastSeen: make(map[string]time.Time),
		cooldown: cooldown,
	}
}

func (l *faucetRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if last, ok := l.lastSeen[key]; ok && now.Sub(last) < l.cooldown {
		return false
	}
	l.lastSeen[key] = now
	if len(l.lastSeen) > 100_000 {
		l.sweepLocked(now)
	}
	return true
}

func (l *faucetRateLimiter) Release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.lastSeen, key)
}

func (l *faucetRateLimiter) sweepLocked(now time.Time) {
	for key, last := range l.lastSeen {
		if now.Sub(last) >= l.cooldown {
			delete(l.lastSeen, key)
		}
	}
}
