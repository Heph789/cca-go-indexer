package api_test

// Production Readiness QA Gate (#45)
//
// These tests verify that the full middleware chain, health probes, and
// cache headers work correctly when composed together as they are in
// production. Unlike unit tests (which exercise each middleware in
// isolation), these tests send HTTP requests through the complete stack:
//
//   cors → requestID → recovery → requestLogger → mux
//
// Each test documents what it verifies and why it matters in production.
//
// The test is designed to compile against both the current branch and
// the previous QA gate branch (resilience). It avoids importing types
// that only exist in the production phase (e.g. handlers.HealthHandler)
// and instead inlines equivalent route handlers so that the test
// exercises the middleware chain through NewServer.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/cca/go-indexer/internal/api"
	"github.com/cca/go-indexer/internal/api/httputil"
	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// ---------------------------------------------------------------------------
// Test infrastructure: mock store and helpers for the QA gate.
// ---------------------------------------------------------------------------

type qaAuctionRepo struct {
	getByAddressFn func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error)
}

func (m *qaAuctionRepo) Insert(ctx context.Context, a *cca.Auction) error { return nil }

func (m *qaAuctionRepo) DeleteFromBlock(ctx context.Context, chainID int64, from uint64) error {
	return nil
}

func (m *qaAuctionRepo) GetByAddress(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
	if m.getByAddressFn != nil {
		return m.getByAddressFn(ctx, chainID, addr)
	}
	return nil, nil
}

type qaStore struct {
	auctionRepo *qaAuctionRepo
	pingFn      func(ctx context.Context) error
}

func (m *qaStore) AuctionRepo() store.AuctionRepository  { return m.auctionRepo }
func (m *qaStore) RawEventRepo() store.RawEventRepository { return nil }
func (m *qaStore) CursorRepo() store.CursorRepository     { return nil }
func (m *qaStore) BlockRepo() store.BlockRepository       { return nil }
func (m *qaStore) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(m)
}
func (m *qaStore) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}
func (m *qaStore) Close() {}

const qaChainID int64 = 324

func qaValidAddress() string {
	return "0x1234567890abcdef1234567890abcdef12345678"
}

func qaTestAuction() *cca.Auction {
	return &cca.Auction{
		AuctionAddress:         common.HexToAddress("0xABcdEF1234567890abCDef1234567890AbCdEf12"),
		Token:                  common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Amount:                 big.NewInt(1000000),
		Currency:               common.HexToAddress("0x2222222222222222222222222222222222222222"),
		TokensRecipient:        common.HexToAddress("0x3333333333333333333333333333333333333333"),
		FundsRecipient:         common.HexToAddress("0x4444444444444444444444444444444444444444"),
		StartBlock:             100,
		EndBlock:               200,
		ClaimBlock:             300,
		TickSpacing:            big.NewInt(60),
		ValidationHook:         common.HexToAddress("0x5555555555555555555555555555555555555555"),
		FloorPrice:             big.NewInt(500),
		RequiredCurrencyRaised: big.NewInt(9999),
		ChainID:                qaChainID,
		BlockNumber:            50,
		TxHash:                 common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		LogIndex:               3,
	}
}

// newQAHandler builds the full middleware chain + mux exactly as production
// does (mirroring cmd/api/main.go) and returns the composed http.Handler.
// It registers health/ready/auction routes inline rather than importing
// handler types so the test compiles on the resilience branch too.
func newQAHandler(st *qaStore) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	mux := http.NewServeMux()

	// Health probe: always 200, no-store cache.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Readiness probe: checks DB via Ping, no-store cache.
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if err := st.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not_ready","reason":"database unreachable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

	// Auction GET with cache header on success only.
	mux.HandleFunc("GET /api/v1/auctions/{address}", func(w http.ResponseWriter, r *http.Request) {
		address := r.PathValue("address")
		if len(address) != 42 {
			httputil.WriteError(w, http.StatusBadRequest, httputil.CodeBadRequest, "invalid address")
			return
		}

		auction, err := st.AuctionRepo().GetByAddress(r.Context(), qaChainID, address)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, httputil.CodeInternalError, "internal error")
			return
		}
		if auction == nil {
			httputil.WriteNotFound(w, "auction")
			return
		}

		w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
		httputil.WriteJSON(w, http.StatusOK, httputil.Response{Data: auction})
	})

	srv := api.NewServer(api.ServerConfig{Port: "0"}, mux, logger)
	return srv.Handler()
}

