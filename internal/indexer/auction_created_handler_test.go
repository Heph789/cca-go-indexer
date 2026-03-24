package indexer

// auction_created_handler_test.go tests the AuctionCreatedHandler — the
// component that decodes AuctionCreated logs emitted by the CCA factory
// contract into domain objects (Auction and RawEvent) and persists them.
//
// Test strategy:
//   - TestAuctionCreatedHandler_Identity: smoke test that EventName and EventID
//     return the correct values (guards against accidental signature changes).
//   - TestAuctionCreatedHandler_Handle: end-to-end decode test that constructs
//     a synthetic ABI-encoded log, runs it through Handle(), and verifies the
//     decoded Auction and RawEvent match expected values.
//
// The Handle test uses go-ethereum's abi package to encode the log data the
// same way a real Solidity contract would, making the test realistic without
// requiring an actual deployed contract.

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

// TestAuctionCreatedHandler_Identity verifies that EventName() and EventID()
// return the correct values.
//
// EventID must be the keccak256 of the exact Solidity signature string
// "AuctionCreated(address,address,uint256,bytes)". If the signature ever
// changes (e.g. adding a parameter), this test will catch the mismatch.
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

// TestAuctionCreatedHandler_Handle verifies full decoding of a synthetic
// AuctionCreated log.
//
// This test mirrors what happens on-chain:
//  1. We define all the auction parameters (addresses, block ranges, prices).
//  2. We ABI-encode the configData bytes (the nested AuctionParameters struct).
//  3. We ABI-encode the log.Data field (uint256 amount, bytes configData).
//  4. We construct a types.Log with the correct topics (event ID + indexed params).
//  5. We call Handle() and verify the decoded Auction and RawEvent.
//
// The test uses go-cmp for structural comparison, with:
//   - A custom comparer for *big.Int (since reflect.DeepEqual doesn't work for big.Int)
//   - cmpopts.IgnoreFields to skip time-dependent fields (CreatedAt, IndexedAt)
//     and fields derived from raw data (TopicsJSON, DataHex, DecodedJSON)
func TestAuctionCreatedHandler_Handle(t *testing.T) {
	h := &AuctionCreatedHandler{}
	s := newFakeStore()

	// --- Define test values for every field in the AuctionCreated event ---

	// Indexed parameters (stored in log topics, not data)
	auctionAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tokenAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Non-indexed parameter in log.Data
	totalSupply := big.NewInt(1000000)

	// Fields inside the nested configData (AuctionParameters struct)
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

	// --- Step 1: ABI-encode configData (the nested AuctionParameters struct) ---
	// This mirrors the Solidity struct encoding: each field is ABI-encoded
	// sequentially as if it were a function call with these parameter types.
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

	// --- Step 2: ABI-encode log.Data (amount + configData bytes) ---
	// The log's data field contains two non-indexed parameters:
	//   - uint256 amount (the total token supply)
	//   - bytes configData (the ABI-encoded AuctionParameters from step 1)
	logDataArgs := abi.Arguments{
		{Type: mustABIType("uint256")},
		{Type: mustABIType("bytes")},
	}
	logData, err := logDataArgs.Pack(totalSupply, configData)
	if err != nil {
		t.Fatalf("failed to pack log data: %v", err)
	}

	// --- Step 3: Build the synthetic Ethereum log ---
	// This mimics what eth_getLogs returns for a real AuctionCreated event.
	factoryAddr := common.HexToAddress("0xCCccCcCAE7503Cac057829BF2811De42E16e0bD5")
	txHash := common.HexToHash("0xabcd")
	blockHash := common.HexToHash("0xef01")

	log := types.Log{
		Address: factoryAddr, // the CCA factory contract that emitted the event
		Topics: []common.Hash{
			h.EventID(),                                  // topic0: event signature hash
			common.BytesToHash(auctionAddr.Bytes()),      // topic1: indexed auction address
			common.BytesToHash(tokenAddr.Bytes()),        // topic2: indexed token address
		},
		Data:        logData,
		BlockNumber: 42,
		TxHash:      txHash,
		BlockHash:   blockHash,
		Index:       7, // log index within the block
	}

	// --- Step 4: Run Handle() ---
	chainID := int64(1)
	if err := h.Handle(context.Background(), chainID, log, s); err != nil {
		t.Fatalf("Handle() error: %v", err)
	}

	// --- Step 5: Verify the decoded Auction ---
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

	// Custom comparer for *big.Int — reflect.DeepEqual doesn't work because
	// big.Int has unexported fields. This uses Cmp() for value comparison.
	bigIntComparer := cmp.Comparer(func(a, b *big.Int) bool { return a.Cmp(b) == 0 })

	// Ignore CreatedAt because it's set to time.Now() inside Handle().
	if diff := cmp.Diff(wantAuction, s.auction.insertCalls[0], bigIntComparer, cmpopts.IgnoreFields(cca.Auction{}, "CreatedAt")); diff != "" {
		t.Errorf("Auction mismatch (-want +got):\n%s", diff)
	}

	// --- Step 6: Verify the RawEvent ---
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

	// Ignore fields that are derived from the raw log data (TopicsJSON, DataHex)
	// and time-dependent fields (IndexedAt) — those are tested implicitly by
	// the fact that Insert was called successfully.
	if diff := cmp.Diff(wantRawEvent, s.rawEvent.insertCalls[0], cmpopts.IgnoreFields(cca.RawEvent{}, "TopicsJSON", "DataHex", "DecodedJSON", "IndexedAt")); diff != "" {
		t.Errorf("RawEvent mismatch (-want +got):\n%s", diff)
	}
}

// TODO: Add unhappy-path tests:
// - TestAuctionCreatedHandler_Handle_MalformedData (truncated / garbage log.Data)
// - TestAuctionCreatedHandler_Handle_MissingTopics (fewer than 3 topics)
// - TestAuctionCreatedHandler_Handle_InvalidConfigData (configData that fails ABI decode)

// mustABIType is a test helper that parses an ABI type string or panics.
// Used only in test setup to build abi.Arguments for encoding synthetic logs.
func mustABIType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic("bad abi type: " + t + ": " + err.Error())
	}
	return typ
}
