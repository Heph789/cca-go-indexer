package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// ---------------------------------------------------------------------------
// Test: Cache-Control headers on auction GET
// ---------------------------------------------------------------------------

func TestAuctionHandler_CacheHeaders(t *testing.T) {
	const wantCache = "public, max-age=86400, immutable"

	t.Run("sets cache header on 200 success", func(t *testing.T) {
		// AuctionCreated events are immutable on-chain data. Once indexed,
		// they never change, so we can aggressively cache successful responses.
		auction := newTestAuction()
		ms := &mockStore{auctionRepo: &mockAuctionRepo{
			GetByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return auction, nil
			},
		}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+validAddress(), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; want 200", rec.Code)
		}
		got := rec.Header().Get("Cache-Control")
		if got != wantCache {
			t.Errorf("Cache-Control = %q; want %q", got, wantCache)
		}
	})

	t.Run("does not set cache header on 404", func(t *testing.T) {
		// A missing auction might be indexed later, so we must not cache 404s.
		ms := &mockStore{auctionRepo: &mockAuctionRepo{
			GetByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return nil, nil
			},
		}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+validAddress(), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d; want 404", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q; want empty (no caching on 404)", got)
		}
	})

	t.Run("does not set cache header on 400", func(t *testing.T) {
		// Validation errors should not be cached.
		ms := &mockStore{auctionRepo: &mockAuctionRepo{}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/0xbad", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; want 400", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q; want empty (no caching on 400)", got)
		}
	})

	t.Run("does not set cache header on 500", func(t *testing.T) {
		// Server errors are transient and must not be cached.
		ms := &mockStore{auctionRepo: &mockAuctionRepo{
			GetByAddressFn: func(ctx context.Context, chainID int64, addr string) (*cca.Auction, error) {
				return nil, errors.New("db down")
			},
		}}
		mux := setupMux(ms)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auctions/"+validAddress(), nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d; want 500", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q; want empty (no caching on 500)", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Test: Cache-Control headers on health probes
// ---------------------------------------------------------------------------

func TestHealthHandler_CacheHeaders(t *testing.T) {
	t.Run("Health sets no-store", func(t *testing.T) {
		// Health probes must not be cached — stale cached 200s hide a dead process.
		h := &HealthHandler{Logger: slog.Default()}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		h.Health(rec, req)

		got := rec.Header().Get("Cache-Control")
		if got != "no-store" {
			t.Errorf("Cache-Control = %q; want %q", got, "no-store")
		}
	})

	t.Run("Ready sets no-store", func(t *testing.T) {
		// Readiness probes must not be cached — stale cached 200s would
		// route traffic to an instance with a broken DB connection.
		ms := &mockStore{
			auctionRepo: &mockAuctionRepo{},
			PingFn:      func(ctx context.Context) error { return nil },
		}
		h := &HealthHandler{Store: ms, Logger: slog.Default()}

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rec := httptest.NewRecorder()
		h.Ready(rec, req)

		got := rec.Header().Get("Cache-Control")
		if got != "no-store" {
			t.Errorf("Cache-Control = %q; want %q", got, "no-store")
		}
	})
}
