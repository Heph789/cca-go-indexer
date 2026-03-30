package handlers

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	ethabi "github.com/cca/go-indexer/internal/eth/abi"
)

// TestBidSubmitted_EventName verifies that the handler reports the correct
// Solidity event name, which is used for log routing and raw event labeling.
func TestBidSubmitted_EventName(t *testing.T) {
	h := &BidSubmittedHandler{}
	wantName := "BidSubmitted"
	if got := h.EventName(); got != wantName {
		t.Errorf("EventName() = %q, want %q", got, wantName)
	}
}

// TestBidSubmitted_EventID verifies that the handler's topic0 matches the
// keccak256 hash registered in the ethabi package, ensuring log filtering
// picks up the correct events on-chain.
func TestBidSubmitted_EventID(t *testing.T) {
	h := &BidSubmittedHandler{}
	wantID := ethabi.BidSubmittedEventID
	if got := h.EventID(); got != wantID {
		t.Errorf("EventID() = %s, want %s", got.Hex(), wantID.Hex())
	}
}

// bidFixture holds deterministic test values for constructing a BidSubmitted log.
type bidFixture struct {
	auctionAddr common.Address
	bidID       *big.Int
	owner       common.Address
	price       *big.Int
	amount      *big.Int
	chainID     int64
	blockNumber uint64
	blockHash   common.Hash
	blockTime   time.Time
	txHash      common.Hash
	logIndex    uint
}

func defaultBidFixture() bidFixture {
	return bidFixture{
		auctionAddr: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		bidID:       big.NewInt(42),
		owner:       common.HexToAddress("0x4444444444444444444444444444444444444444"),
		price:       new(big.Int).Mul(big.NewInt(150), new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil)), // 150 * 2^96
		amount:      big.NewInt(500_000),
		chainID:     324,
		blockNumber: 1000,
		blockHash:   common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		blockTime:   time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		txHash:      common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
		logIndex:    7,
	}
}

// buildBidLog constructs a types.Log that mimics a real BidSubmitted event with
// 3 topics (event sig, bid ID, owner) and ABI-encoded data (price, amount).
func (f bidFixture) buildBidLog(t *testing.T) types.Log {
	t.Helper()

	data, err := bidDataArgs.Pack(f.price, f.amount)
	if err != nil {
		t.Fatalf("failed to pack bid data: %v", err)
	}

	return types.Log{
		Address: f.auctionAddr,
		Topics: []common.Hash{
			ethabi.BidSubmittedEventID,
			common.BigToHash(f.bidID),
			common.BytesToHash(f.owner.Bytes()),
		},
		Data:        data,
		BlockNumber: f.blockNumber,
		BlockHash:   f.blockHash,
		TxHash:      f.txHash,
		Index:       f.logIndex,
	}
}

// TestBidSubmitted_DecodesFieldsCorrectly exercises the full decode path for a
// valid BidSubmitted log. It verifies that every field on the persisted Bid
// domain object is correctly mapped from log topics, ABI-decoded data, and
// contextual metadata (chainID, blockTime, etc.).
func TestBidSubmitted_DecodesFieldsCorrectly(t *testing.T) {
	fix := defaultBidFixture()
	s := newMockStore()
	h := &BidSubmittedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildBidLog(t), fix.blockTime, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	bid := s.bidRepo.InsertedBid
	if bid == nil {
		t.Fatal("expected bid to be inserted, got nil")
	}

	// Indexed topic[1]: the bid identifier.
	wantID := fix.bidID.Uint64()
	if bid.ID != wantID {
		t.Errorf("ID = %d, want %d", bid.ID, wantID)
	}

	// Indexed topic[2]: the bidder's address.
	wantOwner := fix.owner
	if bid.Owner != wantOwner {
		t.Errorf("Owner = %s, want %s", bid.Owner.Hex(), wantOwner.Hex())
	}

	// ABI-decoded data field: the Q96 fixed-point price.
	wantPrice := fix.price
	if bid.PriceQ96.Cmp(wantPrice) != 0 {
		t.Errorf("PriceQ96 = %s, want %s", bid.PriceQ96.String(), wantPrice.String())
	}

	// ABI-decoded data field: the bid amount.
	wantAmount := fix.amount
	if bid.Amount.Cmp(wantAmount) != 0 {
		t.Errorf("Amount = %s, want %s", bid.Amount.String(), wantAmount.String())
	}

	// log.Address maps to the auction contract that emitted the event.
	wantAuctionAddr := fix.auctionAddr
	if bid.AuctionAddress != wantAuctionAddr {
		t.Errorf("AuctionAddress = %s, want %s", bid.AuctionAddress.Hex(), wantAuctionAddr.Hex())
	}

	// Contextual metadata passed into Handle.
	wantChainID := fix.chainID
	if bid.ChainID != wantChainID {
		t.Errorf("ChainID = %d, want %d", bid.ChainID, wantChainID)
	}

	wantBlockNumber := fix.blockNumber
	if bid.BlockNumber != wantBlockNumber {
		t.Errorf("BlockNumber = %d, want %d", bid.BlockNumber, wantBlockNumber)
	}

	wantBlockTime := fix.blockTime
	if !bid.BlockTime.Equal(wantBlockTime) {
		t.Errorf("BlockTime = %v, want %v", bid.BlockTime, wantBlockTime)
	}

	wantTxHash := fix.txHash
	if bid.TxHash != wantTxHash {
		t.Errorf("TxHash = %s, want %s", bid.TxHash.Hex(), wantTxHash.Hex())
	}

	wantLogIndex := fix.logIndex
	if bid.LogIndex != wantLogIndex {
		t.Errorf("LogIndex = %d, want %d", bid.LogIndex, wantLogIndex)
	}
}

