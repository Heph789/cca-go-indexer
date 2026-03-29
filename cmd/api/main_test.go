package main

import (
	"context"
	"fmt"
	"net"
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

func (m *mockStore) AuctionRepo() store.AuctionRepository   { return nil }
func (m *mockStore) RawEventRepo() store.RawEventRepository { return nil }
func (m *mockStore) CursorRepo() store.CursorRepository     { return nil }
func (m *mockStore) BlockRepo() store.BlockRepository       { return nil }
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
