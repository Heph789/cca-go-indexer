// Package indexer implements the core block-processing loop,
// event handler dispatch, and reorg detection.
package indexer

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/store"
)

// EventHandler is the interface each event type implements.
// Adding a new event means implementing this interface and
// registering the handler at startup — no changes to the
// indexer loop, registry, or reorg handling.
type EventHandler interface {
	// EventName returns a human-readable name (e.g., "AuctionCreated").
	EventName() string

	// EventID returns the topic0 hash that identifies this event type.
	EventID() common.Hash

	// Handle decodes a single log entry and writes the result to the store.
	// Called inside a WithTx transaction, so all writes are atomic with
	// other events in the same block range.
	Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error
}

// HandlerRegistry maps topic0 hashes to their EventHandler.
// It automatically builds the topic filter for eth_getLogs from
// the set of registered handlers.
type HandlerRegistry struct {
	handlers map[common.Hash]EventHandler
}

// NewRegistry creates a registry from a set of handlers.
// Panics if two handlers share the same EventID (programming error).
func NewRegistry(handlers ...EventHandler) *HandlerRegistry {
	r := &HandlerRegistry{
		handlers: make(map[common.Hash]EventHandler, len(handlers)),
	}
	for _, h := range handlers {
		if _, exists := r.handlers[h.EventID()]; exists {
			panic(fmt.Sprintf("duplicate handler for topic %s", h.EventID().Hex()))
		}
		r.handlers[h.EventID()] = h
	}
	return r
}

// TopicFilter returns the topic0 filter for eth_getLogs.
// This is a single-element outer slice containing all registered
// event IDs, meaning "match any of these topics".
func (r *HandlerRegistry) TopicFilter() [][]common.Hash {
	topics := make([]common.Hash, 0, len(r.handlers))
	for id := range r.handlers {
		topics = append(topics, id)
	}
	return [][]common.Hash{topics}
}

// HandleLog dispatches a single log to the appropriate handler
// based on topic0. Returns an error if no handler is registered
// (shouldn't happen if the topic filter is correct, but defensive).
func (r *HandlerRegistry) HandleLog(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	if len(log.Topics) == 0 {
		return fmt.Errorf("log has no topics")
	}

	handler, ok := r.handlers[log.Topics[0]]
	if !ok {
		// This shouldn't happen since eth_getLogs is filtered by our topics,
		// but handle defensively.
		return fmt.Errorf("no handler for topic %s", log.Topics[0].Hex())
	}

	return handler.Handle(ctx, chainID, log, s)
}
