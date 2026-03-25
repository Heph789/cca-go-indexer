// Package api provides the HTTP server for the CCA indexer API.
// Routing uses Go 1.22+ enhanced ServeMux (method+pattern) — no external router needed.
package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/cca/go-indexer/internal/api/handlers"
	"github.com/cca/go-indexer/internal/store"
)

// ServerConfig holds the settings that Server needs.
// Extracted from the full config so the API layer doesn't depend
// on indexer-specific fields.
type ServerConfig struct {
	Port    string
	ChainID int64
}

// Server wraps an http.Server and owns route configuration,
// middleware, and shared dependencies (store, logger).
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// NewServer creates a configured Server with all routes and middleware registered.
//
// Middleware is applied in order (outermost first):
//   cors → requestID → recovery → requestLogger → handler
//
// Health probes are registered outside the middleware chain so they
// stay fast and don't pollute request logs.
func NewServer(cfg ServerConfig, st store.Store, logger *slog.Logger) *Server {
	auctionHandler := &handlers.AuctionHandler{
		Store:   st,
		ChainID: cfg.ChainID,
		Logger:  logger,
	}
	healthHandler := &handlers.HealthHandler{
		Store:  st,
		Logger: logger,
	}

	// --- Route registration ---
	mux := http.NewServeMux()

	// Health probes (root level, not versioned — infrastructure concern)
	mux.HandleFunc("GET /health", healthHandler.Health)
	mux.HandleFunc("GET /ready", healthHandler.Ready)

	// Versioned API routes
	mux.HandleFunc("GET /api/v1/auctions/{address}", auctionHandler.Get)

	// --- Middleware chain ---
	// Applied bottom-up: cors wraps requestID wraps recovery wraps logger wraps mux
	var handler http.Handler = mux
	handler = requestLogger(logger)(handler)
	handler = recovery(logger)(handler)
	handler = requestID(handler)
	handler = cors(handler)

	return &Server{
		httpServer: &http.Server{
			Addr:    net.JoinHostPort("", cfg.Port),
			Handler: handler,

			// Defensive timeouts — prevent slow/malicious clients from
			// holding connections indefinitely.
			ReadTimeout:       5 * time.Second,
			ReadHeaderTimeout: 2 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
		logger: logger,
	}
}

// Start begins listening and serving HTTP requests.
// Blocks until the server stops. Returns http.ErrServerClosed on graceful shutdown.
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully drains in-flight requests within the given context deadline.
// Called from main.go's signal handler.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
