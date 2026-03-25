// Package handlers contains HTTP handler types for the API.
// Handlers are struct methods (not plain functions) because they
// need access to the store and logger.
package handlers

import (
	"log/slog"
	"net/http"

	"github.com/cca/go-indexer/internal/store"
)

// HealthHandler serves liveness and readiness probes.
// Readiness checks DB connectivity so the load balancer stops
// sending traffic when the database is unreachable.
type HealthHandler struct {
	Store  store.Store
	Logger *slog.Logger
}

// Health is the liveness probe — "is the process alive?"
// Always returns 200. If this fails, the orchestrator should restart the pod.
// Mounted at GET /health (root level, not under /api/v1).
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// Ready is the readiness probe — "can this instance serve traffic?"
// Checks database connectivity. Returns 503 if the DB is unreachable,
// which tells the load balancer to stop routing requests here.
// Mounted at GET /ready (root level, not under /api/v1).
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	// TODO: h.Store.Ping(r.Context()) — check DB connectivity
	// If err != nil → 503 {"status":"not_ready","reason":"database unreachable"}
	// Else → 200 {"status":"ready"}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ready"}`))
}
