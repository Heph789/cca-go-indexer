package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cca/go-indexer/internal/config"
	applog "github.com/cca/go-indexer/internal/log"
	"github.com/cca/go-indexer/internal/store"
)

// mockStore implements store.Store for testing without a real database.
type mockStore struct {
	PingFn  func(ctx context.Context) error
	CloseFn func()
}

func (m *mockStore) AuctionRepo() store.AuctionRepository             { return nil }
func (m *mockStore) BidRepo() store.BidRepository                     { return nil }
func (m *mockStore) CheckpointRepo() store.CheckpointRepository       { return nil }
func (m *mockStore) RawEventRepo() store.RawEventRepository           { return nil }
func (m *mockStore) CursorRepo() store.CursorRepository               { return nil }
func (m *mockStore) BlockRepo() store.BlockRepository                 { return nil }
func (m *mockStore) WatchedContractRepo() store.WatchedContractRepository { return nil }
func (m *mockStore) RollbackFromBlock(_ context.Context, _ int64, _ uint64) error {
	return nil
}
func (m *mockStore) WithTx(ctx context.Context, fn func(txStore store.Store) error) error {
	return fn(m)
}

func (m *mockStore) Ping(ctx context.Context) error {
	if m.PingFn != nil {
		return m.PingFn(ctx)
	}
	return nil
}

func (m *mockStore) Close() {
	if m.CloseFn != nil {
		m.CloseFn()
	}
}

// freePort asks the OS for an available TCP port and returns it as a string.
// This avoids port conflicts in parallel test runs.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return fmt.Sprintf("%d", port)
}

// TestRun tests the extracted run function that encapsulates the API server
// lifecycle. Covers: startup failure returning an error instead of os.Exit,
// and graceful shutdown via context cancellation returning nil.
func TestRun(t *testing.T) {
	logger := applog.NewLogger("error", "text")

	tests := []struct {
		name     string
		cfg      *config.Config
		setupCtx func(t *testing.T, cfg *config.Config) (context.Context, context.CancelFunc, func())
		st       store.Store
		wantErr  bool
	}{
		{
			name: "returns error when port is already in use",
			cfg: &config.Config{
				ChainID: 1,
			},
			setupCtx: func(t *testing.T, cfg *config.Config) (context.Context, context.CancelFunc, func()) {
				t.Helper()
				// Occupy a port so the server cannot bind to it.
				port := freePort(t)
				l, err := net.Listen("tcp", "127.0.0.1:"+port)
				if err != nil {
					t.Fatalf("failed to listen on port %s: %v", port, err)
				}
				cfg.Port = port
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				return ctx, cancel, func() {
					l.Close()
				}
			},
			st:      &mockStore{},
			wantErr: true,
		},

		{
			name: "returns error when port is invalid",
			cfg: &config.Config{
				Port:    "not-a-port",
				ChainID: 1,
			},
			setupCtx: func(t *testing.T, cfg *config.Config) (context.Context, context.CancelFunc, func()) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				return ctx, cancel, func() {}
			},
			st:      &mockStore{},
			wantErr: true,
		},

		{
			name: "returns nil on graceful shutdown via context cancel",
			cfg: &config.Config{
				ChainID: 1,
			},
			setupCtx: func(t *testing.T, cfg *config.Config) (context.Context, context.CancelFunc, func()) {
				cfg.Port = freePort(t)
				ctx, cancel := context.WithCancel(context.Background())
				// Cancel after a short delay to let the server start.
				go func() {
					time.Sleep(200 * time.Millisecond)
					cancel()
				}()
				return ctx, cancel, func() {}
			},
			st:      &mockStore{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel, teardown := tt.setupCtx(t, tt.cfg)
			defer teardown()
			defer cancel()

			err := run(ctx, cancel, tt.cfg, logger, tt.st)

			if tt.wantErr && err == nil {
				t.Error("run() returned nil; want non-nil error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("run() returned error: %v; want nil", err)
			}
		})
	}
}

