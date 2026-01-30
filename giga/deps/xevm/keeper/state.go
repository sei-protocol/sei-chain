package keeper

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/deps/store"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func (k *Keeper) GetState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	val := k.PrefixStore(ctx, types.StateKey(addr)).Get(hash[:])
	if val == nil {
		return common.Hash{}
	}
	return common.BytesToHash(val)
}

func (k *Keeper) GetCommittedState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	store := k.GetKVStore(ctx).(*store.Store)
	key := append(types.StateKey(addr), hash[:]...)
	val := store.GetCommitted(key)
	if val == nil {
		return common.Hash{}
	}
	return common.BytesToHash(val)
}

func (k *Keeper) SetState(ctx sdk.Context, addr common.Address, key common.Hash, val common.Hash) {
	store := k.PrefixStore(ctx, types.StateKey(addr))
	if val == (common.Hash{}) {
		store.Delete(key[:])
		return
	}
	store.Set(key[:], val[:])
}

func (k *Keeper) IterateState(ctx sdk.Context, cb func(addr common.Address, key common.Hash, val common.Hash) bool) {
	iter := prefix.NewStore(k.GetKVStore(ctx), types.StateKeyPrefix).Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		k := iter.Key()
		evmAddr := common.BytesToAddress(k[:common.AddressLength])
		if cb(evmAddr, common.BytesToHash(k[common.AddressLength:]), common.BytesToHash(iter.Value())) {
			break
		}
	}
}
