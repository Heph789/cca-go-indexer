package indexer

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/store"
)

// EventHandler processes a specific on-chain event type identified by its topic0 hash.
type EventHandler interface {
	EventName() string
	EventID() common.Hash
	Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error
}

// HandlerRegistry maps event topic0 hashes to their corresponding EventHandler
// and provides dispatch and topic filtering for log subscription queries.
type HandlerRegistry struct {
	handlers map[common.Hash]EventHandler
	logger   *slog.Logger
}

func NewRegistry(logger *slog.Logger, handlers ...EventHandler) *HandlerRegistry {
	m := make(map[common.Hash]EventHandler, len(handlers))
	for _, h := range handlers {
		id := h.EventID()
		if _, exists := m[id]; exists {
			panic(fmt.Sprintf("duplicate EventID %s: %s", id.Hex(), h.EventName()))
		}
		m[id] = h
	}
	return &HandlerRegistry{handlers: m, logger: logger}
}

// TopicFilter returns all registered event IDs in the [][]common.Hash shape
// expected by ethereum.FilterQuery.Topics (OR over topic0 position).
func (r *HandlerRegistry) TopicFilter() [][]common.Hash {
	topics := make([]common.Hash, 0, len(r.handlers))
	for id := range r.handlers {
		topics = append(topics, id)
	}
	return [][]common.Hash{topics}
}

// HandleLog dispatches a single log to the handler registered for its topic0.
// It returns an error if the log has no topics or no handler is registered.
func (r *HandlerRegistry) HandleLog(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	if len(log.Topics) == 0 {
		return fmt.Errorf("log has no topics")
	}
	topic0 := log.Topics[0]
	handler, ok := r.handlers[topic0]
	if !ok {
		r.logger.Warn("skipping log with unregistered topic", "topic", topic0.Hex())
		return nil
	}
	return handler.Handle(ctx, chainID, log, s)
}
