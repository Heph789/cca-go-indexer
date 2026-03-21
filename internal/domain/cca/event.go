package cca

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// RawEvent stores every log as-is for auditing and replay.
// If an event type's schema changes, raw data can re-derive typed records.
type RawEvent struct {
	ChainID     int64
	BlockNumber uint64
	BlockHash   common.Hash
	TxHash      common.Hash
	LogIndex    uint
	Address     common.Address
	EventName   string
	TopicsJSON  string // JSON array of topic hex strings
	DataHex     string // hex-encoded log data
	DecodedJSON string // JSON representation of decoded fields
	IndexedAt   time.Time
}
