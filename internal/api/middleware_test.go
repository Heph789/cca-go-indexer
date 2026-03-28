package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cca/go-indexer/internal/api/httputil"
)

// ---------------------------------------------------------------------------
// captureHandler is a slog.Handler that records every log record it receives.
// This lets tests inspect structured log output without parsing text.
// ---------------------------------------------------------------------------

type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{}
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(_ string) slog.Handler { return h }

// getRecords returns a snapshot of captured records.
func (h *captureHandler) getRecords() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

// ---------------------------------------------------------------------------
// nopHandler is a trivial http.Handler that writes 200 OK with a known body.
// Most middleware tests wrap this to verify pass-through behavior.
// ---------------------------------------------------------------------------

func nopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})
}

// ---------------------------------------------------------------------------
// Test: cors middleware
// ---------------------------------------------------------------------------

func TestCors(t *testing.T) {
	t.Run("sets Access-Control-Allow-Origin header", func(t *testing.T) {
		// cors should set Access-Control-Allow-Origin: * on every response,
		// regardless of the request method.
		handler := cors(nopHandler())
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		got := rec.Header().Get("Access-Control-Allow-Origin")
		if got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q; want %q", got, "*")
		}
	})

	t.Run("sets Access-Control-Allow-Methods header", func(t *testing.T) {
		// The allow-methods header tells browsers which HTTP methods the API
		// supports for cross-origin requests.
		handler := cors(nopHandler())
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		got := rec.Header().Get("Access-Control-Allow-Methods")
		if got != "GET, OPTIONS" {
			t.Errorf("Access-Control-Allow-Methods = %q; want %q", got, "GET, OPTIONS")
		}
	})

	t.Run("returns 204 for OPTIONS preflight", func(t *testing.T) {
		// Browsers send OPTIONS preflight requests before cross-origin calls.
		// The middleware should short-circuit with 204 No Content so the
		// actual handler is never invoked.
		called := false
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		})
		handler := cors(inner)

		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusNoContent)
		}
		if called {
			t.Error("inner handler was called for OPTIONS; should be short-circuited")
		}
	})

	t.Run("passes through non-OPTIONS requests", func(t *testing.T) {
		// For normal requests the cors middleware must delegate to the next
		// handler so that the response body is produced as expected.
		handler := cors(nopHandler())
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), `"ok":true`) {
			t.Errorf("body = %q; want inner handler output", rec.Body.String())
		}
	})
}

// ---------------------------------------------------------------------------
// Test: requestID middleware
// ---------------------------------------------------------------------------

func TestRequestID(t *testing.T) {
	logger := slog.New(newCaptureHandler())

	t.Run("generates ID when X-Request-ID header absent", func(t *testing.T) {
		// When the client does not supply a request ID, the middleware must
		// generate one automatically so every request is traceable.
		handler := requestID(logger)(nopHandler())
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		got := rec.Header().Get("X-Request-ID")
		if got == "" {
			t.Fatal("X-Request-ID response header is empty; expected a generated ID")
		}
	})

	t.Run("propagates client-provided X-Request-ID", func(t *testing.T) {
		// When the client provides an X-Request-ID, the middleware should
		// reuse it rather than generating a new one. This enables end-to-end
		// tracing across services.
		handler := requestID(logger)(nopHandler())
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", "client-id-123")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		got := rec.Header().Get("X-Request-ID")
		if got != "client-id-123" {
			t.Errorf("X-Request-ID = %q; want %q", got, "client-id-123")
		}
	})

	t.Run("sets X-Request-ID on response", func(t *testing.T) {
		// The response must always include X-Request-ID so callers can
		// correlate responses with their request logs.
		handler := requestID(logger)(nopHandler())
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("X-Request-ID") == "" {
			t.Error("X-Request-ID not set on response")
		}
	})

	t.Run("stores ID in context via RequestIDFromContext", func(t *testing.T) {
		// Downstream handlers should be able to retrieve the request ID from
		// context so they can include it in their own log entries or error
		// responses.
		var ctxID string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctxID = RequestIDFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})
		handler := requestID(logger)(inner)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", "ctx-test-456")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if ctxID != "ctx-test-456" {
			t.Errorf("RequestIDFromContext = %q; want %q", ctxID, "ctx-test-456")
		}
	})

	t.Run("stores request-scoped logger in context", func(t *testing.T) {
		// The middleware should inject a logger into context that includes
		// the request ID as a structured field. This ensures every log line
		// produced by downstream handlers is automatically tagged.
		var ctxLogger *slog.Logger
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctxLogger = LoggerFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})
		handler := requestID(logger)(inner)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", "logger-test-789")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if ctxLogger == nil {
			t.Fatal("LoggerFromContext returned nil")
		}
		// The context logger should differ from the base logger because it
		// has been enriched with request-scoped attributes.
		if ctxLogger == logger {
			t.Error("context logger is the same instance as the base logger; expected enriched logger")
		}
	})
}

// ---------------------------------------------------------------------------
// Test: recovery middleware
// ---------------------------------------------------------------------------

func TestRecovery(t *testing.T) {
	logger := slog.New(newCaptureHandler())

	t.Run("returns 500 JSON error when handler panics", func(t *testing.T) {
		// If a handler panics, the recovery middleware must catch it and
		// return a well-formed 500 JSON error instead of crashing the server.
		panicker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("something went wrong")
		})
		handler := recovery(logger)(panicker)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusInternalServerError)
		}

		// The response body should be valid JSON matching the ErrorBody
		// structure from httputil.
		var body httputil.ErrorBody
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if body.Error.Code != httputil.CodeInternalError {
			t.Errorf("error code = %q; want %q", body.Error.Code, httputil.CodeInternalError)
		}
	})

	t.Run("passes through when handler does not panic", func(t *testing.T) {
		// Normal (non-panicking) requests should pass through the recovery
		// middleware completely unmodified.
		handler := recovery(logger)(nopHandler())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), `"ok":true`) {
			t.Errorf("body = %q; want inner handler output", rec.Body.String())
		}
	})
}

