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

// BatchEventHandler extends EventHandler with batch dispatch. Handlers that
// implement this interface receive all matching logs in a single HandleLogs
// call instead of individual Handle calls, enabling batch store operations.
type BatchEventHandler interface {
	EventHandler
	HandleLogs(ctx context.Context, chainID int64, logs []types.Log, s store.Store) error
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

// HandleLogs groups logs by topic0 and dispatches each group to the
// corresponding handler. Handlers implementing BatchEventHandler receive all
// their logs in a single call; plain EventHandler implementations fall back to
// per-log Handle calls. Logs within each group preserve their original order.
func (r *HandlerRegistry) HandleLogs(ctx context.Context, chainID int64, logs []types.Log, s store.Store) error {
	if len(logs) == 0 {
		return nil
	}

	// Group logs by topic0, preserving encounter order so dispatch is deterministic.
	type group struct {
		topic common.Hash
		logs  []types.Log
	}
	orderIndex := make(map[common.Hash]int)
	var groups []group

	for _, l := range logs {
		if len(l.Topics) == 0 {
			return fmt.Errorf("log has no topics")
		}
		topic0 := l.Topics[0]
		idx, exists := orderIndex[topic0]
		if !exists {
			orderIndex[topic0] = len(groups)
			groups = append(groups, group{topic: topic0, logs: []types.Log{l}})
		} else {
			groups[idx].logs = append(groups[idx].logs, l)
		}
	}

	for _, g := range groups {
		handler, ok := r.handlers[g.topic]
		if !ok {
			r.logger.Warn("skipping log with unregistered topic", "topic", g.topic.Hex())
			continue
		}

		// Prefer the batch path when the handler supports it.
		if bh, isBatch := handler.(BatchEventHandler); isBatch {
			if err := bh.HandleLogs(ctx, chainID, g.logs, s); err != nil {
				return fmt.Errorf("batch handler failed: %w", err)
			}
			continue
		}

		// Fallback: dispatch each log individually.
		for _, l := range g.logs {
			if err := handler.Handle(ctx, chainID, l, s); err != nil {
				return fmt.Errorf("handling log: %w", err)
			}
		}
	}

	return nil
}
