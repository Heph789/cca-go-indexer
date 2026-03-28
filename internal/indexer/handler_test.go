package indexer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/go-cmp/cmp"

	"github.com/cca/go-indexer/internal/store"
)

// Verifies that TopicFilter includes every registered event ID.
func TestHandlerRegistry_TopicFilterReturnsAllEventIDs(t *testing.T) {
	idA := common.HexToHash("0xaaaa")
	idB := common.HexToHash("0xbbbb")

	handlerA := &mockHandler{eventName: "EventA", eventID: idA}
	handlerB := &mockHandler{eventName: "EventB", eventID: idB}

	registry := NewRegistry(noopLogger(), handlerA, handlerB)

	filter := registry.TopicFilter()

	if len(filter) != 1 {
		t.Fatalf("expected 1 outer slice, got %d", len(filter))
	}

	inner := filter[0]
	if len(inner) != 2 {
		t.Fatalf("expected 2 event IDs, got %d", len(inner))
	}

	found := map[common.Hash]bool{}
	for _, h := range inner {
		found[h] = true
	}

	if !found[idA] {
		t.Error("expected idA in topic filter")
	}
	if !found[idB] {
		t.Error("expected idB in topic filter")
	}
}

// Verifies that NewRegistry panics when two handlers share the same EventID.
func TestNewRegistry_PanicsOnDuplicateEventID(t *testing.T) {
	id := common.HexToHash("0xaaaa")
	h1 := &mockHandler{eventName: "Event1", eventID: id}
	h2 := &mockHandler{eventName: "Event2", eventID: id}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for duplicate EventID")
		}
	}()

	NewRegistry(noopLogger(), h1, h2)
}