// newQAPanicHandler builds the same stack but injects a panicking handler
// at the auction route to verify recovery middleware behavior.
func newQAPanicHandler(st *qaStore) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("GET /api/v1/auctions/{address}", func(w http.ResponseWriter, r *http.Request) {
		panic("unexpected nil pointer in handler")
	})

	srv := api.NewServer(api.ServerConfig{Port: "0"}, mux, logger)
	return srv.Handler()
}

// ---------------------------------------------------------------------------
// Required Verification R1: Full middleware chain headers on success
// ---------------------------------------------------------------------------

func TestQA_R1_FullChainHeadersOnSuccess(t *testing.T) {
	// Verifies that a successful auction GET response includes all three
	// production headers: CORS (Access-Control-Allow-Origin),
	// X-Request-ID, and Cache-Control. This confirms the middleware chain
	// is correctly composed and all layers contribute their headers.
	st := &qaStore{auctionRepo: &qaAuctionRepo{
		getByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
			return qaTestAuction(), nil
		},
	}}
	h := newQAHandler(st)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+qaValidAddress(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}

	// CORS header from cors middleware.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q; want %q", got, "*")
	}

	// Request ID from requestID middleware (auto-generated since we didn't send one).
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Error("X-Request-ID header is empty; expected a generated ID")
	}

	// Cache-Control from auction handler.
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=86400, immutable" {
		t.Errorf("Cache-Control = %q; want %q", got, "public, max-age=86400, immutable")
	}
}

// ---------------------------------------------------------------------------
// Required Verification R2: Readiness probe reflects DB state
// ---------------------------------------------------------------------------

func TestQA_R2_ReadinessProbeReflectsDBState(t *testing.T) {
	t.Run("returns 200 when DB is reachable", func(t *testing.T) {
		// When the database is healthy, /ready should return 200 so the
		// load balancer routes traffic to this instance.
		st := &qaStore{
			auctionRepo: &qaAuctionRepo{},
			pingFn:      func(ctx context.Context) error { return nil },
		}
		h := newQAHandler(st)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d; want 200", rec.Code)
		}
	})

	t.Run("returns 503 when DB is unreachable", func(t *testing.T) {
		// When the database is down, /ready must return 503 so the load
		// balancer stops sending traffic to this instance.
		st := &qaStore{
			auctionRepo: &qaAuctionRepo{},
			pingFn:      func(ctx context.Context) error { return errors.New("connection refused") },
		}
		h := newQAHandler(st)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d; want 503", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Required Verification R3: Liveness probe ignores DB state
// ---------------------------------------------------------------------------

func TestQA_R3_LivenessProbeIgnoresDB(t *testing.T) {
	// /health must always return 200, even when the DB is completely down.
	// If /health fails, the orchestrator restarts the pod — we don't want
	// a DB blip to trigger unnecessary restarts.
	st := &qaStore{
		auctionRepo: &qaAuctionRepo{},
		pingFn:      func(ctx context.Context) error { return errors.New("db gone") },
	}
	h := newQAHandler(st)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200 (health must not depend on DB)", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Required Verification R4: Cache-Control per status code
// ---------------------------------------------------------------------------

func TestQA_R4_CacheControlPerStatus(t *testing.T) {
	const wantCache = "public, max-age=86400, immutable"

	t.Run("200 success gets immutable cache header", func(t *testing.T) {
		// AuctionCreated events are immutable on-chain data. A successful
		// response can be cached for 24 hours with the immutable flag.
		st := &qaStore{auctionRepo: &qaAuctionRepo{
			getByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return qaTestAuction(), nil
			},
		}}
		h := newQAHandler(st)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+qaValidAddress(), nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; want 200", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != wantCache {
			t.Errorf("Cache-Control = %q; want %q", got, wantCache)
		}
	})

	t.Run("404 not found has no cache header", func(t *testing.T) {
		// A missing auction might be indexed later, so 404s must not be cached.
		st := &qaStore{auctionRepo: &qaAuctionRepo{
			getByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return nil, nil
			},
		}}
		h := newQAHandler(st)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+qaValidAddress(), nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d; want 404", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q; want empty (no caching on 404)", got)
		}
	})

	t.Run("400 bad request has no cache header", func(t *testing.T) {
		// Validation errors are transient (user can fix their input) — don't cache.
		st := &qaStore{auctionRepo: &qaAuctionRepo{}}
		h := newQAHandler(st)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/0xbad", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; want 400", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q; want empty (no caching on 400)", got)
		}
	})

	t.Run("500 server error has no cache header", func(t *testing.T) {
		// Server errors are transient and must not be cached.
		st := &qaStore{auctionRepo: &qaAuctionRepo{
			getByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return nil, errors.New("db timeout")
			},
		}}
		h := newQAHandler(st)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+qaValidAddress(), nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d; want 500", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q; want empty (no caching on 500)", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Agent Experiment A1: Panic recovery produces CORS headers + clean JSON 500
// ---------------------------------------------------------------------------

func TestQA_A1_PanicRecoveryWithCORS(t *testing.T) {
	// In production, if a handler panics the user should still see a clean
	// JSON error AND the CORS headers. Without this, a browser client
	// would get an opaque network error instead of a usable 500 response.
	st := &qaStore{auctionRepo: &qaAuctionRepo{}}
	h := newQAPanicHandler(st)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+qaValidAddress(), nil)
	rec := httptest.NewRecorder()

	// If recovery middleware is absent, the panic propagates to the test.
	// Catch it so we can report a clear failure instead of crashing.
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		h.ServeHTTP(rec, req)
	}()

	if panicked {
		t.Fatal("handler panic was not caught — recovery middleware is missing")
	}

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", rec.Code)
	}

	// CORS headers must still be present after a panic — they're set by
	// the outermost middleware before the panic occurs.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q; want %q (must survive panic)", got, "*")
	}
}

