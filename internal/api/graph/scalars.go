package graph

import (
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/ethereum/go-ethereum/common"
)

// Address is a type alias for common.Address used as a gqlgen scalar model.
type Address = common.Address

// BigInt is a named type for *big.Int used as a gqlgen scalar model.
// This avoids a naming conflict with the built-in Int scalar.
// Domain types use *big.Int directly; gqlgen handles the pointer wrapping.
type BigInt = big.Int

// MarshalAddress serializes common.Address as a lowercase hex string.
func MarshalAddress(addr common.Address) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		fmt.Fprintf(w, `"%s"`, strings.ToLower(addr.Hex()))
	})
}

// UnmarshalAddress parses a hex string into common.Address.
// Accepts addresses with or without the 0x prefix.
func UnmarshalAddress(v interface{}) (common.Address, error) {
	s, ok := v.(string)
	if !ok {
		return common.Address{}, fmt.Errorf("address must be a string")
	}
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("invalid address: %s", s)
	}
	return common.HexToAddress(s), nil
}

// MarshalBigInt serializes *big.Int as a quoted decimal string.
// Returns graphql.Null for nil values.
func MarshalBigInt(i *big.Int) graphql.Marshaler {
	if i == nil {
		return graphql.Null
	}
	return graphql.WriterFunc(func(w io.Writer) {
		fmt.Fprintf(w, `"%s"`, i.String())
	})
}

// UnmarshalBigInt parses a decimal string into *big.Int.
func UnmarshalBigInt(v interface{}) (*big.Int, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("big int must be a string")
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid big int: %s", s)
	}
	return n, nil
}