// TestHandlerRegistry_HandleLogs tests the batch log dispatch method on
// HandlerRegistry. It verifies that logs are grouped by topic0, that handlers
// implementing BatchEventHandler receive a single HandleLogs call with all
// matching logs, and that plain EventHandler implementations fall back to
// per-log Handle calls. Also covers edge cases: empty slices, missing topics,
// unregistered topics, and error propagation.
func TestHandlerRegistry_HandleLogs(t *testing.T) {
	// Shared topic hashes used across test cases.
	topicA := common.HexToHash("0xaaaa")
	topicB := common.HexToHash("0xbbbb")
	topicUnknown := common.HexToHash("0xffff")

	// errSentinel is returned by handlers to verify error propagation.
	errSentinel := errors.New("batch handler failed")

	// makeLogs is a helper that creates n logs with the given topic0.
	makeLogs := func(topic common.Hash, n int) []types.Log {
		logs := make([]types.Log, n)
		for i := range logs {
			logs[i] = types.Log{
				Topics:      []common.Hash{topic},
				BlockNumber: uint64(i + 1),
			}
		}
		return logs
	}

	tests := []struct {
		name string
		// handlers are registered in the registry before calling HandleLogs.
		handlers []EventHandler
		// inputLogs is the slice passed to HandleLogs.
		inputLogs []types.Log
		wantErr   bool
		// wantErrContains, if non-empty, asserts the error message substring.
		wantErrContains string
		// assertFn runs additional assertions on the handlers after HandleLogs returns.
		// Receives the handlers slice in registration order.
		assertFn func(t *testing.T, handlers []EventHandler)
	}{
		// --- happy path ---

		// An empty log slice is a no-op; no handlers should be invoked and no
		// error should be returned.
		{
			name:      "returns nil for empty logs slice",
			handlers:  []EventHandler{&mockHandler{eventName: "A", eventID: topicA}},
			inputLogs: []types.Log{},
			wantErr:   false,
			assertFn: func(t *testing.T, handlers []EventHandler) {
				t.Helper()
				h := handlers[0].(*mockHandler)
				wantCalls := 0
				if diff := cmp.Diff(wantCalls, len(h.calls)); diff != "" {
					t.Errorf("handler A call count mismatch (-want +got):\n%s", diff)
				}
			},
		},

		// A single log dispatched to a plain EventHandler (not BatchEventHandler)
		// should fall back to calling Handle exactly once.
		{
			name:      "falls back to Handle for non-batch handler with single log",
			handlers:  []EventHandler{&mockHandler{eventName: "A", eventID: topicA}},
			inputLogs: makeLogs(topicA, 1),
			wantErr:   false,
			assertFn: func(t *testing.T, handlers []EventHandler) {
				t.Helper()
				h := handlers[0].(*mockHandler)
				wantCalls := 1
				if diff := cmp.Diff(wantCalls, len(h.calls)); diff != "" {
					t.Errorf("handler A call count mismatch (-want +got):\n%s", diff)
				}
			},
		},

		// A single log dispatched to a BatchEventHandler should use HandleLogs
		// (not the single-log Handle fallback).
		{
			name:      "calls HandleLogs for batch handler with single log",
			handlers:  []EventHandler{&mockBatchHandler{eventName: "A", eventID: topicA}},
			inputLogs: makeLogs(topicA, 1),
			wantErr:   false,
			assertFn: func(t *testing.T, handlers []EventHandler) {
				t.Helper()
				h := handlers[0].(*mockBatchHandler)
				wantBatchCalls := 1
				if diff := cmp.Diff(wantBatchCalls, len(h.batchCalls)); diff != "" {
					t.Errorf("batch call count mismatch (-want +got):\n%s", diff)
				}
				wantLogsInBatch := 1
				if diff := cmp.Diff(wantLogsInBatch, len(h.batchCalls[0])); diff != "" {
					t.Errorf("logs in batch mismatch (-want +got):\n%s", diff)
				}
				// Handle (single-log path) should NOT have been called.
				wantSingleCalls := 0
				if diff := cmp.Diff(wantSingleCalls, len(h.calls)); diff != "" {
					t.Errorf("single Handle should not be called (-want +got):\n%s", diff)
				}
			},
		},

		// Multiple logs sharing the same topic0 should be delivered in a single
		// HandleLogs call when the handler implements BatchEventHandler.
		{
			name:      "batches multiple logs for same topic into one HandleLogs call",
			handlers:  []EventHandler{&mockBatchHandler{eventName: "A", eventID: topicA}},
			inputLogs: makeLogs(topicA, 5),
			wantErr:   false,
			assertFn: func(t *testing.T, handlers []EventHandler) {
				t.Helper()
				h := handlers[0].(*mockBatchHandler)
				wantBatchCalls := 1
				if diff := cmp.Diff(wantBatchCalls, len(h.batchCalls)); diff != "" {
					t.Errorf("batch call count mismatch (-want +got):\n%s", diff)
				}
				wantLogsInBatch := 5
				if diff := cmp.Diff(wantLogsInBatch, len(h.batchCalls[0])); diff != "" {
					t.Errorf("logs in batch mismatch (-want +got):\n%s", diff)
				}
			},
		},

		// Logs for different topics should be grouped separately; each handler
		// receives only the logs matching its topic0.
		{
			name: "groups logs by topic and dispatches to correct handlers",
			handlers: []EventHandler{
				&mockBatchHandler{eventName: "A", eventID: topicA},
				&mockBatchHandler{eventName: "B", eventID: topicB},
			},
			inputLogs: append(makeLogs(topicA, 3), makeLogs(topicB, 2)...),
			wantErr:   false,
			assertFn: func(t *testing.T, handlers []EventHandler) {
				t.Helper()
				hA := handlers[0].(*mockBatchHandler)
				hB := handlers[1].(*mockBatchHandler)

				wantABatchCount := 1
				if diff := cmp.Diff(wantABatchCount, len(hA.batchCalls)); diff != "" {
					t.Errorf("handler A batch call count mismatch (-want +got):\n%s", diff)
				}
				wantALogCount := 3
				if diff := cmp.Diff(wantALogCount, len(hA.batchCalls[0])); diff != "" {
					t.Errorf("handler A log count mismatch (-want +got):\n%s", diff)
				}

				wantBBatchCount := 1
				if diff := cmp.Diff(wantBBatchCount, len(hB.batchCalls)); diff != "" {
					t.Errorf("handler B batch call count mismatch (-want +got):\n%s", diff)
				}
				wantBLogCount := 2
				if diff := cmp.Diff(wantBLogCount, len(hB.batchCalls[0])); diff != "" {
					t.Errorf("handler B log count mismatch (-want +got):\n%s", diff)
				}
			},
		},

		// When the registry has a mix of batch and non-batch handlers, the batch
		// handler should receive HandleLogs while the non-batch handler falls
		// back to individual Handle calls.
		{
			name: "mixed handlers: batch gets HandleLogs, non-batch gets Handle per log",
			handlers: []EventHandler{
				&mockBatchHandler{eventName: "Batch", eventID: topicA},
				&mockHandler{eventName: "Single", eventID: topicB},
			},
			inputLogs: append(makeLogs(topicA, 3), makeLogs(topicB, 2)...),
			wantErr:   false,
			assertFn: func(t *testing.T, handlers []EventHandler) {
				t.Helper()
				batch := handlers[0].(*mockBatchHandler)
				single := handlers[1].(*mockHandler)

				// Batch handler should get one HandleLogs call with 3 logs.
				wantBatchCalls := 1
				if diff := cmp.Diff(wantBatchCalls, len(batch.batchCalls)); diff != "" {
					t.Errorf("batch handler call count mismatch (-want +got):\n%s", diff)
				}
				wantBatchLogCount := 3
				if diff := cmp.Diff(wantBatchLogCount, len(batch.batchCalls[0])); diff != "" {
					t.Errorf("batch handler log count mismatch (-want +got):\n%s", diff)
				}

				// Non-batch handler should get 2 individual Handle calls.
				wantSingleCalls := 2
				if diff := cmp.Diff(wantSingleCalls, len(single.calls)); diff != "" {
					t.Errorf("single handler call count mismatch (-want +got):\n%s", diff)
				}
			},
		},

		// --- error cases ---

		// A log with no topics is malformed and should cause HandleLogs to
		// return an error immediately.
		{
			name:            "returns error when log has no topics",
			handlers:        []EventHandler{&mockHandler{eventName: "A", eventID: topicA}},
			inputLogs:       []types.Log{{}},
			wantErr:         true,
			wantErrContains: "no topics",
		},

		// --- edge cases ---

		// A log whose topic0 has no registered handler should be silently
		// skipped. Other valid logs in the same batch should still be processed.
		{
			name:     "skips unregistered topic and still processes known logs",
			handlers: []EventHandler{&mockHandler{eventName: "A", eventID: topicA}},
			inputLogs: append(
				makeLogs(topicA, 1),
				types.Log{Topics: []common.Hash{topicUnknown}},
			),
			wantErr: false,
			assertFn: func(t *testing.T, handlers []EventHandler) {
				t.Helper()
				h := handlers[0].(*mockHandler)
				wantCalls := 1
				if diff := cmp.Diff(wantCalls, len(h.calls)); diff != "" {
					t.Errorf("handler A call count mismatch (-want +got):\n%s", diff)
				}
			},
		},

		// When a BatchEventHandler returns an error, HandleLogs must propagate
		// that error to the caller.
		{
			name: "propagates error from batch handler",
			handlers: []EventHandler{
				&mockBatchHandler{
					eventName: "Failing",
					eventID:   topicA,
					HandleLogsFn: func(ctx context.Context, chainID int64, logs []types.Log, s store.Store) error {
						return errSentinel
					},
				},
			},
			inputLogs:       makeLogs(topicA, 2),
			wantErr:         true,
			wantErrContains: "batch handler failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(noopLogger(), tt.handlers...)
			s := newMockStore()

			chainID := int64(1)
			err := registry.HandleLogs(context.Background(), chainID, tt.inputLogs, s)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.assertFn != nil {
				tt.assertFn(t, tt.handlers)
			}
		})
	}
}
