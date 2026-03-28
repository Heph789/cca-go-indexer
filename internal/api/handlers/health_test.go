package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// Test: Health handler (liveness probe)
// ---------------------------------------------------------------------------

func TestHealthHandler_Health(t *testing.T) {
	t.Run("returns 200 with status ok", func(t *testing.T) {
		// The liveness probe always returns 200 if the process is alive.
		// No external dependencies are checked.
		h := &HealthHandler{Logger: slog.Default()}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		h.Health(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusOK)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if body["status"] != "ok" {
			t.Errorf("status = %q; want %q", body["status"], "ok")
		}
	})

	t.Run("sets Cache-Control no-store", func(t *testing.T) {
		// Health probes must not be cached by proxies or CDNs —
		// stale cached 200s would hide a dead process.
		h := &HealthHandler{Logger: slog.Default()}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		h.Health(rec, req)

		got := rec.Header().Get("Cache-Control")
		if got != "no-store" {
			t.Errorf("Cache-Control = %q; want %q", got, "no-store")
		}
	})
}

// ---------------------------------------------------------------------------
// Test: Ready handler (readiness probe)
// ---------------------------------------------------------------------------

func TestHealthHandler_Ready(t *testing.T) {
	t.Run("returns 200 with status ready when DB ping succeeds", func(t *testing.T) {
		// When the database is reachable, the readiness probe returns 200
		// so the load balancer routes traffic to this instance.
		ms := &mockStore{
			auctionRepo: &mockAuctionRepo{},
			PingFn:      func(ctx context.Context) error { return nil },
		}
		h := &HealthHandler{Store: ms, Logger: slog.Default()}

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rec := httptest.NewRecorder()

		h.Ready(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusOK)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if body["status"] != "ready" {
			t.Errorf("status = %q; want %q", body["status"], "ready")
		}
	})

	t.Run("returns 503 with not_ready when DB ping fails", func(t *testing.T) {
		// When the database is unreachable, the readiness probe returns 503
		// so the load balancer stops sending traffic.
		ms := &mockStore{
			auctionRepo: &mockAuctionRepo{},
			PingFn:      func(ctx context.Context) error { return errors.New("connection refused") },
		}
		h := &HealthHandler{Store: ms, Logger: slog.Default()}

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rec := httptest.NewRecorder()

		h.Ready(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusServiceUnavailable)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if body["status"] != "not_ready" {
			t.Errorf("status = %q; want %q", body["status"], "not_ready")
		}
		if body["reason"] != "database unreachable" {
			t.Errorf("reason = %q; want %q", body["reason"], "database unreachable")
		}
	})

	t.Run("sets Cache-Control no-store", func(t *testing.T) {
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
