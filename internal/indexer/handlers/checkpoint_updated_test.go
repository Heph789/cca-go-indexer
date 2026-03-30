package handlers

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	ethabi "github.com/cca/go-indexer/internal/eth/abi"
)

// TestCheckpointUpdated_EventName verifies the handler reports the correct
// Solidity event name used for log routing and raw event labeling.
func TestCheckpointUpdated_EventName(t *testing.T) {
	h := &CheckpointUpdatedHandler{}
	wantName := "CheckpointUpdated"
	if got := h.EventName(); got != wantName {
		t.Errorf("EventName() = %q, want %q", got, wantName)
	}
}

// checkpointFixture holds deterministic test values for a CheckpointUpdated log.
type checkpointFixture struct {
	auctionAddr   common.Address
	blockNumber   *big.Int // the auction's logical block from the event data
	clearingPrice *big.Int
	cumulativeMps *big.Int
	chainID       int64
	txBlockNumber uint64 // the chain block where the tx was mined (log.BlockNumber)
	blockHash     common.Hash
	blockTime     time.Time
	txHash        common.Hash
	logIndex      uint
}

func defaultCheckpointFixture() checkpointFixture {
	return checkpointFixture{
		auctionAddr:   common.HexToAddress("0x5555555555555555555555555555555555555555"),
		blockNumber:   big.NewInt(950),
		clearingPrice: new(big.Int).Mul(big.NewInt(200), new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil)), // 200 * 2^96
		cumulativeMps: big.NewInt(15000),                                                                       // fits in uint24
		chainID:       324,
		txBlockNumber: 2000,
		blockHash:     common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
		blockTime:     time.Date(2025, 7, 1, 8, 30, 0, 0, time.UTC),
		txHash:        common.HexToHash("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
		logIndex:      2,
	}
}

// buildCheckpointLog constructs a types.Log mimicking a real CheckpointUpdated event.
// CheckpointUpdated has no indexed params, so topics is just [eventID].
// Data is ABI-encoded (uint256 blockNumber, uint256 clearingPrice, uint24 cumulativeMps).
func (f checkpointFixture) buildCheckpointLog(t *testing.T) types.Log {
	t.Helper()

	data, err := checkpointDataArgs.Pack(f.blockNumber, f.clearingPrice, f.cumulativeMps)
	if err != nil {
		t.Fatalf("failed to pack checkpoint data: %v", err)
	}

	return types.Log{
		Address: f.auctionAddr,
		Topics: []common.Hash{
			ethabi.CheckpointUpdatedEventID,
		},
		Data:        data,
		BlockNumber: f.txBlockNumber,
		BlockHash:   f.blockHash,
		TxHash:      f.txHash,
		Index:       f.logIndex,
	}
}

