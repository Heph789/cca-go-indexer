package abi

import (
	"bytes"
	_ "embed"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

//go:embed ContinuousClearingAuction.json
var auctionABIJSON []byte

// AuctionABI is the parsed ABI for ContinuousClearingAuction events.
var AuctionABI abi.ABI

// BidSubmittedEventID is the keccak256 topic0 for the BidSubmitted event.
var BidSubmittedEventID common.Hash

// CheckpointUpdatedEventID is the keccak256 topic0 for the CheckpointUpdated event.
var CheckpointUpdatedEventID common.Hash

func init() {
	parsed, err := abi.JSON(bytes.NewReader(auctionABIJSON))
	if err != nil {
		panic("failed to parse auction ABI: " + err.Error())
	}
	AuctionABI = parsed
	BidSubmittedEventID = parsed.Events["BidSubmitted"].ID
	CheckpointUpdatedEventID = parsed.Events["CheckpointUpdated"].ID
}
