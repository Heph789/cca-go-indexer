package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	ethabi "github.com/cca/go-indexer/internal/eth/abi"
)

type auctionParameters struct {
	Currency               common.Address
	TokensRecipient        common.Address
	FundsRecipient         common.Address
	StartBlock             uint64
	EndBlock               uint64
	ClaimBlock             uint64
	TickSpacing            *big.Int
	ValidationHook         common.Address
	FloorPrice             *big.Int
	RequiredCurrencyRaised *big.Int
	AuctionStepsData       []byte
}

func buildTestLogData(t *testing.T, amount *big.Int, params auctionParameters) []byte {
	t.Helper()

	// Pack as a tuple struct — matches how Solidity's abi.encode(AuctionParameters) works
	paramStruct := struct {
		Currency               common.Address
		TokensRecipient        common.Address
		FundsRecipient         common.Address
		StartBlock             uint64
		EndBlock               uint64
		ClaimBlock             uint64
		TickSpacing            *big.Int
		ValidationHook         common.Address
		FloorPrice             *big.Int
		RequiredCurrencyRaised *big.Int
		AuctionStepsData       []byte
	}{
		Currency:               params.Currency,
		TokensRecipient:        params.TokensRecipient,
		FundsRecipient:         params.FundsRecipient,
		StartBlock:             params.StartBlock,
		EndBlock:               params.EndBlock,
		ClaimBlock:             params.ClaimBlock,
		TickSpacing:            params.TickSpacing,
		ValidationHook:         params.ValidationHook,
		FloorPrice:             params.FloorPrice,
		RequiredCurrencyRaised: params.RequiredCurrencyRaised,
		AuctionStepsData:       params.AuctionStepsData,
	}

	configData, err := auctionParamsArgs.Pack(paramStruct)
	if err != nil {
		t.Fatalf("failed to pack configData: %v", err)
	}

	data, err := eventDataArgs.Pack(amount, configData)
	if err != nil {
		t.Fatalf("failed to pack event data: %v", err)
	}
	return data
}

type testFixture struct {
	auctionAddr common.Address
	tokenAddr   common.Address
	amount      *big.Int
	params      auctionParameters
	chainID     int64
	blockNumber uint64
	blockHash   common.Hash
	txHash      common.Hash
	logIndex    uint
}

func defaultFixture() testFixture {
	return testFixture{
		auctionAddr: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		tokenAddr:   common.HexToAddress("0x2222222222222222222222222222222222222222"),
		amount:      big.NewInt(1_000_000),
		params: auctionParameters{
			Currency:               common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
			TokensRecipient:        common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"),
			FundsRecipient:         common.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"),
			StartBlock:             100,
			EndBlock:               200,
			ClaimBlock:             250,
			TickSpacing:            big.NewInt(10),
			ValidationHook:         common.HexToAddress("0xDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"),
			FloorPrice:             big.NewInt(500),
			RequiredCurrencyRaised: big.NewInt(10_000),
			AuctionStepsData:       []byte{0x01, 0x02, 0x03},
		},
		chainID:     324,
		blockNumber: 42,
		blockHash:   common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		txHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		logIndex:    3,
	}
}

func (f testFixture) buildLog(t *testing.T) types.Log {
	t.Helper()
	return types.Log{
		Address: common.HexToAddress("0xFactoryFactoryFactoryFactory0000"),
		Topics: []common.Hash{
			ethabi.AuctionCreatedEventID,
			common.BytesToHash(f.auctionAddr.Bytes()),
			common.BytesToHash(f.tokenAddr.Bytes()),
		},
		Data:        buildTestLogData(t, f.amount, f.params),
		BlockNumber: f.blockNumber,
		BlockHash:   f.blockHash,
		TxHash:      f.txHash,
		Index:       f.logIndex,
	}
}

func TestEventName(t *testing.T) {
	h := &AuctionCreatedHandler{}
	if got := h.EventName(); got != "AuctionCreated" {
		t.Errorf("EventName() = %q, want %q", got, "AuctionCreated")
	}
}