// ---------------------------------------------------------------------------
// Test: statusWriter
// ---------------------------------------------------------------------------

func TestStatusWriter(t *testing.T) {
	t.Run("WriteHeader captures status code", func(t *testing.T) {
		// statusWriter wraps http.ResponseWriter to intercept WriteHeader
		// calls so the middleware can inspect the status after the handler
		// completes. Verify it captures the code correctly.
		rec := httptest.NewRecorder()
		sw := &statusWriter{ResponseWriter: rec}

		sw.WriteHeader(http.StatusCreated)

		if sw.status != http.StatusCreated {
			t.Errorf("status = %d; want %d", sw.status, http.StatusCreated)
		}
		// The underlying ResponseWriter should also receive the status.
		if rec.Code != http.StatusCreated {
			t.Errorf("underlying recorder status = %d; want %d", rec.Code, http.StatusCreated)
		}
	})
}

// ---------------------------------------------------------------------------
// Test: requestLogger middleware
// ---------------------------------------------------------------------------

func TestRequestLogger(t *testing.T) {
	t.Run("logs method path status and duration", func(t *testing.T) {
		// The request logger should emit a structured log entry after each
		// request containing at minimum: method, path, status, and duration.
		ch := newCaptureHandler()
		logger := slog.New(ch)

		// Use a handler that returns 201 so we can verify the logged status
		// is not just a default value.
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})
		handler := requestLogger(logger)(inner)

		req := httptest.NewRequest(http.MethodPost, "/test/path", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		records := ch.getRecords()
		if len(records) == 0 {
			t.Fatal("no log records captured; expected at least one")
		}

		// Examine the last record (the request-complete log entry).
		last := records[len(records)-1]

		// Collect all attributes from the record into a map for easy lookup.
		attrs := make(map[string]any)
		last.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.Any()
			return true
		})

		// Verify method is logged.
		if v, ok := attrs["method"]; !ok {
			t.Error("log record missing 'method' attribute")
		} else if v != http.MethodPost {
			t.Errorf("method = %v; want %q", v, http.MethodPost)
		}

		// Verify path is logged.
		if v, ok := attrs["path"]; !ok {
			t.Error("log record missing 'path' attribute")
		} else if v != "/test/path" {
			t.Errorf("path = %v; want %q", v, "/test/path")
		}

		// Verify status is logged. Accept int64 or int since slog may store
		// the value as either.
		if v, ok := attrs["status"]; !ok {
			t.Error("log record missing 'status' attribute")
		} else {
			var status int64
			switch s := v.(type) {
			case int:
				status = int64(s)
			case int64:
				status = s
			default:
				t.Fatalf("status attr has unexpected type %T", v)
			}
			if status != http.StatusCreated {
				t.Errorf("status = %d; want %d", status, http.StatusCreated)
			}
		}

		// Verify duration is logged and is a positive value.
		if v, ok := attrs["duration"]; !ok {
			t.Error("log record missing 'duration' attribute")
		} else {
			switch d := v.(type) {
			case time.Duration:
				if d <= 0 {
					t.Errorf("duration = %v; want positive", d)
				}
			case string:
				// Some implementations log duration as a formatted string.
				if d == "" || d == "0s" {
					t.Errorf("duration = %q; want non-zero", d)
				}
			default:
				// Accept any non-nil value; the important thing is it exists.
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Test: full middleware chain
// ---------------------------------------------------------------------------

func TestMiddlewareChain(t *testing.T) {
	t.Run("cors -> requestID -> recovery -> logger produces correct headers", func(t *testing.T) {
		// Verify the full middleware stack works together. The chain order is:
		//   cors -> requestID -> recovery -> requestLogger -> handler
		// After processing, the response should include both CORS headers
		// and the X-Request-ID header, and the handler should execute
		// normally.
		ch := newCaptureHandler()
		logger := slog.New(ch)

		// The inner handler verifies it can retrieve the request ID from
		// context, proving the entire chain ran in order.
		var gotCtxID string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotCtxID = RequestIDFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"chain":"ok"}`))
		})

		// Build the chain: outermost middleware wraps first.
		handler := cors(
			requestID(logger)(
				recovery(logger)(
					requestLogger(logger)(inner),
				),
			),
		)

		req := httptest.NewRequest(http.MethodGet, "/chain-test", nil)
		req.Header.Set("X-Request-ID", "chain-id-001")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// Verify status.
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d; want %d", rec.Code, http.StatusOK)
		}

		// Verify CORS headers are present.
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q; want %q", got, "*")
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, OPTIONS" {
			t.Errorf("Access-Control-Allow-Methods = %q; want %q", got, "GET, OPTIONS")
		}

		// Verify X-Request-ID header is on the response.
		if got := rec.Header().Get("X-Request-ID"); got != "chain-id-001" {
			t.Errorf("X-Request-ID = %q; want %q", got, "chain-id-001")
		}

		// Verify the handler received the request ID through context.
		if gotCtxID != "chain-id-001" {
			t.Errorf("RequestIDFromContext in handler = %q; want %q", gotCtxID, "chain-id-001")
		}

		// Verify the body from the inner handler made it through.
		if !strings.Contains(rec.Body.String(), `"chain":"ok"`) {
			t.Errorf("body = %q; want inner handler output", rec.Body.String())
		}
	})
}
