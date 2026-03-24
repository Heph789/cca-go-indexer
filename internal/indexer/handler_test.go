package indexer

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// TestNewRegistryPanicsOnDuplicate verifies that NewRegistry panics when two
// handlers share the same EventID.
func TestNewRegistryPanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate handler, got none")
		}
	}()
	topic := common.HexToHash("0xaa")
	h1 := &fakeEventHandler{eventID: topic}
	h2 := &fakeEventHandler{eventID: topic}
	NewRegistry(h1, h2) // should panic
}

// TestTopicFilter verifies that TopicFilter returns all registered topic0 hashes.
func TestTopicFilter(t *testing.T) {
	t1 := common.HexToHash("0x01")
	t2 := common.HexToHash("0x02")
	registry := NewRegistry(
		&fakeEventHandler{eventID: t1},
		&fakeEventHandler{eventID: t2},
	)

	filter := registry.TopicFilter()
	if len(filter) != 1 {
		t.Fatalf("expected outer slice len=1, got %d", len(filter))
	}
	if len(filter[0]) != 2 {
		t.Fatalf("expected 2 topic hashes, got %d", len(filter[0]))
	}
	got := map[common.Hash]bool{filter[0][0]: true, filter[0][1]: true}
	if !got[t1] || !got[t2] {
		t.Fatalf("missing expected topics in filter: %v", filter[0])
	}
}

// TestHandleLogDispatches verifies that HandleLog routes to the correct handler.
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
func TestHandleLogUnknownTopic(t *testing.T) {
	registry := NewRegistry(&fakeEventHandler{eventID: common.HexToHash("0xaa")})
	s := newFakeStore()

	log := types.Log{Topics: []common.Hash{common.HexToHash("0xbb")}}
	if err := registry.HandleLog(context.Background(), 1, log, s); err == nil {
		t.Fatal("expected error for unknown topic, got nil")
	}
}
