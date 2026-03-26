package indexer

import (
	"context"

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
}

func NewRegistry(handlers ...EventHandler) *HandlerRegistry {
	panic("not implemented")
}

func (r *HandlerRegistry) TopicFilter() [][]common.Hash {
	panic("not implemented")
}

func (r *HandlerRegistry) HandleLog(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	panic("not implemented")
}