// ---------------------------------------------------------------------------
// Agent Experiment A2: OPTIONS preflight returns correct response
// ---------------------------------------------------------------------------

func TestQA_A2_OPTIONSPreflight(t *testing.T) {
	// Browsers send OPTIONS preflight requests before cross-origin API
	// calls. The response must be 204 with CORS headers, no body, and
	// no Cache-Control (preflight is not a data response).
	st := &qaStore{auctionRepo: &qaAuctionRepo{}}
	h := newQAHandler(st)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/auctions/"+qaValidAddress(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d; want 204 No Content for preflight", rec.Code)
	}

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q; want %q", got, "*")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, OPTIONS" {
		t.Errorf("Access-Control-Allow-Methods = %q; want %q", got, "GET, OPTIONS")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Request-ID" {
		t.Errorf("Access-Control-Allow-Headers = %q; want %q", got, "Content-Type, X-Request-ID")
	}

	// Preflight should NOT have Cache-Control headers — it's not a data response.
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control = %q; want empty on preflight", got)
	}
}

// ---------------------------------------------------------------------------
// Agent Experiment A3: Generated request IDs are unique
// ---------------------------------------------------------------------------

func TestQA_A3_GeneratedRequestIDsAreUnique(t *testing.T) {
	// When no X-Request-ID is provided, each request must get a unique ID.
	// Duplicate IDs would make request tracing useless.
	st := &qaStore{auctionRepo: &qaAuctionRepo{}}
	h := newQAHandler(st)

	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		id := rec.Header().Get("X-Request-ID")
		if id == "" {
			t.Fatalf("request %d: X-Request-ID is empty", i)
		}
		if ids[id] {
			t.Errorf("request %d: duplicate X-Request-ID %q", i, id)
		}
		ids[id] = true
	}
}

// ---------------------------------------------------------------------------
// Agent Experiment A4: Client-provided X-Request-ID survives full chain
// ---------------------------------------------------------------------------

