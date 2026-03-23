// Package indexer implements the core block-processing loop and event handler dispatch.
package indexer

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/store"
)

// EventHandler is the interface each event type implements.
type EventHandler interface {
	EventName() string
	EventID() common.Hash
	Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error
}

// HandlerRegistry maps topic0 hashes to their EventHandler.
type HandlerRegistry struct {
	handlers map[common.Hash]EventHandler
}

// NewRegistry creates a registry from a set of handlers.
// Panics if two handlers share the same EventID.
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
func (r *HandlerRegistry) TopicFilter() [][]common.Hash {
	topics := make([]common.Hash, 0, len(r.handlers))
	for id := range r.handlers {
		topics = append(topics, id)
	}
	return [][]common.Hash{topics}
}

// HandleLog dispatches a single log to the appropriate handler based on topic0.
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