func TestEventID(t *testing.T) {
	h := &AuctionCreatedHandler{}
	if got := h.EventID(); got != ethabi.AuctionCreatedEventID {
		t.Errorf("EventID() = %s, want %s", got.Hex(), ethabi.AuctionCreatedEventID.Hex())
	}
}

func TestHandle_DecodesIndexedFields(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), time.Time{}, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	auction := s.auctionRepo.InsertedAuction
	if auction == nil {
		t.Fatal("expected auction to be inserted")
	}
	if auction.AuctionAddress != fix.auctionAddr {
		t.Errorf("AuctionAddress = %s, want %s", auction.AuctionAddress.Hex(), fix.auctionAddr.Hex())
	}
	if auction.Token != fix.tokenAddr {
		t.Errorf("Token = %s, want %s", auction.Token.Hex(), fix.tokenAddr.Hex())
	}
}

func TestHandle_DecodesNonIndexedFields(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), time.Time{}, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	auction := s.auctionRepo.InsertedAuction
	if auction == nil {
		t.Fatal("expected auction to be inserted")
	}
	if auction.Amount.Cmp(fix.amount) != 0 {
		t.Errorf("Amount = %s, want %s", auction.Amount.String(), fix.amount.String())
	}
}

func TestHandle_DecodesConfigData(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), time.Time{}, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	a := s.auctionRepo.InsertedAuction
	if a == nil {
		t.Fatal("expected auction to be inserted")
	}

	if a.Currency != fix.params.Currency {
		t.Errorf("Currency = %s, want %s", a.Currency.Hex(), fix.params.Currency.Hex())
	}
	if a.TokensRecipient != fix.params.TokensRecipient {
		t.Errorf("TokensRecipient = %s, want %s", a.TokensRecipient.Hex(), fix.params.TokensRecipient.Hex())
	}
	if a.FundsRecipient != fix.params.FundsRecipient {
		t.Errorf("FundsRecipient = %s, want %s", a.FundsRecipient.Hex(), fix.params.FundsRecipient.Hex())
	}
	if a.StartBlock != fix.params.StartBlock {
		t.Errorf("StartBlock = %d, want %d", a.StartBlock, fix.params.StartBlock)
	}
	if a.EndBlock != fix.params.EndBlock {
		t.Errorf("EndBlock = %d, want %d", a.EndBlock, fix.params.EndBlock)
	}
	if a.ClaimBlock != fix.params.ClaimBlock {
		t.Errorf("ClaimBlock = %d, want %d", a.ClaimBlock, fix.params.ClaimBlock)
	}
	if a.TickSpacing.Cmp(fix.params.TickSpacing) != 0 {
		t.Errorf("TickSpacing = %s, want %s", a.TickSpacing.String(), fix.params.TickSpacing.String())
	}
	if a.ValidationHook != fix.params.ValidationHook {
		t.Errorf("ValidationHook = %s, want %s", a.ValidationHook.Hex(), fix.params.ValidationHook.Hex())
	}
	if a.FloorPrice.Cmp(fix.params.FloorPrice) != 0 {
		t.Errorf("FloorPrice = %s, want %s", a.FloorPrice.String(), fix.params.FloorPrice.String())
	}
	if a.RequiredCurrencyRaised.Cmp(fix.params.RequiredCurrencyRaised) != 0 {
		t.Errorf("RequiredCurrencyRaised = %s, want %s", a.RequiredCurrencyRaised.String(), fix.params.RequiredCurrencyRaised.String())
	}
	if !bytes.Equal(a.AuctionStepsData, fix.params.AuctionStepsData) {
		t.Errorf("AuctionStepsData = %x, want %x", a.AuctionStepsData, fix.params.AuctionStepsData)
	}
}

