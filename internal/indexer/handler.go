// Package indexer implements the core block-processing loop and event handler dispatch.
//
// Architecture overview:
//   - ChainIndexer runs the main polling loop for a single EVM chain.
//   - HandlerRegistry maps Solidity event signatures (topic0) to EventHandlers.
//   - Each EventHandler (e.g. AuctionCreatedHandler) knows how to decode a
//     specific log type and persist the result through the store interfaces.
package indexer

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/store"
)

// EventHandler is the interface each event type implements.
//
// EventName returns a human-readable label (used in RawEvent.EventName).
// EventID returns the keccak256 hash of the Solidity event signature, which
// corresponds to topic0 in Ethereum logs. This is what the registry uses to
// route incoming logs to the correct handler.
type EventHandler interface {
	EventName() string
	EventID() common.Hash
	Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error
}

// HandlerRegistry maps topic0 hashes to their EventHandler.
// The indexer builds one registry at startup and passes it to ChainIndexer.
type HandlerRegistry struct {
	handlers map[common.Hash]EventHandler
}

// NewRegistry creates a registry from a set of handlers.
// Panics if two handlers share the same EventID — this is a programming error
// caught at startup rather than silently dropping events at runtime.
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
// The result is a [][]common.Hash where the outer slice has one element
// (the topic0 position) containing all registered event IDs. This tells the
// RPC node to return logs matching ANY of the registered events.
func (r *HandlerRegistry) TopicFilter() [][]common.Hash {
	topics := make([]common.Hash, 0, len(r.handlers))
	for id := range r.handlers {
		topics = append(topics, id)
	}
	return [][]common.Hash{topics}
}

// HandleLog dispatches a single log to the appropriate handler based on topic0.
// Returns an error if the log has no topics or if no handler is registered for
// the log's topic0 — both indicate a bug in the filter query or registry setup.
func (r *HandlerRegistry) HandleLog(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	if len(log.Topics) == 0 {
		return fmt.Errorf("log has no topics")
	}
	handler, ok := r.handlers[log.Topics[0]]
	if !ok {
		return fmt.Errorf("no handler for topic %s", log.Topics[0].Hex())
	}
	return handler.Handle(ctx, chainID, log, s)
}
