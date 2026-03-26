package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/cca/go-indexer/internal/domain/cca"
	ethabi "github.com/cca/go-indexer/internal/eth/abi"
	"github.com/cca/go-indexer/internal/store"
)

type AuctionCreatedHandler struct{}

func (h *AuctionCreatedHandler) EventName() string {
	return "AuctionCreated"
}

func (h *AuctionCreatedHandler) EventID() common.Hash {
	return ethabi.AuctionCreatedEventID
}

// auctionParamsArgs defines the ABI types for the configData tuple.
var auctionParamsArgs = abi.Arguments{
	{Name: "currency", Type: mustABIType("address")},
	{Name: "tokensRecipient", Type: mustABIType("address")},
	{Name: "fundsRecipient", Type: mustABIType("address")},
	{Name: "startBlock", Type: mustABIType("uint64")},
	{Name: "endBlock", Type: mustABIType("uint64")},
	{Name: "claimBlock", Type: mustABIType("uint64")},
	{Name: "tickSpacing", Type: mustABIType("uint256")},
	{Name: "validationHook", Type: mustABIType("address")},
	{Name: "floorPrice", Type: mustABIType("uint256")},
	{Name: "requiredCurrencyRaised", Type: mustABIType("uint128")},
	{Name: "auctionStepsData", Type: mustABIType("bytes")},
}

// eventDataArgs defines the ABI types for the non-indexed event fields.
var eventDataArgs = abi.Arguments{
	{Name: "amount", Type: mustABIType("uint256")},
	{Name: "configData", Type: mustABIType("bytes")},
}

func mustABIType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic("bad abi type: " + t + ": " + err.Error())
	}
	return typ
}

func (h *AuctionCreatedHandler) Handle(ctx context.Context, chainID int64, log types.Log, s store.Store) error {
	// Decode indexed fields from topics
	auctionAddr := common.BytesToAddress(log.Topics[1].Bytes())
	tokenAddr := common.BytesToAddress(log.Topics[2].Bytes())

	// Decode non-indexed fields from data
	vals, err := eventDataArgs.Unpack(log.Data)
	if err != nil {
		return fmt.Errorf("unpack event data: %w", err)
	}
	amount := vals[0].(*big.Int)
	configData := vals[1].([]byte)

	// Decode configData into auction parameters
	paramVals, err := auctionParamsArgs.Unpack(configData)
	if err != nil {
		return fmt.Errorf("unpack config data: %w", err)
	}
	currency := paramVals[0].(common.Address)
	tokensRecipient := paramVals[1].(common.Address)
	fundsRecipient := paramVals[2].(common.Address)
	startBlock := paramVals[3].(uint64)
	endBlock := paramVals[4].(uint64)
	claimBlock := paramVals[5].(uint64)
	tickSpacing := paramVals[6].(*big.Int)
	validationHook := paramVals[7].(common.Address)
	floorPrice := paramVals[8].(*big.Int)
	requiredCurrencyRaised := paramVals[9].(*big.Int)

	// Build TopicsJSON
	topicStrs := make([]string, len(log.Topics))
	for i, t := range log.Topics {
		topicStrs[i] = t.Hex()
	}
	topicsJSON, _ := json.Marshal(topicStrs)

	// Build DecodedJSON
	decoded := map[string]interface{}{
		"auctionAddress":         auctionAddr.Hex(),
		"token":                  tokenAddr.Hex(),
		"amount":                 amount.String(),
		"currency":               currency.Hex(),
		"tokensRecipient":        tokensRecipient.Hex(),
		"fundsRecipient":         fundsRecipient.Hex(),
		"startBlock":             startBlock,
		"endBlock":               endBlock,
		"claimBlock":             claimBlock,
		"tickSpacing":            tickSpacing.String(),
		"validationHook":         validationHook.Hex(),
		"floorPrice":             floorPrice.String(),
		"requiredCurrencyRaised": requiredCurrencyRaised.String(),
	}
	decodedJSON, _ := json.Marshal(decoded)

	// Insert raw event
	rawEvent := &cca.RawEvent{
		ChainID:     chainID,
		BlockNumber: log.BlockNumber,
		BlockHash:   log.BlockHash,
		TxHash:      log.TxHash,
		LogIndex:    log.Index,
		Address:     log.Address,
		EventName:   "AuctionCreated",
		TopicsJSON:  string(topicsJSON),
		DataHex:     "0x" + hex.EncodeToString(log.Data),
		DecodedJSON: string(decodedJSON),
	}
	if err := s.RawEventRepo().Insert(ctx, rawEvent); err != nil {
		return fmt.Errorf("insert raw event: %w", err)
	}

	// Insert typed auction
	auction := &cca.Auction{
		AuctionAddress:         auctionAddr,
		Token:                  tokenAddr,
		Amount:                 amount,
		Currency:               currency,
		TokensRecipient:        tokensRecipient,
		FundsRecipient:         fundsRecipient,
		StartBlock:             startBlock,
		EndBlock:               endBlock,
		ClaimBlock:             claimBlock,
		TickSpacing:            tickSpacing,
		ValidationHook:         validationHook,
		FloorPrice:             floorPrice,
		RequiredCurrencyRaised: requiredCurrencyRaised,
		ChainID:                chainID,
		BlockNumber:            log.BlockNumber,
		TxHash:                 log.TxHash,
		LogIndex:               log.Index,
	}
	if err := s.AuctionRepo().Insert(ctx, auction); err != nil {
		return fmt.Errorf("insert auction: %w", err)
	}

	return nil
}
