package cca

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type Auction struct {
	AuctionAddress         common.Address
	Token                  common.Address
	Amount                 *big.Int
	Currency               common.Address
	TokensRecipient        common.Address
	FundsRecipient         common.Address
	StartBlock             uint64
	EndBlock               uint64
	ClaimBlock             uint64
	TickSpacing            *big.Int
	ValidationHook         common.Address
	FloorPrice             *big.Int
	RequiredCurrencyRaised *big.Int
	ChainID                int64
	BlockNumber            uint64
	TxHash                 common.Hash
	LogIndex               uint
	CreatedAt              time.Time
	UpdatedAt              time.Time
}
