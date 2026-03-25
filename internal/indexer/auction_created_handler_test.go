package indexer

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cca/go-indexer/internal/domain/cca"
)

// TestAuctionCreatedHandler_Identity verifies EventName and EventID.
func TestAuctionCreatedHandler_Identity(t *testing.T) {
	h := &AuctionCreatedHandler{}

	if h.EventName() != "AuctionCreated" {
		t.Fatalf("EventName() = %q, want %q", h.EventName(), "AuctionCreated")
	}

	want := crypto.Keccak256Hash([]byte("AuctionCreated(address,address,uint256,bytes)"))
	if h.EventID() != want {
		t.Fatalf("EventID() = %s, want %s", h.EventID().Hex(), want.Hex())
	}
}

// TestAuctionCreatedHandler_Handle verifies that Handle decodes a synthetic
// AuctionCreated log into the correct Auction and RawEvent, and inserts both.
func TestAuctionCreatedHandler_Handle(t *testing.T) {
	h := &AuctionCreatedHandler{}
	s := newFakeStore()

	// Test values
	auctionAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tokenAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	totalSupply := big.NewInt(1000000)

	currency := common.HexToAddress("0x3333333333333333333333333333333333333333")
	tokensRecipient := common.HexToAddress("0x4444444444444444444444444444444444444444")
	fundsRecipient := common.HexToAddress("0x5555555555555555555555555555555555555555")
	startBlock := uint64(100)
	endBlock := uint64(200)
	claimBlock := uint64(250)
	tickSpacing := big.NewInt(1000)
	validationHook := common.HexToAddress("0x6666666666666666666666666666666666666666")
	floorPrice := big.NewInt(5000)
	requiredCurrencyRaised := big.NewInt(999)
	auctionStepsData := []byte{0x01, 0x02, 0x03}

	// ABI-encode configData (AuctionParameters tuple)
	configDataArgs := abi.Arguments{
		{Type: mustABIType("address")},  // currency
		{Type: mustABIType("address")},  // tokensRecipient
		{Type: mustABIType("address")},  // fundsRecipient
		{Type: mustABIType("uint64")},   // startBlock
		{Type: mustABIType("uint64")},   // endBlock
		{Type: mustABIType("uint64")},   // claimBlock
		{Type: mustABIType("uint256")},  // tickSpacing
		{Type: mustABIType("address")},  // validationHook
		{Type: mustABIType("uint256")},  // floorPrice
		{Type: mustABIType("uint128")},  // requiredCurrencyRaised
		{Type: mustABIType("bytes")},    // auctionStepsData
	}
	configData, err := configDataArgs.Pack(
		currency, tokensRecipient, fundsRecipient,
		startBlock, endBlock, claimBlock,
		tickSpacing, validationHook, floorPrice,
		requiredCurrencyRaised, auctionStepsData,
	)
	if err != nil {
		t.Fatalf("failed to pack configData: %v", err)
	}

	// ABI-encode log.Data: (uint256 amount, bytes configData)
	logDataArgs := abi.Arguments{
		{Type: mustABIType("uint256")}, // amount
		{Type: mustABIType("bytes")},   // configData
	}
	logData, err := logDataArgs.Pack(totalSupply, configData)
	if err != nil {
		t.Fatalf("failed to pack log data: %v", err)
	}

	factoryAddr := common.HexToAddress("0xCCccCcCAE7503Cac057829BF2811De42E16e0bD5")
	txHash := common.HexToHash("0xabcd")
	blockHash := common.HexToHash("0xef01")

	log := types.Log{
		Address: factoryAddr,
		Topics: []common.Hash{
			h.EventID(),
			common.BytesToHash(auctionAddr.Bytes()),
			common.BytesToHash(tokenAddr.Bytes()),
		},
		Data:        logData,
		BlockNumber: 42,
		TxHash:      txHash,
		BlockHash:   blockHash,
		Index:       7,
	}

	chainID := int64(1)
	if err := h.Handle(context.Background(), chainID, log, s); err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	// Verify Auction was inserted
	if len(s.auction.insertCalls) != 1 {
		t.Fatalf("expected 1 auction insert, got %d", len(s.auction.insertCalls))
	}

	wantAuction := &cca.Auction{
		AuctionAddress:         auctionAddr,
		Token:                  tokenAddr,
		TotalSupply:            totalSupply,
		Currency:               currency,
		TokensRecipient:        tokensRecipient,
		FundsRecipient:         fundsRecipient,
		StartBlock:             startBlock,
		EndBlock:               endBlock,
		ClaimBlock:             claimBlock,
		TickSpacingQ96:         tickSpacing,
		ValidationHook:         validationHook,
		FloorPriceQ96:          floorPrice,
		RequiredCurrencyRaised: requiredCurrencyRaised,
		AuctionStepsData:       auctionStepsData,
		EmitterContract:        factoryAddr,
		ChainID:                chainID,
		BlockNumber:            42,
		TxHash:                 txHash,
		LogIndex:               7,
	}

	bigIntComparer := cmp.Comparer(func(a, b *big.Int) bool { return a.Cmp(b) == 0 })

	if diff := cmp.Diff(wantAuction, s.auction.insertCalls[0], bigIntComparer, cmpopts.IgnoreFields(cca.Auction{}, "CreatedAt")); diff != "" {
		t.Errorf("Auction mismatch (-want +got):\n%s", diff)
	}

	// Verify RawEvent was inserted
	if len(s.rawEvent.insertCalls) != 1 {
		t.Fatalf("expected 1 raw event insert, got %d", len(s.rawEvent.insertCalls))
	}

	wantRawEvent := &cca.RawEvent{
		ChainID:     chainID,
		BlockNumber: 42,
		BlockHash:   blockHash,
		TxHash:      txHash,
		LogIndex:    7,
		Address:     factoryAddr,
		EventName:   "AuctionCreated",
	}

	if diff := cmp.Diff(wantRawEvent, s.rawEvent.insertCalls[0], cmpopts.IgnoreFields(cca.RawEvent{}, "TopicsJSON", "DataHex", "DecodedJSON", "IndexedAt")); diff != "" {
		t.Errorf("RawEvent mismatch (-want +got):\n%s", diff)
	}
}

// TODO: Add unhappy-path tests:
// - TestAuctionCreatedHandler_Handle_MalformedData (truncated / garbage log.Data)
// - TestAuctionCreatedHandler_Handle_MissingTopics (fewer than 3 topics)
// - TestAuctionCreatedHandler_Handle_InvalidConfigData (configData that fails ABI decode)

// mustABIType is a test helper that parses an ABI type string or panics.
func mustABIType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic("bad abi type: " + t + ": " + err.Error())
	}
	return typ
}
