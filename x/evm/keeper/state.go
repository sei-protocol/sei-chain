package keeper

import (
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	if ctx.TxIndex() == -1 {
            fmt.Printf("getting state for %s, %s\n", addr.Hex(), hash.Hex())
	}
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
				fmt.Println(err)
				return common.Hash{}
			}
			if ctx.TxIndex() == -1 {
				fmt.Printf("got %s from trie\n", common.BytesToHash(val).Hex())
			}
			return common.BytesToHash(val)
		}
		return common.Hash{}
	}
	 if ctx.TxIndex() == -1 {
		  fmt.Printf("got %s from kv\n", common.BytesToHash(val).Hex())
	  }
	return common.BytesToHash(val)
}

func (k *Keeper) SetState(ctx sdk.Context, addr common.Address, key common.Hash, val common.Hash) {
	if ctx.TxIndex() == -1 {
            fmt.Printf("setting state for %s, %s, %s\n", addr.Hex(), key.Hex(), val.Hex())
        }
	k.PrefixStore(ctx, types.StateKey(addr)).Set(key[:], val[:])
}
