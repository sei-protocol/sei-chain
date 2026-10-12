package keeper

import (
	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	val := k.PrefixStore(ctx, types.StateKey(addr)).Get(hash[:])
	if val == nil {
		if k.EthReplayConfig.Enabled {
			// try to get from eth DB
			tr, err := k.DB.OpenStorageTrie(k.Root, addr, common.BytesToHash(k.PrefixStore(ctx, types.ReplaySeenAddrPrefix).Get(addr[:])), k.Trie)
			if err != nil {
				panic(err)
			}
			val, err := tr.GetStorage(addr, hash.Bytes())
			if err != nil {
				return common.Hash{}
			}
			return common.BytesToHash(val)
		}
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
