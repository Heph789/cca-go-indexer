// Package abi embeds and parses the CCA factory contract ABI.
// Only the AuctionCreated event is needed for MVP.
package abi

import (
	"bytes"
	_ "embed"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ContinuousClearingAuctionFactory.json is the ABI for the CCA factory contract.
// Source: CCA contract repo or verified Sepolia explorer.
//
//go:embed ContinuousClearingAuctionFactory.json
var factoryABIJSON []byte

// FactoryABI is the parsed factory contract ABI.
var FactoryABI abi.ABI

// AuctionCreatedEventID is the topic0 hash for the AuctionCreated event.
// Used as the key in the handler registry's topic filter.
var AuctionCreatedEventID common.Hash

func init() {
	parsed, err := abi.JSON(bytes.NewReader(factoryABIJSON))
	if err != nil {
		panic("failed to parse factory ABI: " + err.Error())
	}
	FactoryABI = parsed
	AuctionCreatedEventID = parsed.Events["AuctionCreated"].ID
}
