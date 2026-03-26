package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	ethabi "github.com/cca/go-indexer/internal/eth/abi"
)

// AuctionParameters mirrors the configData tuple in the AuctionCreated event.
type AuctionParameters struct {
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

// auctionParamsABIArgs defines the ABI encoding for the AuctionParameters tuple.
var auctionParamsABIArgs = abi.Arguments{
	{Name: "currency", Type: mustType("address")},
	{Name: "tokensRecipient", Type: mustType("address")},
	{Name: "fundsRecipient", Type: mustType("address")},
	{Name: "startBlock", Type: mustType("uint64")},
	{Name: "endBlock", Type: mustType("uint64")},
	{Name: "claimBlock", Type: mustType("uint64")},
	{Name: "tickSpacing", Type: mustType("uint256")},
	{Name: "validationHook", Type: mustType("address")},
	{Name: "floorPrice", Type: mustType("uint256")},
	{Name: "requiredCurrencyRaised", Type: mustType("uint128")},
	{Name: "auctionStepsData", Type: mustType("bytes")},
}

// eventDataABIArgs defines the ABI encoding for the non-indexed event fields (amount, configData).
var eventDataABIArgs = abi.Arguments{
	{Name: "amount", Type: mustType("uint256")},
	{Name: "configData", Type: mustType("bytes")},
}

func mustType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic("bad abi type: " + t + ": " + err.Error())
	}
	return typ
}

// buildTestLogData ABI-encodes the non-indexed event data: (amount uint256, configData bytes).
// configData is itself the ABI-encoded AuctionParameters tuple.
func buildTestLogData(t *testing.T, amount *big.Int, params AuctionParameters) []byte {
	t.Helper()

	configData, err := auctionParamsABIArgs.Pack(
		params.Currency,
		params.TokensRecipient,
		params.FundsRecipient,
		params.StartBlock,
		params.EndBlock,
		params.ClaimBlock,
		params.TickSpacing,
		params.ValidationHook,
		params.FloorPrice,
		params.RequiredCurrencyRaised,
		params.AuctionStepsData,
	)
	if err != nil {
		t.Fatalf("failed to pack configData: %v", err)
	}

	data, err := eventDataABIArgs.Pack(amount, configData)
	if err != nil {
		t.Fatalf("failed to pack event data: %v", err)
	}
	return data
}

// testFixture holds shared test values used across multiple tests.
type testFixture struct {
	auctionAddr common.Address
	tokenAddr   common.Address
	amount      *big.Int
	params      AuctionParameters
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
		params: AuctionParameters{
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
		Address:     common.HexToAddress("0xFactoryFactoryFactoryFactory0000"),
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

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), s)
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

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), s)
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

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), s)
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
}

func TestHandle_InsertsRawEvent(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	logEntry := fix.buildLog(t)
	err := h.Handle(context.Background(), fix.chainID, logEntry, s)
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

	// TopicsJSON should be a JSON array of hex topic strings
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

	// DataHex should be hex-encoded log data
	if ev.DataHex == "" {
		t.Error("DataHex should not be empty")
	}

	// DecodedJSON should contain decoded field values
	if ev.DecodedJSON == "" {
		t.Error("DecodedJSON should not be empty")
	}
}

func TestHandle_InsertsTypedAuction(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), s)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	a := s.auctionRepo.InsertedAuction
	if a == nil {
		t.Fatal("expected auction to be inserted")
	}

	// Verify all fields are correctly mapped
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

func TestHandle_ReturnsErrorOnMalformedData(t *testing.T) {
	fix := defaultFixture()
	s := newMockStore()
	h := &AuctionCreatedHandler{}

	logEntry := fix.buildLog(t)
	// Truncate the data to make it invalid
	logEntry.Data = logEntry.Data[:10]

	err := h.Handle(context.Background(), fix.chainID, logEntry, s)
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

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), s)
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

	err := h.Handle(context.Background(), fix.chainID, fix.buildLog(t), s)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}