// TestCheckpointUpdated_DecodesFieldsCorrectly exercises the full decode path for a
// valid CheckpointUpdated log. It verifies the dual block-number semantics:
// BlockNumber comes from the event's data param (the auction's logical block),
// while TxBlockNumber comes from log.BlockNumber (the chain block where tx was mined).
// Also checks ClearingPriceQ96, CumulativeMps, and tx metadata.
func TestCheckpointUpdated_DecodesFieldsCorrectly(t *testing.T) {
	fix := defaultCheckpointFixture()
	s := newMockStore()
	h := &CheckpointUpdatedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildCheckpointLog(t), fix.blockTime, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	cp := s.checkpointRepo.InsertedCheckpoint
	if cp == nil {
		t.Fatal("expected checkpoint to be inserted, got nil")
	}

	// BlockNumber is the auction's logical block decoded from event data,
	// NOT the chain block where the tx was mined.
	wantBlockNumber := fix.blockNumber.Uint64()
	if cp.BlockNumber != wantBlockNumber {
		t.Errorf("BlockNumber = %d, want %d", cp.BlockNumber, wantBlockNumber)
	}

	// TxBlockNumber is the chain block from log.BlockNumber.
	wantTxBlockNumber := fix.txBlockNumber
	if cp.TxBlockNumber != wantTxBlockNumber {
		t.Errorf("TxBlockNumber = %d, want %d", cp.TxBlockNumber, wantTxBlockNumber)
	}

	// ABI-decoded Q96 clearing price.
	wantClearingPrice := fix.clearingPrice
	if cp.ClearingPriceQ96.Cmp(wantClearingPrice) != 0 {
		t.Errorf("ClearingPriceQ96 = %s, want %s", cp.ClearingPriceQ96.String(), wantClearingPrice.String())
	}

	// ABI-decoded cumulative MPS, truncated to uint32.
	wantCumulativeMps := uint32(fix.cumulativeMps.Uint64())
	if cp.CumulativeMps != wantCumulativeMps {
		t.Errorf("CumulativeMps = %d, want %d", cp.CumulativeMps, wantCumulativeMps)
	}

	// log.Address is the auction contract address.
	wantAuctionAddr := fix.auctionAddr
	if cp.AuctionAddress != wantAuctionAddr {
		t.Errorf("AuctionAddress = %s, want %s", cp.AuctionAddress.Hex(), wantAuctionAddr.Hex())
	}

	wantChainID := fix.chainID
	if cp.ChainID != wantChainID {
		t.Errorf("ChainID = %d, want %d", cp.ChainID, wantChainID)
	}

	// TxBlockTime comes from the blockTime param passed to Handle.
	wantTxBlockTime := fix.blockTime
	if !cp.TxBlockTime.Equal(wantTxBlockTime) {
		t.Errorf("TxBlockTime = %v, want %v", cp.TxBlockTime, wantTxBlockTime)
	}

	wantTxHash := fix.txHash
	if cp.TxHash != wantTxHash {
		t.Errorf("TxHash = %s, want %s", cp.TxHash.Hex(), wantTxHash.Hex())
	}

	wantLogIndex := fix.logIndex
	if cp.LogIndex != wantLogIndex {
		t.Errorf("LogIndex = %d, want %d", cp.LogIndex, wantLogIndex)
	}
}

// TestCheckpointUpdated_InsertsRawEvent verifies that Handle persists a RawEvent
// with the correct EventName and DecodedJSON containing all checkpoint fields
// (blockNumber, clearingPrice, cumulativeMps).
func TestCheckpointUpdated_InsertsRawEvent(t *testing.T) {
	fix := defaultCheckpointFixture()
	s := newMockStore()
	h := &CheckpointUpdatedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildCheckpointLog(t), fix.blockTime, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	ev := s.rawEventRepo.InsertedEvent
	if ev == nil {
		t.Fatal("expected raw event to be inserted, got nil")
	}

	wantEventName := "CheckpointUpdated"
	if ev.EventName != wantEventName {
		t.Errorf("EventName = %q, want %q", ev.EventName, wantEventName)
	}

	// Only 1 topic (event sig) since CheckpointUpdated has no indexed params.
	var topics []string
	if err := json.Unmarshal([]byte(ev.TopicsJSON), &topics); err != nil {
		t.Fatalf("TopicsJSON is not valid JSON: %v", err)
	}
	wantTopicCount := 1
	if len(topics) != wantTopicCount {
		t.Fatalf("TopicsJSON length = %d, want %d", len(topics), wantTopicCount)
	}
	wantTopic0 := ethabi.CheckpointUpdatedEventID.Hex()
	if topics[0] != wantTopic0 {
		t.Errorf("topics[0] = %s, want %s", topics[0], wantTopic0)
	}

	// DecodedJSON should contain all three decoded checkpoint fields.
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(ev.DecodedJSON), &decoded); err != nil {
		t.Fatalf("DecodedJSON is not valid JSON: %v", err)
	}

	// blockNumber is serialized as a string (big.Int.String()).
	wantDecodedBlockNumber := fix.blockNumber.String()
	if got, _ := decoded["blockNumber"].(string); got != wantDecodedBlockNumber {
		t.Errorf("decoded[blockNumber] = %q, want %q", got, wantDecodedBlockNumber)
	}

	// clearingPrice is serialized as a string.
	wantDecodedClearingPrice := fix.clearingPrice.String()
	if got, _ := decoded["clearingPrice"].(string); got != wantDecodedClearingPrice {
		t.Errorf("decoded[clearingPrice] = %q, want %q", got, wantDecodedClearingPrice)
	}

	// cumulativeMps is serialized as a JSON number (uint64 via Uint64()).
	wantDecodedMps := float64(fix.cumulativeMps.Uint64())
	if got, _ := decoded["cumulativeMps"].(float64); got != wantDecodedMps {
		t.Errorf("decoded[cumulativeMps] = %v, want %v", got, wantDecodedMps)
	}
}
