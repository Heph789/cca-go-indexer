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

var auctionParamsTupleType = mustABITupleType()

func mustABITupleType() abi.Type {
	t, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "currency", Type: "address"},
		{Name: "tokensRecipient", Type: "address"},
		{Name: "fundsRecipient", Type: "address"},
		{Name: "startBlock", Type: "uint64"},
		{Name: "endBlock", Type: "uint64"},
		{Name: "claimBlock", Type: "uint64"},
		{Name: "tickSpacing", Type: "uint256"},
		{Name: "validationHook", Type: "address"},
		{Name: "floorPrice", Type: "uint256"},
		{Name: "requiredCurrencyRaised", Type: "uint128"},
		{Name: "auctionStepsData", Type: "bytes"},
	})
	if err != nil {
		panic("bad abi tuple type: " + err.Error())
	}
	return t
}

var auctionParamsArgs = abi.Arguments{
	{Name: "params", Type: auctionParamsTupleType},
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

	params := paramVals[0].(struct {
		Currency               common.Address `json:"currency"`
		TokensRecipient        common.Address `json:"tokensRecipient"`
		FundsRecipient         common.Address `json:"fundsRecipient"`
		StartBlock             uint64         `json:"startBlock"`
		EndBlock               uint64         `json:"endBlock"`
		ClaimBlock             uint64         `json:"claimBlock"`
		TickSpacing            *big.Int       `json:"tickSpacing"`
		ValidationHook         common.Address `json:"validationHook"`
		FloorPrice             *big.Int       `json:"floorPrice"`
		RequiredCurrencyRaised *big.Int       `json:"requiredCurrencyRaised"`
		AuctionStepsData       []byte         `json:"auctionStepsData"`
	})

	currency := params.Currency
	tokensRecipient := params.TokensRecipient
	fundsRecipient := params.FundsRecipient
	startBlock := params.StartBlock
	endBlock := params.EndBlock
	claimBlock := params.ClaimBlock
	tickSpacing := params.TickSpacing
	validationHook := params.ValidationHook
	floorPrice := params.FloorPrice
	requiredCurrencyRaised := params.RequiredCurrencyRaised

	topicStrs := make([]string, len(log.Topics))
	for i, t := range log.Topics {
		topicStrs[i] = t.Hex()
	}
	topicsJSON, _ := json.Marshal(topicStrs)

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
