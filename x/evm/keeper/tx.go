package keeper

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const DefaultTxHashesToRemove = 100

func (k *Keeper) RemoveFirstNTxHashes(ctx sdk.Context, n int) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), types.TxHashesPrefix)
	iter := store.Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	keysToDelete := make([][]byte, 0, n)
	for ; n > 0 && iter.Valid(); iter.Next() {
		keysToDelete = append(keysToDelete, iter.Key())
		n--
	}
	for _, k := range keysToDelete {
		store.Delete(k)
	}
}
