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
// cors -> requestID -> recovery -> requestLogger -> mux.
func NewServer(cfg ServerConfig, mux *http.ServeMux, logger *slog.Logger) *Server {
	handler := cors(
		requestID(logger)(
			recovery(logger)(
				requestLogger(logger)(mux),
			),
		),
	)

	return &Server{
		httpServer: &http.Server{
			Addr:              net.JoinHostPort("", cfg.Port),
			Handler:           handler,
			ReadTimeout:       5 * time.Second,
			ReadHeaderTimeout: 2 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
		logger: logger,
	}
}

func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
