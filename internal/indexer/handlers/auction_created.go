package handlers

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	ethabi "github.com/cca/go-indexer/internal/eth/abi"
	"github.com/cca/go-indexer/internal/store"
)

type AuctionCreatedHandler struct{}

func (h *AuctionCreatedHandler) EventName() string {
	return "AuctionCreated"
}

func (h *AuctionCreatedHandler) EventID() common.Hash {
	return ethabi.AuctionCreatedEventID
}

func (h *AuctionCreatedHandler) Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	panic("not implemented")
}
