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

func NewServer(cfg ServerConfig, mux *http.ServeMux, logger *slog.Logger) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:         net.JoinHostPort("", cfg.Port),
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  120 * time.Second,
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
