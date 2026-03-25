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

var auctionCreatedEventID = crypto.Keccak256Hash([]byte("AuctionCreated(address,address,uint256,bytes)"))

// AuctionCreatedHandler decodes AuctionCreated logs from the factory contract.
type AuctionCreatedHandler struct{}

func (h *AuctionCreatedHandler) EventName() string    { return "AuctionCreated" }
func (h *AuctionCreatedHandler) EventID() common.Hash { return auctionCreatedEventID }

// Handle decodes the log into an Auction and RawEvent, then inserts both.
func (h *AuctionCreatedHandler) Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	if len(log.Topics) < 3 {
		return fmt.Errorf("AuctionCreated log: expected 3 topics, got %d", len(log.Topics))
	}

	// Indexed params from topics
	auctionAddr := common.BytesToAddress(log.Topics[1].Bytes())
	tokenAddr := common.BytesToAddress(log.Topics[2].Bytes())

	// Decode non-indexed params: (uint256 amount, bytes configData)
	logDataArgs := abi.Arguments{
		{Type: mustType("uint256")},
		{Type: mustType("bytes")},
	}
	vals, err := logDataArgs.Unpack(log.Data)
	if err != nil {
		return fmt.Errorf("AuctionCreated: unpack log data: %w", err)
	}
	totalSupply := vals[0].(*big.Int)
	configDataBytes := vals[1].([]byte)

	// Decode configData: AuctionParameters tuple
	configArgs := abi.Arguments{
		{Type: mustType("address")},  // currency
		{Type: mustType("address")},  // tokensRecipient
		{Type: mustType("address")},  // fundsRecipient
		{Type: mustType("uint64")},   // startBlock
		{Type: mustType("uint64")},   // endBlock
		{Type: mustType("uint64")},   // claimBlock
		{Type: mustType("uint256")},  // tickSpacing
		{Type: mustType("address")},  // validationHook
		{Type: mustType("uint256")},  // floorPrice
		{Type: mustType("uint128")},  // requiredCurrencyRaised
		{Type: mustType("bytes")},    // auctionStepsData
	}
	cfgVals, err := configArgs.Unpack(configDataBytes)
	if err != nil {
		return fmt.Errorf("AuctionCreated: unpack configData: %w", err)
	}

	auction := &cca.Auction{
		AuctionAddress:         auctionAddr,
		Token:                  tokenAddr,
		TotalSupply:            totalSupply,
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
		EmitterContract:        log.Address,
		ChainID:                chainID,
		BlockNumber:            log.BlockNumber,
		TxHash:                 log.TxHash,
		LogIndex:               log.Index,
		CreatedAt:              time.Now(),
	}

	rawEvent := buildRawEvent(h.EventName(), chainID, log)

	if err := s.RawEventRepo().Insert(ctx, rawEvent); err != nil {
		return fmt.Errorf("AuctionCreated: insert raw event: %w", err)
	}
	if err := s.AuctionRepo().Insert(ctx, auction); err != nil {
		return fmt.Errorf("AuctionCreated: insert auction: %w", err)
	}
	return nil
}

// buildRawEvent constructs a RawEvent from a log. Reusable by all handlers.
func buildRawEvent(eventName string, chainID int64, log types.Log) *cca.RawEvent {
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

// mustType parses an ABI type string or panics. Used at init time only.
func mustType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic("bad abi type: " + t + ": " + err.Error())
	}
	return typ
}
