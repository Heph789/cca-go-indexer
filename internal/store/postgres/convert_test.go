package postgres

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestAddrToText(t *testing.T) {
	tests := []struct {
		name string
		addr common.Address
		want string
	}{
		{
			name: "zero address",
			addr: common.Address{},
			want: "0x0000000000000000000000000000000000000000",
		},
		{
			name: "typical address",
			addr: common.HexToAddress("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"),
			want: common.HexToAddress("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045").Hex(),
		},
		{
			name: "address from lowercase input",
			addr: common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
			want: common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd").Hex(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addrToText(tt.addr)
			if got != tt.want {
				t.Errorf("addrToText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHashToText(t *testing.T) {
	tests := []struct {
		name string
		hash common.Hash
		want string
	}{
		{
			name: "zero hash",
			hash: common.Hash{},
			want: "0x0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name: "typical hash",
			hash: common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
			want: common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890").Hex(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hashToText(tt.hash)
			if got != tt.want {
				t.Errorf("hashToText() = %q, want %q", got, tt.want)
			}
		})
	}
}
