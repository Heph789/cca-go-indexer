package main

import (
	"log/slog"
	"testing"

	"github.com/cca/go-indexer/internal/indexer"
	"github.com/cca/go-indexer/internal/indexer/handlers"
)

// TestRegistry_BothHandlersRegistered verifies that a registry created with
// both AuctionCreatedHandler and CheckpointUpdatedHandler exposes both event
// topics in its TopicFilter.
func TestRegistry_BothHandlersRegistered(t *testing.T) {
	logger := slog.Default()

	auctionHandler := &handlers.AuctionCreatedHandler{}
	checkpointHandler := &handlers.CheckpointUpdatedHandler{}

	registry := indexer.NewRegistry(logger, auctionHandler, checkpointHandler)

	topicFilter := registry.TopicFilter()

	// TopicFilter returns [][]common.Hash where the outer slice has exactly one
	// element (the OR-set for topic0 position).
	wantOuterLen := 1
	if len(topicFilter) != wantOuterLen {
		t.Fatalf("TopicFilter() outer length = %d, want %d", len(topicFilter), wantOuterLen)
	}

	// The inner slice should contain one topic per registered handler.
	wantTopicCount := 2
	gotTopicCount := len(topicFilter[0])
	if gotTopicCount != wantTopicCount {
		t.Errorf("TopicFilter()[0] length = %d, want %d", gotTopicCount, wantTopicCount)
	}

	// Verify the actual topic hashes match the handlers' EventIDs, not just
	// the count. Build a set of expected topics and check membership.
	wantTopics := map[string]bool{
		auctionHandler.EventID().Hex():    true,
		checkpointHandler.EventID().Hex(): true,
	}

	for _, topic := range topicFilter[0] {
		hex := topic.Hex()
		if !wantTopics[hex] {
			t.Errorf("unexpected topic in filter: %s", hex)
		}
		delete(wantTopics, hex)
	}

	// Any remaining entries in wantTopics are missing from the filter.
	for missing := range wantTopics {
		t.Errorf("missing expected topic in filter: %s", missing)
	}
}

// TestRegistry_SingleHandler_TopicFilter verifies the baseline behavior that
// a registry with only one handler returns a single topic.
func TestRegistry_SingleHandler_TopicFilter(t *testing.T) {
	logger := slog.Default()

	auctionHandler := &handlers.AuctionCreatedHandler{}
	registry := indexer.NewRegistry(logger, auctionHandler)

	topicFilter := registry.TopicFilter()

	wantOuterLen := 1
	if len(topicFilter) != wantOuterLen {
		t.Fatalf("TopicFilter() outer length = %d, want %d", len(topicFilter), wantOuterLen)
	}

	wantTopicCount := 1
	gotTopicCount := len(topicFilter[0])
	if gotTopicCount != wantTopicCount {
		t.Errorf("TopicFilter()[0] length = %d, want %d", gotTopicCount, wantTopicCount)
	}

	// Verify the single topic matches AuctionCreatedHandler's EventID.
	wantHex := auctionHandler.EventID().Hex()
	gotHex := topicFilter[0][0].Hex()
	if gotHex != wantHex {
		t.Errorf("TopicFilter()[0][0] = %s, want %s", gotHex, wantHex)
	}
}
