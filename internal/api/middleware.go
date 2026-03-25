package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/cca/go-indexer/internal/api/httputil"
)

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys from other packages.
type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDFromContext extracts the request ID set by the requestID middleware.
// Returns empty string if not present (e.g. health probes that skip middleware).
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// requestID assigns a unique ID to each request. If the client sends
// X-Request-ID, we propagate it (useful for distributed tracing);
// otherwise we generate one. The ID is set on the response header
// and stored in context for downstream logging.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = generateID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// generateID produces a short random hex string for request IDs.
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// requestLogger logs each request's method, path, status, and duration.
// Uses structured slog fields so logs are machine-parseable.
// The status is captured via a responseWriter wrapper.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
			)
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code
// for logging. WriteHeader is called at most once per request.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// recovery catches panics in handlers and returns a 500 JSON error
// instead of crashing the process. Logs the panic value and stack
// at error level so it shows up in alerting.
func recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						"error", rec,
						"path", r.URL.Path,
						"request_id", RequestIDFromContext(r.Context()),
					)
					httputil.WriteError(w, http.StatusInternalServerError,
						httputil.CodeInternalError, "an unexpected error occurred")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// cors sets permissive CORS headers for browser clients.
// For production, AllowOrigin should be restricted to known frontends
// via configuration rather than wildcard.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: make origin configurable via config.AllowedOrigins
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
