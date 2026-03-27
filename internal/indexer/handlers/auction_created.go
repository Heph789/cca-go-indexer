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
	if len(log.Topics) < 3 {
		return fmt.Errorf("expected 3 topics, got %d", len(log.Topics))
	}
	auctionAddr := common.BytesToAddress(log.Topics[1].Bytes())
	tokenAddr := common.BytesToAddress(log.Topics[2].Bytes())

	vals, err := eventDataArgs.Unpack(log.Data)
	if err != nil {
		return fmt.Errorf("unpack event data: %w", err)
	}
	amount := vals[0].(*big.Int)
	configData := vals[1].([]byte)

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
	auctionStepsData := paramVals[10].([]byte)

	topicStrs := make([]string, len(log.Topics))
	for i, t := range log.Topics {
		topicStrs[i] = t.Hex()
	}
	topicsJSON, err := json.Marshal(topicStrs)
	if err != nil {
		return fmt.Errorf("marshal topics: %w", err)
	}

	decoded := map[string]any{
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
		"auctionStepsData":       "0x" + hex.EncodeToString(auctionStepsData),
	}
	decodedJSON, err := json.Marshal(decoded)
	if err != nil {
		return fmt.Errorf("marshal decoded data: %w", err)
	}

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
		AuctionStepsData:       auctionStepsData,
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
