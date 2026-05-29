package core

import (
	"context"
	"errors"

	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// UnsafeFlushMempool removes all transactions from the mempool.
func (env *Environment) UnsafeFlushMempool(ctx context.Context) (*coretypes.ResultUnsafeFlushMempool, error) {
	if _, ok := env.gigaRouter().Get(); ok {
		return nil, errors.New("unsafe_flush_mempool is not supported with autobahn mempool")
	}
	mp, err := env.requireMempool()
	if err != nil {
		return nil, err
	}
	mp.Flush()
	return &coretypes.ResultUnsafeFlushMempool{}, nil
}