// TestRun_GraphQLEndpoint tests that the API server mounts a GraphQL endpoint
// at /graphql. POST /graphql accepts GraphQL queries and GET /graphql serves
// the playground.
func TestRun_GraphQLEndpoint(t *testing.T) {
	logger := applog.NewLogger("error", "text")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{
			name:       "POST /graphql returns 200 for introspection query",
			method:     http.MethodPost,
			path:       "/graphql",
			body:       `{"query": "{ __schema { queryType { name } } }"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "GET /graphql returns 200 (playground)",
			method:     http.MethodGet,
			path:       "/graphql",
			body:       "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "GET /health still returns 200",
			method:     http.MethodGet,
			path:       "/health",
			body:       "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "GET /ready still returns 200",
			method:     http.MethodGet,
			path:       "/ready",
			body:       "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := freePort(t)
			cfg := &config.Config{
				Port:    port,
				ChainID: 1,
			}
			st := &mockStore{}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			errCh := make(chan error, 1)
			go func() {
				errCh <- run(ctx, cancel, cfg, logger, st)
			}()

			// Wait for the server to be ready by polling the health endpoint.
			baseURL := "http://127.0.0.1:" + port
			waitForServer(t, baseURL+"/health", 2*time.Second)

			var resp *http.Response
			var err error

			targetURL := baseURL + tt.path
			if tt.method == http.MethodPost {
				resp, err = http.Post(targetURL, "application/json", strings.NewReader(tt.body))
			} else {
				resp, err = http.Get(targetURL)
			}
			if err != nil {
				t.Fatalf("HTTP %s %s failed: %v", tt.method, tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("HTTP %s %s status = %d, want %d; body: %s",
					tt.method, tt.path, resp.StatusCode, tt.wantStatus, string(body))
			}

			cancel()
			// Drain the run goroutine error.
			<-errCh
		})
	}
}

// TestRun_GraphQLIntrospectionReturnsSchema sends an introspection query to
// /graphql and verifies the response contains data.__schema.queryType.name,
// confirming the schema is loaded and resolvers are wired.
func TestRun_GraphQLIntrospectionReturnsSchema(t *testing.T) {
	logger := applog.NewLogger("error", "text")
	port := freePort(t)
	cfg := &config.Config{
		Port:    port,
		ChainID: 1,
	}
	st := &mockStore{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cancel, cfg, logger, st)
	}()

	baseURL := "http://127.0.0.1:" + port
	waitForServer(t, baseURL+"/health", 2*time.Second)

	// Send an introspection query.
	introspectionQuery := `{"query": "{ __schema { queryType { name } } }"}`
	resp, err := http.Post(baseURL+"/graphql", "application/json", strings.NewReader(introspectionQuery))
	if err != nil {
		t.Fatalf("POST /graphql failed: %v", err)
	}
	defer resp.Body.Close()

	wantStatus := http.StatusOK
	if resp.StatusCode != wantStatus {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /graphql status = %d, want %d; body: %s", resp.StatusCode, wantStatus, string(body))
	}

	// Parse the JSON response and verify it contains the expected structure.
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode /graphql response as JSON: %v", err)
	}

	// Traverse data.__schema.queryType.name
	data, ok := result["data"]
	if !ok {
		t.Fatalf("response missing 'data' key; got keys: %v", mapKeys(result))
	}
	dataMap, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("'data' is not an object; got %T", data)
	}

	schema, ok := dataMap["__schema"]
	if !ok {
		t.Fatalf("response.data missing '__schema' key; got keys: %v", mapKeys(dataMap))
	}
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("'__schema' is not an object; got %T", schema)
	}

	queryType, ok := schemaMap["queryType"]
	if !ok {
		t.Fatalf("response.data.__schema missing 'queryType' key; got keys: %v", mapKeys(schemaMap))
	}
	queryTypeMap, ok := queryType.(map[string]any)
	if !ok {
		t.Fatalf("'queryType' is not an object; got %T", queryType)
	}

	wantName := "Query"
	gotName, ok := queryTypeMap["name"]
	if !ok {
		t.Fatalf("response.data.__schema.queryType missing 'name' key")
	}
	if gotName != wantName {
		t.Errorf("queryType.name = %v, want %q", gotName, wantName)
	}

	cancel()
	<-errCh
}

// TestRun_GraphQLBidHintQueryAvailable verifies that the bidHint query is
// exposed in the GraphQL schema via introspection.
func TestRun_GraphQLBidHintQueryAvailable(t *testing.T) {
	logger := applog.NewLogger("error", "text")
	port := freePort(t)
	cfg := &config.Config{
		Port:    port,
		ChainID: 1,
	}
	st := &mockStore{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, cancel, cfg, logger, st)
	}()

	baseURL := "http://127.0.0.1:" + port
	waitForServer(t, baseURL+"/health", 2*time.Second)

	query := `{"query": "{ __schema { queryType { fields { name } } } }"}`
	resp, err := http.Post(baseURL+"/graphql", "application/json", strings.NewReader(query))
	if err != nil {
		t.Fatalf("POST /graphql failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Traverse data.__schema.queryType.fields to find "bidHint".
	data := result["data"].(map[string]any)
	schema := data["__schema"].(map[string]any)
	queryType := schema["queryType"].(map[string]any)
	fields := queryType["fields"].([]any)

	found := false
	for _, f := range fields {
		field := f.(map[string]any)
		if field["name"] == "bidHint" {
			found = true
			break
		}
	}
	if !found {
		t.Error("bidHint query not found in GraphQL schema introspection")
	}

	cancel()
	<-errCh
}

// waitForServer polls the given URL until it gets a 200 response or the
// timeout expires. Used to wait for the test server to be ready.
func waitForServer(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s not ready within %v", url, timeout)
}

// mapKeys returns the keys of a map for diagnostic output in test failures.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestRun_DeferredCleanupExecutes verifies that the store's Close method is
// called during shutdown. This is the core behavior that os.Exit(1) was
// preventing: deferred cleanup must run when the server goroutine encounters
// an error.
func TestRun_DeferredCleanupExecutes(t *testing.T) {
	logger := applog.NewLogger("error", "text")

	closeCalled := false
	st := &mockStore{
		CloseFn: func() {
			closeCalled = true
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	cfg := &config.Config{
		Port:    freePort(t),
		ChainID: 1,
	}

	// Cancel after a short delay to let the server start.
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	_ = run(ctx, cancel, cfg, logger, st)

	if !closeCalled {
		t.Error("store.Close() was not called; want it called on shutdown")
	}
}
