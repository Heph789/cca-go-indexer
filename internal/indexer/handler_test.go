package indexer

// handler_test.go tests the HandlerRegistry — the component that maps
// Solidity event signatures (topic0 hashes) to their EventHandler implementations.
//
// These tests use fakeEventHandler (from fakes_test.go) with arbitrary topic
// hashes. They verify:
//   - Duplicate detection: NewRegistry panics if two handlers share a topic.
//   - TopicFilter: the filter returned for eth_getLogs contains all registered topics.
//   - Dispatch: HandleLog routes a log to the correct handler based on topic0.
//   - Error cases: logs with no topics or unknown topics produce errors.

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// TestNewRegistryPanicsOnDuplicate verifies that NewRegistry panics when two
// handlers share the same EventID (topic0).
//
// This is a safety check: duplicate handlers would silently shadow each other,
// so we catch this at startup with a panic rather than dropping events at
// runtime.
func TestNewRegistryPanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate handler, got none")
		}
	}()
	topic := common.HexToHash("0xaa")
	h1 := &fakeEventHandler{eventID: topic}
	h2 := &fakeEventHandler{eventID: topic}
	NewRegistry(h1, h2) // should panic — both handlers claim the same topic
}

// TestTopicFilter verifies that TopicFilter returns all registered topic0 hashes.
//
// TopicFilter is used to build the eth_getLogs query. The result should be
// [][]common.Hash with one inner slice (the topic0 position) containing all
// registered event IDs. This tells the RPC node: "give me logs where topic0
// is any of these hashes."
func TestTopicFilter(t *testing.T) {
	t1 := common.HexToHash("0x01")
	t2 := common.HexToHash("0x02")
	registry := NewRegistry(
		&fakeEventHandler{eventID: t1},
		&fakeEventHandler{eventID: t2},
	)

	filter := registry.TopicFilter()

	// Outer slice should have exactly 1 element (the topic0 position).
	if len(filter) != 1 {
		t.Fatalf("expected outer slice len=1, got %d", len(filter))
	}
	// Inner slice should contain both registered topics.
	if len(filter[0]) != 2 {
		t.Fatalf("expected 2 topic hashes, got %d", len(filter[0]))
	}
	// Use a map since the order of topics from map iteration is non-deterministic.
	got := map[common.Hash]bool{filter[0][0]: true, filter[0][1]: true}
	if !got[t1] || !got[t2] {
		t.Fatalf("missing expected topics in filter: %v", filter[0])
	}
}

// TestHandleLogDispatches verifies that HandleLog routes a log to the correct
// handler based on its topic0 (the first topic hash).
//
// We register one handler for topic 0xaa, then send a log with that topic.
// The handler should receive exactly one call.
func TestHandleLogDispatches(t *testing.T) {
	topic := common.HexToHash("0xaa")
	handler := &fakeEventHandler{eventID: topic}
	registry := NewRegistry(handler)
	s := newFakeStore()

	log := types.Log{Topics: []common.Hash{topic}}
	if err := registry.HandleLog(context.Background(), 1, log, s); err != nil {
		t.Fatalf("HandleLog() error: %v", err)
	}
	if len(handler.handleCalls) != 1 {
		t.Fatalf("expected 1 handler call, got %d", len(handler.handleCalls))
	}
}

// TestHandleLogNoTopics verifies that HandleLog returns an error for a log
// with no topics.
//
// Ethereum logs always have at least one topic (the event signature hash).
// A log with zero topics is either malformed or an anonymous event — either
// way, the registry can't route it.
func TestHandleLogNoTopics(t *testing.T) {
	registry := NewRegistry()
	s := newFakeStore()

	log := types.Log{Topics: nil}
	if err := registry.HandleLog(context.Background(), 1, log, s); err == nil {
		t.Fatal("expected error for log with no topics, got nil")
	}
}

// TestHandleLogUnknownTopic verifies that HandleLog returns an error when no
// handler is registered for the log's topic0.
//
// This would indicate a bug: FilterLogs returned a log that doesn't match
// any of the topics we asked for. Returning an error surfaces this immediately
// rather than silently dropping the event.
func TestHandleLogUnknownTopic(t *testing.T) {
	// Register a handler for topic 0xaa...
	registry := NewRegistry(&fakeEventHandler{eventID: common.HexToHash("0xaa")})
	s := newFakeStore()

	// ...but send a log with topic 0xbb — no handler matches.
	log := types.Log{Topics: []common.Hash{common.HexToHash("0xbb")}}
	if err := registry.HandleLog(context.Background(), 1, log, s); err == nil {
		t.Fatal("expected error for unknown topic, got nil")
	}
}
