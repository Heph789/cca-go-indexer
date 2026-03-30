package cca

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// WatchedContract represents a contract whose events the indexer tracks
// alongside the factory address. Each contract maintains its own cursor
// (LastIndexedBlock) so it can be backfilled independently.
type WatchedContract struct {
	ChainID          int64
	Address          common.Address
	Label            string
	StartBlock       uint64    // block to begin indexing from
	StartBlockTime   time.Time // wall-clock time of the start block
	LastIndexedBlock uint64    // per-contract cursor (0 = not yet indexed)
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
