// Package handlers contains EventHandler implementations for each
// CCA event type. Adding a new event means adding a new file here
// and registering it in main.go.
package handlers

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	ethabi "github.com/cca/go-indexer/internal/eth/abi"
	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// AuctionCreatedHandler decodes AuctionCreated logs from the factory
// contract and writes both a raw event record and a typed auction record.
type AuctionCreatedHandler struct{}

func (h *AuctionCreatedHandler) EventName() string {
	return "AuctionCreated"
}

func (h *AuctionCreatedHandler) EventID() common.Hash {
	return ethabi.AuctionCreatedEventID
}

// Handle decodes an AuctionCreated log and persists it.
// Called inside a WithTx transaction — both the raw event and the
// typed auction record are written atomically.
func (h *AuctionCreatedHandler) Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	// --- Step 1: Decode the log using the factory ABI ---
	// TODO: Use ethabi.FactoryABI.Unpack("AuctionCreated", log.Data)
	// to extract non-indexed fields. Indexed fields come from log.Topics.
	// Exact field mapping depends on the ABI — adapt once obtained.

	// --- Step 2: Build the raw event for the audit trail ---
	rawEvent := &cca.RawEvent{
		ChainID:     chainID,
		BlockNumber: log.BlockNumber,
		BlockHash:   log.BlockHash,
		TxHash:      log.TxHash,
		LogIndex:    log.Index,
		Address:     log.Address,
		EventName:   h.EventName(),
		// TODO: serialize log.Topics to JSON array
		// TODO: hex-encode log.Data
		// TODO: JSON-encode decoded fields
	}

	if err := s.RawEventRepo().Insert(ctx, rawEvent); err != nil {
		return fmt.Errorf("insert raw event: %w", err)
	}

	// --- Step 3: Build the typed auction record ---
	auction := &cca.Auction{
		ChainID:     chainID,
		BlockNumber: log.BlockNumber,
		TxHash:      log.TxHash,
		LogIndex:    log.Index,
		// TODO: map decoded fields to Auction struct
		// AuctionAddress, TokenOut, CurrencyIn, Owner, StartTime, EndTime
	}

	if err := s.AuctionRepo().Insert(ctx, auction); err != nil {
		return fmt.Errorf("insert auction: %w", err)
	}

	return nil
}
