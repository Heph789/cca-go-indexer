package postgres

import "context"

type cursorRepo struct{ q querier }

func (r *cursorRepo) Get(ctx context.Context, chainID int64) (uint64, string, error) {
	panic("not implemented")
}

func (r *cursorRepo) Upsert(ctx context.Context, chainID int64, blockNumber uint64, blockHash string) error {
	panic("not implemented")
}
