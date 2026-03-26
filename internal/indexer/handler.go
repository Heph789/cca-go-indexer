package indexer

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/store"
)

type EventHandler interface {
	EventName() string
	EventID() common.Hash
	Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error
}

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

func (r *HandlerRegistry) TopicFilter() [][]common.Hash {
	topics := make([]common.Hash, 0, len(r.handlers))
	for id := range r.handlers {
		topics = append(topics, id)
	}
	return [][]common.Hash{topics}
}

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
