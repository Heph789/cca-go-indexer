package indexer

import (
	"context"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Verifies that HandleLog routes a log to the handler matching its topic0.
func TestHandlerRegistry_DispatchesToCorrectHandler(t *testing.T) {
	idA := common.HexToHash("0xaaaa")
	idB := common.HexToHash("0xbbbb")

	handlerA := &mockHandler{eventName: "EventA", eventID: idA}
	handlerB := &mockHandler{eventName: "EventB", eventID: idB}

	registry := NewRegistry(handlerA, handlerB)

	log := types.Log{
		Topics: []common.Hash{idA},
	}

	s := newMockStore()
	err := registry.HandleLog(context.Background(), 1, log, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(handlerA.calls) != 1 {
		t.Errorf("expected handler A to be called once, got %d", len(handlerA.calls))
	}
	if len(handlerB.calls) != 0 {
		t.Errorf("expected handler B not to be called, got %d calls", len(handlerB.calls))
	}
}

// Verifies that TopicFilter includes every registered event ID.
func TestHandlerRegistry_TopicFilterReturnsAllEventIDs(t *testing.T) {
	idA := common.HexToHash("0xaaaa")
	idB := common.HexToHash("0xbbbb")

	handlerA := &mockHandler{eventName: "EventA", eventID: idA}
	handlerB := &mockHandler{eventName: "EventB", eventID: idB}

	registry := NewRegistry(handlerA, handlerB)

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

// Verifies that HandleLog returns an error when the log has no topics.
func TestHandleLog_ErrorOnNoTopics(t *testing.T) {
	registry := NewRegistry()

	log := types.Log{
		Topics: []common.Hash{},
	}

	s := newMockStore()
	err := registry.HandleLog(context.Background(), 1, log, s)
	if err == nil {
		t.Fatal("expected error for log with no topics")
	}
	if !strings.Contains(err.Error(), "no topics") {
		t.Errorf("expected error to contain 'no topics', got: %v", err)
	}
}

// Verifies that HandleLog returns an error for a topic with no registered handler.
func TestHandleLog_ErrorOnUnregisteredTopic(t *testing.T) {
	idA := common.HexToHash("0xaaaa")
	handlerA := &mockHandler{eventName: "EventA", eventID: idA}
	registry := NewRegistry(handlerA)

	unknownID := common.HexToHash("0xcccc")
	log := types.Log{
		Topics: []common.Hash{unknownID},
	}

	s := newMockStore()
	err := registry.HandleLog(context.Background(), 1, log, s)
	if err == nil {
		t.Fatal("expected error for unregistered topic")
	}
	if !strings.Contains(err.Error(), "no handler") {
		t.Errorf("expected error to contain 'no handler', got: %v", err)
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

	NewRegistry(h1, h2)
}