func TestHandle_InsertsRawEvent(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	logEntry := fix.buildLog(t)
	err := h.Handle(context.Background(), fix.chainID, logEntry, time.Time{}, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	ev := s.rawEventRepo.InsertedEvent
	if ev == nil {
		t.Fatal("expected raw event to be inserted")
	}
	if ev.ChainID != fix.chainID {
		t.Errorf("ChainID = %d, want %d", ev.ChainID, fix.chainID)
	}
	if ev.BlockNumber != fix.blockNumber {
		t.Errorf("BlockNumber = %d, want %d", ev.BlockNumber, fix.blockNumber)
	}
	if ev.BlockHash != fix.blockHash {
		t.Errorf("BlockHash = %s, want %s", ev.BlockHash.Hex(), fix.blockHash.Hex())
	}
	if ev.TxHash != fix.txHash {
		t.Errorf("TxHash = %s, want %s", ev.TxHash.Hex(), fix.txHash.Hex())
	}
	if ev.LogIndex != fix.logIndex {
		t.Errorf("LogIndex = %d, want %d", ev.LogIndex, fix.logIndex)
	}
	if ev.EventName != "AuctionCreated" {
		t.Errorf("EventName = %q, want %q", ev.EventName, "AuctionCreated")
	}

	var topics []string
	if err := json.Unmarshal([]byte(ev.TopicsJSON), &topics); err != nil {
		t.Fatalf("TopicsJSON is not valid JSON: %v", err)
	}
	if len(topics) != 3 {
		t.Fatalf("TopicsJSON length = %d, want 3", len(topics))
	}
	if topics[0] != ethabi.AuctionCreatedEventID.Hex() {
		t.Errorf("topics[0] = %s, want %s", topics[0], ethabi.AuctionCreatedEventID.Hex())
	}

	if ev.DataHex == "" {
		t.Error("DataHex should not be empty")
	}

	if ev.DecodedJSON == "" {
		t.Error("DecodedJSON should not be empty")
	}
}

func TestHandle_InsertsTypedAuction(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), time.Time{}, s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	a := s.auctionRepo.InsertedAuction
	if a == nil {
		t.Fatal("expected auction to be inserted")
	}

	if a.ChainID != fix.chainID {
		t.Errorf("ChainID = %d, want %d", a.ChainID, fix.chainID)
	}
	if a.BlockNumber != fix.blockNumber {
		t.Errorf("BlockNumber = %d, want %d", a.BlockNumber, fix.blockNumber)
	}
	if a.TxHash != fix.txHash {
		t.Errorf("TxHash = %s, want %s", a.TxHash.Hex(), fix.txHash.Hex())
	}
	if a.LogIndex != fix.logIndex {
		t.Errorf("LogIndex = %d, want %d", a.LogIndex, fix.logIndex)
	}
	if a.AuctionAddress != fix.auctionAddr {
		t.Errorf("AuctionAddress = %s, want %s", a.AuctionAddress.Hex(), fix.auctionAddr.Hex())
	}
	if a.Token != fix.tokenAddr {
		t.Errorf("Token = %s, want %s", a.Token.Hex(), fix.tokenAddr.Hex())
	}
	if a.Amount.Cmp(fix.amount) != 0 {
		t.Errorf("Amount = %s, want %s", a.Amount.String(), fix.amount.String())
	}
}

func TestHandle_ReturnsErrorOnTooFewTopics(t *testing.T) {
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	logEntry := types.Log{
		Address: common.HexToAddress("0xFactoryFactoryFactoryFactory0000"),
		Topics:  []common.Hash{ethabi.AuctionCreatedEventID},
		Data:    []byte{},
	}

	err := h.Handle(context.Background(), 324, logEntry, time.Time{}, s)
	if err == nil {
		t.Fatal("expected error for too few topics, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "expected 3 topics") {
		t.Errorf("error = %q, want it to contain %q", got, "expected 3 topics")
	}
}

func TestHandle_ReturnsErrorOnMalformedData(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	logEntry := fix.buildLog(t)
	logEntry.Data = logEntry.Data[:10]

	err := h.Handle(context.Background(), fix.chainID, logEntry, time.Time{}, s)
	if err == nil {
		t.Fatal("expected error for malformed log data, got nil")
	}
}

func TestHandle_PropagatesRawEventInsertError(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	wantErr := errors.New("raw event insert failed")
	s.rawEventRepo.InsertFn = func(ctx context.Context, event *cca.RawEvent) error {
		return wantErr
	}

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), time.Time{}, s)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

func TestHandle_PropagatesAuctionInsertError(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	wantErr := errors.New("auction insert failed")
	s.auctionRepo.InsertFn = func(ctx context.Context, auction *cca.Auction) error {
		return wantErr
	}

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), time.Time{}, s)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}
