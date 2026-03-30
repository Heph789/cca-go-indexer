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

// CheckpointUpdatedHandler processes CheckpointUpdated events emitted by CCA auction contracts.
type CheckpointUpdatedHandler struct{}

// EventName returns the Solidity event name.
func (h *CheckpointUpdatedHandler) EventName() string {
	return "CheckpointUpdated"
}

// EventID returns the keccak256 topic0 hash for CheckpointUpdated.
func (h *CheckpointUpdatedHandler) EventID() common.Hash {
	return ethabi.CheckpointUpdatedEventID
}

var checkpointDataArgs = abi.Arguments{
	{Name: "blockNumber", Type: mustABIType("uint256")},
	{Name: "clearingPrice", Type: mustABIType("uint256")},
	{Name: "cumulativeMps", Type: mustABIType("uint24")},
}

// Handle decodes a CheckpointUpdated log, persists the raw event and the Checkpoint domain object.
func (h *CheckpointUpdatedHandler) Handle(ctx context.Context, chainID int64, log types.Log, blockTime time.Time, s store.Store) error {
	vals, err := checkpointDataArgs.Unpack(log.Data)
	if err != nil {
		return fmt.Errorf("unpack checkpoint data: %w", err)
	}
	blockNumber := vals[0].(*big.Int)
	clearingPrice := vals[1].(*big.Int)
	cumulativeMps := vals[2].(*big.Int)

	topicStrs := make([]string, len(log.Topics))
	for i, t := range log.Topics {
		topicStrs[i] = t.Hex()
	}
	topicsJSON, err := json.Marshal(topicStrs)
	if err != nil {
		return fmt.Errorf("marshal topics: %w", err)
	}

	decoded := map[string]any{
		"blockNumber":   blockNumber.String(),
		"clearingPrice": clearingPrice.String(),
		"cumulativeMps": cumulativeMps.Uint64(),
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
		EventName:   "CheckpointUpdated",
		TopicsJSON:  string(topicsJSON),
		DataHex:     "0x" + hex.EncodeToString(log.Data),
		DecodedJSON: string(decodedJSON),
	}
	if err := s.RawEventRepo().Insert(ctx, rawEvent); err != nil {
		return fmt.Errorf("insert raw event: %w", err)
	}

	checkpoint := &cca.Checkpoint{
		BlockNumber:      blockNumber.Uint64(),
		ClearingPriceQ96: clearingPrice,
		CumulativeMps:    uint32(cumulativeMps.Uint64()),
		AuctionAddress:   log.Address,
		ChainID:          chainID,
		TxBlockNumber:    log.BlockNumber,
		TxBlockTime:      blockTime,
		TxHash:           log.TxHash,
		LogIndex:         log.Index,
	}
	if err := s.CheckpointRepo().Insert(ctx, checkpoint); err != nil {
		return fmt.Errorf("insert checkpoint: %w", err)
	}

	return nil
}
