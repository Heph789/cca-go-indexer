package abi

import (
	"bytes"
	_ "embed"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

//go:embed ContinuousClearingAuctionFactory.json
var factoryABIJSON []byte

var FactoryABI abi.ABI
var AuctionCreatedEventID common.Hash

func init() {
	parsed, err := abi.JSON(bytes.NewReader(factoryABIJSON))
	if err != nil {
		panic("failed to parse factory ABI: " + err.Error())
	}
	FactoryABI = parsed
	AuctionCreatedEventID = parsed.Events["AuctionCreated"].ID
}
