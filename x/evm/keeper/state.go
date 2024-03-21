package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	val := k.PrefixStore(ctx, types.StateKey(addr)).Get(hash[:])
	if val == nil {
		if !k.EthReplayConfig.Enabled && !k.EthBlockTestConfig.Enabled {
			return common.Hash{}
		}
		var tr state.Trie
		var err error
		if k.EthBlockTestConfig.Enabled {
			// try to get from blocktest DB
			tr, err = k.DB.OpenStorageTrie(k.Root, addr, common.BytesToHash(k.PrefixStore(ctx, types.BlockTestSeenAddrPrefix).Get(addr[:])), k.Trie)
			if err != nil {
				panic(err)
			}
		}
		if k.EthReplayConfig.Enabled {
			// try to get from eth DB
			tr, err = k.DB.OpenStorageTrie(k.Root, addr, common.BytesToHash(k.PrefixStore(ctx, types.ReplaySeenAddrPrefix).Get(addr[:])), k.Trie)
			if err != nil {
				panic(err)
			}
		}
		val, err := tr.GetStorage(addr, hash.Bytes())
		if err != nil {
			return common.Hash{}
		}
		return common.BytesToHash(val)
	}
	return common.BytesToHash(val)
}

func (k *Keeper) SetState(ctx sdk.Context, addr common.Address, key common.Hash, val common.Hash) {
	k.PrefixStore(ctx, types.StateKey(addr)).Set(key[:], val[:])
}
