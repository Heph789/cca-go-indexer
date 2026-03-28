package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type ServerConfig struct {
	Port string
}

type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// NewServer creates a new Server with the middleware chain wired in the order:
// cors -> requestID -> recovery -> requestLogger -> appMux.
// Health probes (healthMux) bypass the middleware chain entirely.
func NewServer(cfg ServerConfig, appMux *http.ServeMux, healthMux *http.ServeMux, logger *slog.Logger) *Server {
	middlewareHandler := cors(
		requestID(logger)(
			recovery(logger)(
				requestLogger(logger)(appMux),
			),
		),
	)

	// Outer mux: health probes are served directly; everything else
	// goes through the middleware chain.
	outer := http.NewServeMux()
	outer.Handle("/health", healthMux)
	outer.Handle("/ready", healthMux)
	outer.Handle("/", middlewareHandler)

	return &Server{
		httpServer: &http.Server{
			Addr:              net.JoinHostPort("", cfg.Port),
			Handler:           outer,
			ReadTimeout:       5 * time.Second,
			ReadHeaderTimeout: 2 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
		logger: logger,
	}
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