// TestBidSubmitted_InsertsRawEvent verifies that Handle persists a RawEvent
// alongside the typed Bid. Checks EventName, topic count, and that the
// DecodedJSON contains the expected bid fields (id, owner, price, amount).
func TestBidSubmitted_InsertsRawEvent(t *testing.T) {
	fix := defaultBidFixture()
	s := newMockStore()
	h := &BidSubmittedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildBidLog(t), fix.blockTime, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	ev := s.rawEventRepo.InsertedEvent
	if ev == nil {
		t.Fatal("expected raw event to be inserted, got nil")
	}

	// The event name tag should match the handler's event name.
	wantEventName := "BidSubmitted"
	if ev.EventName != wantEventName {
		t.Errorf("EventName = %q, want %q", ev.EventName, wantEventName)
	}

	// Verify topic0 is the BidSubmitted event signature.
	var topics []string
	if err := json.Unmarshal([]byte(ev.TopicsJSON), &topics); err != nil {
		t.Fatalf("TopicsJSON is not valid JSON: %v", err)
	}
	wantTopicCount := 3
	if len(topics) != wantTopicCount {
		t.Fatalf("TopicsJSON length = %d, want %d", len(topics), wantTopicCount)
	}
	wantTopic0 := ethabi.BidSubmittedEventID.Hex()
	if topics[0] != wantTopic0 {
		t.Errorf("topics[0] = %s, want %s", topics[0], wantTopic0)
	}

	// DecodedJSON should contain the four decoded fields.
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(ev.DecodedJSON), &decoded); err != nil {
		t.Fatalf("DecodedJSON is not valid JSON: %v", err)
	}

	wantDecodedID := fix.bidID.String()
	if got, _ := decoded["id"].(string); got != wantDecodedID {
		t.Errorf("decoded[id] = %q, want %q", got, wantDecodedID)
	}

	wantDecodedOwner := fix.owner.Hex()
	if got, _ := decoded["owner"].(string); got != wantDecodedOwner {
		t.Errorf("decoded[owner] = %q, want %q", got, wantDecodedOwner)
	}

	wantDecodedPrice := fix.price.String()
	if got, _ := decoded["price"].(string); got != wantDecodedPrice {
		t.Errorf("decoded[price] = %q, want %q", got, wantDecodedPrice)
	}

	wantDecodedAmount := fix.amount.String()
	if got, _ := decoded["amount"].(string); got != wantDecodedAmount {
		t.Errorf("decoded[amount] = %q, want %q", got, wantDecodedAmount)
	}
}

// TestBidSubmitted_ErrorOnTooFewTopics verifies that Handle returns an error
// when the log has fewer than 3 topics. BidSubmitted requires topic0 (event sig),
// topic1 (bid ID), and topic2 (owner); missing topics indicate a malformed log.
func TestBidSubmitted_ErrorOnTooFewTopics(t *testing.T) {
	s := newMockStore()
	h := &BidSubmittedHandler{}

	// Only 1 topic (event sig) — missing bid ID and owner.
	logEntry := types.Log{
		Address: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		Topics:  []common.Hash{ethabi.BidSubmittedEventID},
		Data:    []byte{},
	}

	wantChainID := int64(324)
	err := h.Handle(context.Background(), wantChainID, logEntry, time.Time{}, s)
	if err == nil {
		t.Fatal("expected error for too few topics, got nil")
	}
	wantSubstring := "expected 3 topics"
	if got := err.Error(); !strings.Contains(got, wantSubstring) {
		t.Errorf("error = %q, want it to contain %q", got, wantSubstring)
	}
}
