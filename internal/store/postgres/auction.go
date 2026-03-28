package postgres

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

type auctionRepo struct{ q querier }

func (r *auctionRepo) Insert(ctx context.Context, auction *cca.Auction) error {
	panic("not implemented")
}

func (r *auctionRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	panic("not implemented")
}

func (r *auctionRepo) GetByAddress(ctx context.Context, chainID int64, auctionAddress string) (*cca.Auction, error) {
	panic("not implemented")
}
