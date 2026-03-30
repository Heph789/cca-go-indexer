package graph

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestMarshalAddress verifies that MarshalAddress serializes common.Address values
// as lowercase, 0x-prefixed hex strings.
func TestMarshalAddress(t *testing.T) {
	tests := []struct {
		name string
		addr common.Address
		want string
	}{
		{
			name: "marshals address as lowercase hex string",
			addr: common.HexToAddress("0xABCDEF1234567890ABCDEF1234567890ABCDEF12"),
			want: `"0xabcdef1234567890abcdef1234567890abcdef12"`,
		},
		{
			name: "marshals zero address",
			addr: common.Address{},
			want: `"0x0000000000000000000000000000000000000000"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			MarshalAddress(tt.addr).MarshalGQL(&buf)
			got := buf.String()
			if got != tt.want {
				t.Errorf("MarshalAddress(%s) = %s, want %s", tt.addr.Hex(), got, tt.want)
			}
		})
	}
}

// TestUnmarshalAddress verifies that UnmarshalAddress correctly parses hex strings
// into common.Address and rejects invalid input.
func TestUnmarshalAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    common.Address
		wantErr bool
	}{
		{
			name:  "parses 0x-prefixed address",
			input: "0x1234567890abcdef1234567890abcdef12345678",
			want:  common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		},
		{
			name:    "rejects non-string input",
			input:   12345,
			wantErr: true,
		},
		{
			name:    "rejects invalid hex string",
			input:   "not-an-address",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("UnmarshalAddress(%v) = nil error, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalAddress(%v) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("UnmarshalAddress(%v) = %s, want %s", tt.input, got.Hex(), tt.want.Hex())
			}
		})
	}
}

// TestMarshalBigInt verifies that MarshalBigInt serializes *big.Int values
// as quoted decimal strings and handles nil as graphql.Null.
func TestMarshalBigInt(t *testing.T) {
	tests := []struct {
		name  string
		input *big.Int
		want  string
	}{
		{
			name:  "marshals positive big int as decimal string",
			input: big.NewInt(1234567890),
			want:  `"1234567890"`,
		},
		{
			name:  "marshals zero",
			input: big.NewInt(0),
			want:  `"0"`,
		},
		{
			name:  "marshals nil as null",
			input: nil,
			want:  "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			MarshalBigInt(tt.input).MarshalGQL(&buf)
			got := buf.String()
			if got != tt.want {
				t.Errorf("MarshalBigInt(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestUnmarshalBigInt verifies that UnmarshalBigInt parses decimal strings into
// *big.Int and rejects non-string or non-numeric input.
func TestUnmarshalBigInt(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    *big.Int
		wantErr bool
	}{
		{
			name:  "parses decimal string",
			input: "99999999999999999999",
			want:  func() *big.Int { n, _ := new(big.Int).SetString("99999999999999999999", 10); return n }(),
		},
		{
			name:  "parses zero string",
			input: "0",
			want:  big.NewInt(0),
		},
		{
			name:    "rejects non-string input",
			input:   42,
			wantErr: true,
		},
		{
			name:    "rejects non-numeric string",
			input:   "abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalBigInt(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("UnmarshalBigInt(%v) = nil error, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalBigInt(%v) error = %v", tt.input, err)
			}
			if got.Cmp(tt.want) != 0 {
				t.Errorf("UnmarshalBigInt(%v) = %s, want %s", tt.input, got.String(), tt.want.String())
			}
		})
	}
}
