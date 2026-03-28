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

// cors is an HTTP middleware that sets CORS headers on every response.
// It returns 204 No Content for OPTIONS preflight requests and passes
// all other requests through to the next handler.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// requestID returns middleware that reads the X-Request-ID header from the
// incoming request (or generates a new one) and stores it in the request
// context. It also sets the X-Request-ID response header and enriches the
// logger stored in context with a request_id attribute.
func requestID(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = generateID()
			}

			w.Header().Set("X-Request-ID", id)

			ctx := context.WithValue(r.Context(), requestIDKey, id)
			enrichedLogger := logger.With("request_id", id)
			ctx = WithLogger(ctx, enrichedLogger)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// generateID creates a random hex string suitable for use as a request ID.
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// statusWriter wraps http.ResponseWriter to capture the HTTP status code
// written by the handler.
type statusWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader captures the status code and delegates to the underlying
// ResponseWriter.
func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Write delegates to the underlying ResponseWriter. If WriteHeader has not
// been called yet, it defaults the captured status to 200 (matching the
// implicit behavior of net/http).
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// requestLogger returns middleware that logs the method, path, status, and
// duration of each HTTP request after it completes.
func requestLogger(_ *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			// Use the request-scoped logger from context (enriched with
			// request_id by the requestID middleware) so that request
			// completion logs include the request ID for correlation.
			ctxLogger := LoggerFromContext(r.Context())
			ctxLogger.Info("request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration", time.Since(start),
			)
		})
	}
}

// recovery returns middleware that catches panics from downstream handlers
// and returns a 500 Internal Server Error JSON response instead of crashing
// the server.
func recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered", "error", err)
					httputil.WriteError(w, http.StatusInternalServerError, httputil.CodeInternalError, "internal server error")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
