package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	ethabi "github.com/cca/go-indexer/internal/eth/abi"
	"github.com/cca/go-indexer/internal/store"
)

// BidSubmittedHandler processes BidSubmitted events emitted by CCA auction contracts.
type BidSubmittedHandler struct{}

// EventName returns the Solidity event name.
func (h *BidSubmittedHandler) EventName() string {
	return "BidSubmitted"
}

// EventID returns the keccak256 topic0 hash for BidSubmitted.
func (h *BidSubmittedHandler) EventID() common.Hash {
	return ethabi.BidSubmittedEventID
}

var bidDataArgs = abi.Arguments{
	{Name: "price", Type: mustABIType("uint256")},
	{Name: "amount", Type: mustABIType("uint128")},
}

// Handle decodes a BidSubmitted log, persists the raw event and the Bid domain object.
func (h *BidSubmittedHandler) Handle(ctx context.Context, chainID int64, log types.Log, blockTime time.Time, s store.Store) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("expected 3 topics, got %d", len(log.Topics))
	}

	bidID := new(big.Int).SetBytes(log.Topics[1].Bytes())
	owner := common.BytesToAddress(log.Topics[2].Bytes())

	vals, err := bidDataArgs.Unpack(log.Data)
	if err != nil {
		return fmt.Errorf("unpack bid data: %w", err)
	}
	price := vals[0].(*big.Int)
	amount := vals[1].(*big.Int)

	topicStrs := make([]string, len(log.Topics))
	for i, t := range log.Topics {
		topicStrs[i] = t.Hex()
	}
	topicsJSON, err := json.Marshal(topicStrs)
	if err != nil {
		return fmt.Errorf("marshal topics: %w", err)
	}

	decoded := map[string]any{
		"id":     bidID.String(),
		"owner":  owner.Hex(),
		"price":  price.String(),
		"amount": amount.String(),
	}
	decodedJSON, err := json.Marshal(decoded)
	if err != nil {
		return fmt.Errorf("marshal decoded data: %w", err)
	}

	rawEvent := &cca.RawEvent{
		ChainID:     chainID,
		BlockNumber: log.BlockNumber,
		BlockHash:   log.BlockHash,
		BlockTime:   blockTime,
		TxHash:      log.TxHash,
		LogIndex:    log.Index,
		Address:     log.Address,
		EventName:   h.EventName(),
		TopicsJSON:  string(topicsJSON),
		DataHex:     "0x" + hex.EncodeToString(log.Data),
		DecodedJSON: string(decodedJSON),
	}
	if err := s.RawEventRepo().Insert(ctx, rawEvent); err != nil {
		return fmt.Errorf("insert raw event: %w", err)
	}

	bid := &cca.Bid{
		ID:             bidID.Uint64(),
		Owner:          owner,
		PriceQ96:       price,
		Amount:         amount,
		AuctionAddress: log.Address,
		ChainID:        chainID,
		BlockNumber:    log.BlockNumber,
		BlockTime:      blockTime,
		TxHash:         log.TxHash,
		LogIndex:       log.Index,
	}
	if err := s.BidRepo().Insert(ctx, bid); err != nil {
		return fmt.Errorf("insert bid: %w", err)
	}

	return nil
}
