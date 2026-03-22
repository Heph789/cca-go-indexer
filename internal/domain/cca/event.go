package cca

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// RawEvent stores every log as-is for auditing and replay.
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
	IndexedAt   time.Time
}
