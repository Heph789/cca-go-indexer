package postgres

import "context"

type blockRepo struct{ q querier }

func (r *blockRepo) Insert(ctx context.Context, chainID int64, blockNumber uint64, blockHash, parentHash string) error {
	panic("not implemented")
}

func (r *blockRepo) GetHash(ctx context.Context, chainID int64, blockNumber uint64) (string, error) {
	panic("not implemented")
}

func (r *blockRepo) DeleteFrom(ctx context.Context, chainID int64, fromBlock uint64) error {
	panic("not implemented")
}
