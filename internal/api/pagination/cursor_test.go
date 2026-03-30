package pagination

import (
	"testing"
)

// TestEncodeCursor_RoundTrips verifies that EncodeCursor and DecodeCursor are
// inverses for a variety of (blockNumber, logIndex) pairs.
func TestEncodeCursor_RoundTrips(t *testing.T) {
	tests := []struct {
		name        string
		blockNumber uint64
		logIndex    uint
	}{
		{
			name:        "round-trips typical block and log index",
			blockNumber: 12345,
			logIndex:    7,
		},
		{
			name:        "round-trips zero block number and zero log index",
			blockNumber: 0,
			logIndex:    0,
		},
		{
			name:        "round-trips large block number near uint64 max",
			blockNumber: 18446744073709551615, // math.MaxUint64
			logIndex:    999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cursor := EncodeCursor(tt.blockNumber, tt.logIndex)
			if cursor == "" {
				t.Fatal("EncodeCursor returned empty string")
			}

			gotBlock, gotLogIndex, err := DecodeCursor(cursor)
			if err != nil {
				t.Fatalf("DecodeCursor(%q) error = %v", cursor, err)
			}
			if gotBlock != tt.blockNumber {
				t.Errorf("blockNumber = %d, want %d", gotBlock, tt.blockNumber)
			}
			if gotLogIndex != tt.logIndex {
				t.Errorf("logIndex = %d, want %d", gotLogIndex, tt.logIndex)
			}
		})
	}
}

// TestEncodeCursor_ProducesOpaqueString verifies that the encoded cursor is
// a non-trivial base64 value, not the raw "blockNumber:logIndex" string.
func TestEncodeCursor_ProducesOpaqueString(t *testing.T) {
	cursor := EncodeCursor(100, 5)
	if cursor == "" {
		t.Fatal("EncodeCursor(100, 5) produced empty string")
	}
	if cursor == "100:5" {
		t.Errorf("EncodeCursor(100, 5) = %q, want an opaque base64-encoded value", cursor)
	}
}

// TestDecodeCursor_RejectsInvalidInput verifies that DecodeCursor returns errors
// for malformed cursor strings.
func TestDecodeCursor_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "rejects non-base64 garbage",
			input: "!!!not-base64!!!",
		},
		{
			name:  "rejects base64 with no colon separator",
			input: "MTIzNDU", // base64 of "12345"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeCursor(tt.input)
			if err == nil {
				t.Errorf("DecodeCursor(%q) = nil error, want error", tt.input)
			}
		})
	}
}

// TestClampLimit verifies that ClampLimit applies correct defaults and maximums.
func TestClampLimit(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		want      int
	}{
		{
			name:      "defaults to 20 for zero",
			requested: 0,
			want:      DefaultLimit,
		},
		{
			name:      "defaults to 20 for negative",
			requested: -5,
			want:      DefaultLimit,
		},
		{
			name:      "passes through value within range",
			requested: 50,
			want:      50,
		},
		{
			name:      "allows exactly max limit",
			requested: MaxLimit,
			want:      MaxLimit,
		},
		{
			name:      "clamps to 100 for value above max",
			requested: 200,
			want:      MaxLimit,
		},
		{
			name:      "clamps to 100 for max+1",
			requested: MaxLimit + 1,
			want:      MaxLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampLimit(tt.requested)
			if got != tt.want {
				t.Errorf("ClampLimit(%d) = %d, want %d", tt.requested, got, tt.want)
			}
		})
	}
}
