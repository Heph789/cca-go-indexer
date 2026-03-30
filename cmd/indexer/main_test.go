package main

import (
	"log/slog"
	"testing"

	"github.com/cca/go-indexer/internal/indexer"
	"github.com/cca/go-indexer/internal/indexer/handlers"
)

// TestRegistry_AllHandlersRegistered verifies that a registry created with
// AuctionCreated, BidSubmitted, and CheckpointUpdated handlers exposes all
// three event topics in its TopicFilter.
func TestRegistry_AllHandlersRegistered(t *testing.T) {
	logger := slog.Default()

	auctionHandler := &handlers.AuctionCreatedHandler{}
	bidHandler := &handlers.BidSubmittedHandler{}
	checkpointHandler := &handlers.CheckpointUpdatedHandler{}

	registry := indexer.NewRegistry(logger, auctionHandler, bidHandler, checkpointHandler)

	topicFilter := registry.TopicFilter()

	if len(topicFilter) != 1 {
		t.Fatalf("TopicFilter() outer length = %d, want 1", len(topicFilter))
	}

	if len(topicFilter[0]) != 3 {
		t.Errorf("TopicFilter()[0] length = %d, want 3", len(topicFilter[0]))
	}

	wantTopics := map[string]bool{
		auctionHandler.EventID().Hex():    true,
		bidHandler.EventID().Hex():        true,
		checkpointHandler.EventID().Hex(): true,
	}

	for _, topic := range topicFilter[0] {
		hex := topic.Hex()
		if !wantTopics[hex] {
			t.Errorf("unexpected topic in filter: %s", hex)
		}
		delete(wantTopics, hex)
	}

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
