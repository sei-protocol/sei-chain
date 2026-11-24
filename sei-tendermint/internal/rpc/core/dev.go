package core

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// UnsafeFlushMempool removes all transactions from the mempool.
func (env *Environment) UnsafeFlushMempool(ctx context.Context) (*coretypes.ResultUnsafeFlushMempool, error) {
	mp, err := env.requireMempool()
	if err != nil {
		return nil, err
	}
	mp.Flush()
	return &coretypes.ResultUnsafeFlushMempool{}, nil
}