func TestQA_A4_ClientRequestIDPreserved(t *testing.T) {
	// When a client (or upstream proxy) provides X-Request-ID, it must
	// survive the entire middleware chain and appear in the response.
	// This enables end-to-end distributed tracing.
	st := &qaStore{auctionRepo: &qaAuctionRepo{
		getByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
			return qaTestAuction(), nil
		},
	}}
	h := newQAHandler(st)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+qaValidAddress(), nil)
	req.Header.Set("X-Request-ID", "trace-abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "trace-abc-123" {
		t.Errorf("X-Request-ID = %q; want %q", got, "trace-abc-123")
	}
}

// ---------------------------------------------------------------------------
// Agent Experiment A5: Cache-Control doesn't leak between responses
// ---------------------------------------------------------------------------

func TestQA_A5_CacheControlNoLeakBetweenRequests(t *testing.T) {
	// Verify that a Cache-Control header set on a 200 response does not
	// leak into a subsequent 404 response. This could happen if headers
	// were set on a shared writer or global state.
	st := &qaStore{auctionRepo: &qaAuctionRepo{
		getByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
			if addr == "0x1234567890abcdef1234567890abcdef12345678" {
				return qaTestAuction(), nil
			}
			return nil, nil
		},
	}}
	h := newQAHandler(st)

	// First request: 200 with Cache-Control.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+qaValidAddress(), nil)
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d; want 200", rec1.Code)
	}
	if rec1.Header().Get("Cache-Control") == "" {
		t.Fatal("first request: expected Cache-Control header on 200")
	}

	// Second request: different address → 404, must have NO Cache-Control.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotFound {
		t.Fatalf("second request: status = %d; want 404", rec2.Code)
	}
	if got := rec2.Header().Get("Cache-Control"); got != "" {
		t.Errorf("second request: Cache-Control = %q; want empty (leaked from previous 200)", got)
	}
}

// ---------------------------------------------------------------------------
// Agent Experiment A6: Health probe through full chain gets all middleware headers
// ---------------------------------------------------------------------------

func TestQA_A6_HealthProbeFullChainHeaders(t *testing.T) {
	// /health should get the full middleware treatment: CORS for browser
	// health dashboards, X-Request-ID for tracing, and no-store for
	// freshness.
	st := &qaStore{auctionRepo: &qaAuctionRepo{}}
	h := newQAHandler(st)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q; want %q", got, "*")
	}
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Error("X-Request-ID is empty; health probe should get middleware headers")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q; want %q", got, "no-store")
	}
}

// ---------------------------------------------------------------------------
// Agent Experiment A7: Ready 503 includes CORS headers
// ---------------------------------------------------------------------------

func TestQA_A7_Ready503IncludesCORS(t *testing.T) {
	// Browser-based monitoring dashboards may call /ready cross-origin.
	// Even on 503, CORS headers must be present so the browser can read
	// the response status and body.
	st := &qaStore{
		auctionRepo: &qaAuctionRepo{},
		pingFn:      func(ctx context.Context) error { return errors.New("connection refused") },
	}
	h := newQAHandler(st)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q; want %q (must be set even on 503)", got, "*")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q; want %q (readiness must not be cached)", got, "no-store")
	}
}

// ---------------------------------------------------------------------------
// Agent Experiment A8: Recovery middleware produces valid JSON error body
// ---------------------------------------------------------------------------

func TestQA_A8_RecoveryProducesValidJSON(t *testing.T) {
	// When recovery catches a panic, the response must be valid JSON
	// matching the standard error envelope: {"error":{"code":"...","message":"..."}}.
	// A bare text response or dropped connection would break API clients.
	st := &qaStore{auctionRepo: &qaAuctionRepo{}}
	h := newQAPanicHandler(st)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+qaValidAddress(), nil)
	rec := httptest.NewRecorder()

	// Catch unrecovered panics so the test reports a clear failure.
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		h.ServeHTTP(rec, req)
	}()

	if panicked {
		t.Fatal("handler panic was not caught — recovery middleware is missing")
	}

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", rec.Code)
	}

	var body httputil.ErrorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Error.Code != httputil.CodeInternalError {
		t.Errorf("error.code = %q; want %q", body.Error.Code, httputil.CodeInternalError)
	}
	if body.Error.Message == "" {
		t.Error("error.message is empty; expected a human-readable message")
	}
}
