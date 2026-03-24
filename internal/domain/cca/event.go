package cca

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// RawEvent stores every log as-is for auditing and replay.
// Every event the indexer processes is persisted as a RawEvent in addition to
// its decoded form (e.g. Auction). This gives us a full audit trail and the
// ability to replay or re-decode events if the schema changes later.
type RawEvent struct {
	// Chain and block location of the log.
	ChainID     int64
	BlockNumber uint64
	BlockHash   common.Hash

	// Transaction-level identifiers.
	TxHash   common.Hash
	LogIndex uint

	// The contract that emitted the log.
	Address common.Address

	// Human-readable event name (e.g. "AuctionCreated") for filtering.
	EventName string

	// Raw log payload preserved verbatim for replay.
	TopicsJSON  string // JSON array of hex-encoded topic hashes
	DataHex     string // hex-encoded log.Data bytes
	DecodedJSON string // optional decoded payload (currently unused)

	// When this event was indexed (wall-clock time of the indexer).
	IndexedAt time.Time
}
