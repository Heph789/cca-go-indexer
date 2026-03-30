// Package pagination provides cursor-based pagination utilities for the GraphQL API.
// Cursors encode a (blockNumber, logIndex) pair as an opaque base64 string,
// enabling stable, deterministic pagination over blockchain-ordered data.
package pagination

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultLimit is the number of items returned when no limit is specified.
	DefaultLimit = 20
	// MaxLimit is the maximum number of items a client can request in one page.
	MaxLimit = 100
)

// EncodeCursor encodes a (blockNumber, logIndex) pair as an opaque base64 string
// suitable for use as a Relay-style pagination cursor.
func EncodeCursor(blockNumber uint64, logIndex uint) string {
	raw := fmt.Sprintf("%d:%d", blockNumber, logIndex)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor decodes an opaque cursor string back into (blockNumber, logIndex).
// Returns an error if the cursor is malformed or contains invalid values.
func DecodeCursor(s string) (uint64, uint, error) {
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	parts := strings.SplitN(string(data), ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid cursor format")
	}

	blockNumber, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cursor block number: %w", err)
	}

	logIndex, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cursor log index: %w", err)
	}

	return blockNumber, uint(logIndex), nil
}

// ClampLimit returns DefaultLimit if requested is <= 0, otherwise min(requested, MaxLimit).
func ClampLimit(requested int) int {
	if requested <= 0 {
		return DefaultLimit
	}
	if requested > MaxLimit {
		return MaxLimit
	}
	return requested
}
