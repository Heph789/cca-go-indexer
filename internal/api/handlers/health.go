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
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// Ready is the readiness probe — "can this instance serve traffic?"
// Checks database connectivity via Store.Ping. Returns 503 if the DB
// is unreachable, which tells the load balancer to stop routing requests here.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")

	if err := h.Store.Ping(r.Context()); err != nil {
		h.Logger.Error("readiness check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"not_ready","reason":"database unreachable"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ready"}`))
}
