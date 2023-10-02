package state

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// Initialized for each transaction individually
type StateDBImpl struct {
	ctx             sdk.Context
	snapshottedCtxs []sdk.Context
	// If err is not nil at the end of the execution, the transaction will be rolled
	// back.
	err error

	k *keeper.Keeper
}

func NewStateDBImpl(ctx sdk.Context, k *keeper.Keeper) *StateDBImpl {
	s := &StateDBImpl{
		ctx:             ctx,
		k:               k,
		snapshottedCtxs: []sdk.Context{},
	}
	s.Snapshot() // take an initial snapshot for GetCommitted
	return s
}

func (s *StateDBImpl) Finalize() error {
	if s.err != nil {
		return s.err
	}
	if err := s.CheckBalance(); err != nil {
		return err
	}
	// remove transient states
	s.k.PurgePrefix(s.ctx, types.TransientStateKeyPrefix)
	s.k.PurgePrefix(s.ctx, types.AccountTransientStateKeyPrefix)
	s.k.PurgePrefix(s.ctx, types.TransientModuleStateKeyPrefix)

	// write cache to underlying
	s.ctx.MultiStore().(sdk.CacheMultiStore).Write()
	// write all snapshotted caches in reverse order, except the very first one
	for i := len(s.snapshottedCtxs) - 1; i > 0; i-- {
		s.snapshottedCtxs[i].MultiStore().(sdk.CacheMultiStore).Write()
	}

	return nil
}
