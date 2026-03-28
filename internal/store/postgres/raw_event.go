package postgres

import (
	"context"

	"github.com/cca/go-indexer/internal/domain/cca"
)

type rawEventRepo struct{ q querier }

func (r *rawEventRepo) Insert(ctx context.Context, event *cca.RawEvent) error {
	panic("not implemented")
}

func (r *rawEventRepo) DeleteFromBlock(ctx context.Context, chainID int64, fromBlock uint64) error {
	panic("not implemented")
}
