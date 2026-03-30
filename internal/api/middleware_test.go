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

// captureStore holds the shared state for captureHandler instances.
// Multiple captureHandler instances (created via WithAttrs) share the
// same store so all records are collected in one place.
type captureStore struct {
	mu      sync.Mutex
	records []slog.Record
}

type captureHandler struct {
	store *captureStore
	// preAttrs are attributes added via WithAttrs, prepended to each record.
	preAttrs []slog.Attr
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{store: &captureStore{}}
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	// Prepend any pre-configured attrs so that attributes added via
	// logger.With() appear on captured records.
	if len(h.preAttrs) > 0 {
		r.AddAttrs(h.preAttrs...)
	}
	h.store.records = append(h.store.records, r)
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Return a new captureHandler that shares the same store but has
	// the additional attrs prepended to every record it handles.
	combined := make([]slog.Attr, len(h.preAttrs)+len(attrs))
	copy(combined, h.preAttrs)
	copy(combined[len(h.preAttrs):], attrs)
	return &captureHandler{store: h.store, preAttrs: combined}
}

func (h *captureHandler) WithGroup(_ string) slog.Handler { return h }

// getRecords returns a snapshot of captured records.
func (h *captureHandler) getRecords() []slog.Record {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	out := make([]slog.Record, len(h.store.records))
	copy(out, h.store.records)
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
		if got != "GET, POST, OPTIONS" {
			t.Errorf("Access-Control-Allow-Methods = %q; want %q", got, "GET, POST, OPTIONS")
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

	t.Run("Write without WriteHeader defaults status to 200", func(t *testing.T) {
		// When a handler calls Write without an explicit WriteHeader, the
		// Go HTTP library implicitly sends 200. statusWriter should also
		// reflect 200 so the request logger records the correct status.
		rec := httptest.NewRecorder()
		sw := &statusWriter{ResponseWriter: rec}

		sw.Write([]byte("hello"))

		wantStatus := http.StatusOK
		if sw.status != wantStatus {
			t.Errorf("status = %d; want %d", sw.status, wantStatus)
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
		// Store the logger in context so requestLogger can retrieve it.
		req = req.WithContext(WithLogger(req.Context(), logger))
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

	t.Run("uses request-scoped logger from context for request_id correlation", func(t *testing.T) {
		// When requestID middleware enriches the context logger with a
		// request_id attribute, requestLogger should use that context logger
		// so that request completion logs include the request_id field.
		ch := newCaptureHandler()
		logger := slog.New(ch)

		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Build chain: requestID -> requestLogger -> inner
		// requestID injects an enriched logger into context; requestLogger
		// should pick it up from context.
		handler := requestID(logger)(requestLogger(logger)(inner))

		req := httptest.NewRequest(http.MethodGet, "/correlated", nil)
		req.Header.Set("X-Request-ID", "corr-test-001")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		records := ch.getRecords()
		if len(records) == 0 {
			t.Fatal("no log records captured")
		}

		// Find the "request completed" log entry and check it has request_id.
		var found bool
		for _, r := range records {
			if r.Message == "request completed" {
				found = true
				var hasRequestID bool
				r.Attrs(func(a slog.Attr) bool {
					if a.Key == "request_id" {
						hasRequestID = true
						if a.Value.String() != "corr-test-001" {
							t.Errorf("request_id = %q; want %q", a.Value.String(), "corr-test-001")
						}
						return false
					}
					return true
				})
				if !hasRequestID {
					t.Error("request completed log missing 'request_id' attribute; requestLogger should use context logger")
				}
			}
		}
		if !found {
			t.Error("no 'request completed' log record found")
		}
	})

	t.Run("logs status 200 when handler calls Write without WriteHeader", func(t *testing.T) {
		// When a handler calls Write() without explicitly calling WriteHeader(),
		// Go's net/http implicitly sends 200. The statusWriter should report
		// 200 (not 0) in the log entry.
		ch := newCaptureHandler()
		logger := slog.New(ch)

		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"implicit":true}`))
		})
		handler := requestLogger(logger)(inner)

		req := httptest.NewRequest(http.MethodGet, "/implicit-200", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		records := ch.getRecords()
		if len(records) == 0 {
			t.Fatal("no log records captured")
		}

		last := records[len(records)-1]
		attrs := make(map[string]any)
		last.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.Any()
			return true
		})

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
			wantStatus := int64(http.StatusOK)
			if status != wantStatus {
				t.Errorf("status = %d; want %d (implicit 200 when Write called without WriteHeader)", status, wantStatus)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Test: full middleware chain
// ---------------------------------------------------------------------------

// TestHealthProbesBypassMiddleware verifies that health and readiness probes
// served through NewServer do not pass through the middleware chain. This is
// important because probes should be fast, dependency-free, and should not
// produce request logs that drown out real traffic.
func TestHealthProbesBypassMiddleware(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"health probe bypasses middleware", "/health"},
		{"readiness probe bypasses middleware", "/ready"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := newCaptureHandler()
			logger := slog.New(ch)

			// App mux — no routes needed; we're testing health paths.
			appMux := http.NewServeMux()

			// Health mux with a simple handler that writes a response.
			healthMux := http.NewServeMux()
			healthMux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status":"ok"}`))
			})
			healthMux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status":"ready"}`))
			})

			srv := NewServer(ServerConfig{Port: "0"}, appMux, healthMux, logger)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			srv.httpServer.Handler.ServeHTTP(rec, req)

			// The response should succeed.
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d; want %d", rec.Code, http.StatusOK)
			}

			// Middleware sets X-Request-ID — its absence proves the probe
			// bypassed the middleware chain.
			if got := rec.Header().Get("X-Request-ID"); got != "" {
				t.Errorf("X-Request-ID = %q; want empty (probe should bypass middleware)", got)
			}

			// Middleware sets CORS headers — their absence also proves bypass.
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("Access-Control-Allow-Origin = %q; want empty (probe should bypass middleware)", got)
			}
		})
	}
}

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
		if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, OPTIONS" {
			t.Errorf("Access-Control-Allow-Methods = %q; want %q", got, "GET, POST, OPTIONS")
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
