package indexer

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
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
)

// auctionCreatedEventID is the keccak256 hash of the Solidity event signature:
//
//	event AuctionCreated(address indexed auction, address indexed token, uint256 amount, bytes configData)
//
// This is computed once at package init and used as the topic0 filter for eth_getLogs.
var auctionCreatedEventID = crypto.Keccak256Hash([]byte("AuctionCreated(address,address,uint256,bytes)"))

// AuctionCreatedHandler decodes AuctionCreated logs emitted by the CCA factory
// contract. Each log represents a new auction deployment and contains both the
// auction parameters and the token/auction addresses as indexed topics.
type AuctionCreatedHandler struct{}

func (h *AuctionCreatedHandler) EventName() string    { return "AuctionCreated" }
func (h *AuctionCreatedHandler) EventID() common.Hash { return auctionCreatedEventID }

// Handle decodes the log into an Auction and RawEvent, then inserts both.
//
// The AuctionCreated event has the following structure:
//   - topic0: event signature hash (AuctionCreated)
//   - topic1: auction contract address (indexed)
//   - topic2: token address (indexed)
//   - data:   ABI-encoded (uint256 amount, bytes configData)
//
// The configData bytes contain a nested ABI-encoded AuctionParameters struct
// with the auction's full configuration (currency, recipients, block range,
// pricing parameters, etc.).
func (h *AuctionCreatedHandler) Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	// AuctionCreated has 3 topics: event signature + 2 indexed params.
	if len(log.Topics) < 3 {
		return fmt.Errorf("AuctionCreated log: expected 3 topics, got %d", len(log.Topics))
	}

	// Extract indexed parameters from topics. Topics are 32-byte hashes;
	// for indexed address params, the address is left-padded with zeros.
	auctionAddr := common.BytesToAddress(log.Topics[1].Bytes())
	tokenAddr := common.BytesToAddress(log.Topics[2].Bytes())

	// Decode non-indexed params from log.Data.
	// The data field contains ABI-encoded: (uint256 amount, bytes configData)
	logDataArgs := abi.Arguments{
		{Type: mustType("uint256")}, // amount: total token supply for the auction
		{Type: mustType("bytes")},   // configData: nested ABI-encoded AuctionParameters
	}
	vals, err := logDataArgs.Unpack(log.Data)
	if err != nil {
		return fmt.Errorf("AuctionCreated: unpack log data: %w", err)
	}
	totalSupply := vals[0].(*big.Int)
	configDataBytes := vals[1].([]byte)

	// Decode the nested configData bytes into individual AuctionParameters fields.
	// This is a second level of ABI decoding — the configData bytes themselves
	// are an ABI-encoded tuple of the auction's configuration.
	configArgs := abi.Arguments{
		{Type: mustType("address")},  // currency: the ERC-20 used for bidding
		{Type: mustType("address")},  // tokensRecipient: receives unsold tokens
		{Type: mustType("address")},  // fundsRecipient: receives auction proceeds
		{Type: mustType("uint64")},   // startBlock: auction opens at this block
		{Type: mustType("uint64")},   // endBlock: auction closes at this block
		{Type: mustType("uint64")},   // claimBlock: winners can claim after this block
		{Type: mustType("uint256")},  // tickSpacing: price tick spacing in Q96 format
		{Type: mustType("address")},  // validationHook: optional bid validation contract
		{Type: mustType("uint256")},  // floorPrice: minimum bid price in Q96 format
		{Type: mustType("uint128")},  // requiredCurrencyRaised: minimum total raise (raw uint128)
		{Type: mustType("bytes")},    // auctionStepsData: encoded price curve steps
	}
	cfgVals, err := configArgs.Unpack(configDataBytes)
	if err != nil {
		return fmt.Errorf("AuctionCreated: unpack configData: %w", err)
	}

	// Assemble the domain object from decoded values + log metadata.
	auction := &cca.Auction{
		// From indexed topics
		AuctionAddress: auctionAddr,
		Token:          tokenAddr,

		// From non-indexed log data
		TotalSupply: totalSupply,

		// From decoded configData
		Currency:               cfgVals[0].(common.Address),
		TokensRecipient:        cfgVals[1].(common.Address),
		FundsRecipient:         cfgVals[2].(common.Address),
		StartBlock:             cfgVals[3].(uint64),
		EndBlock:               cfgVals[4].(uint64),
		ClaimBlock:             cfgVals[5].(uint64),
		TickSpacingQ96:         cfgVals[6].(*big.Int),
		ValidationHook:         cfgVals[7].(common.Address),
		FloorPriceQ96:          cfgVals[8].(*big.Int),
		RequiredCurrencyRaised: cfgVals[9].(*big.Int),
		AuctionStepsData:       cfgVals[10].([]byte),

		// Log metadata
		EmitterContract: log.Address,
		ChainID:         chainID,
		BlockNumber:     log.BlockNumber,
		TxHash:          log.TxHash,
		LogIndex:        log.Index,
		CreatedAt:       time.Now(),
	}

	// Persist both the raw event (for audit/replay) and the decoded auction.
	rawEvent := buildRawEvent(h.EventName(), chainID, log)

	if err := s.RawEventRepo().Insert(ctx, rawEvent); err != nil {
		return fmt.Errorf("AuctionCreated: insert raw event: %w", err)
	}
	if err := s.AuctionRepo().Insert(ctx, auction); err != nil {
		return fmt.Errorf("AuctionCreated: insert auction: %w", err)
	}
	return nil
}

// buildRawEvent constructs a RawEvent from a log, preserving the raw topics
// and data verbatim. This function is shared by all handlers so every event
// type gets an identical audit trail regardless of its decoded form.
func buildRawEvent(eventName string, chainID int64, log types.Log) *cca.RawEvent {
	// Convert topic hashes to hex strings and serialize as a JSON array.
	topicsHex := make([]string, len(log.Topics))
	for i, t := range log.Topics {
		topicsHex[i] = t.Hex()
	}
	topicsJSON, _ := json.Marshal(topicsHex)

	return &cca.RawEvent{
		ChainID:     chainID,
		BlockNumber: log.BlockNumber,
		BlockHash:   log.BlockHash,
		TxHash:      log.TxHash,
		LogIndex:    log.Index,
		Address:     log.Address,
		EventName:   eventName,
		TopicsJSON:  string(topicsJSON),
		DataHex:     hex.EncodeToString(log.Data),
		IndexedAt:   time.Now(),
	}
}

// mustType parses an ABI type string or panics. Only called during handler
// construction (effectively init time), so a panic here surfaces immediately
// at startup rather than silently producing wrong decodes at runtime.
func mustType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic("bad abi type: " + t + ": " + err.Error())
	}
	return typ
}
