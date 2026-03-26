package cca

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type RawEvent struct {
	ChainID     int64
	BlockNumber uint64
	BlockHash   common.Hash
	TxHash      common.Hash
	LogIndex    uint
	Address     common.Address
	EventName   string
	TopicsJSON  string
	DataHex     string
	DecodedJSON string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
