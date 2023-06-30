package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/goutils"
)

func (k Keeper) removeAllForPrefix(ctx sdk.Context, prefix []byte) {
	store := ctx.KVStore(k.storeKey)
	for _, key := range k.getAllKeysForPrefix(store, prefix) {
		store.Delete(key)
	}
}

func (k Keeper) getAllKeysForPrefix(store sdk.KVStore, prefix []byte) [][]byte {
	keys := [][]byte{}
	iter := sdk.KVStorePrefixIterator(store, prefix)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		goutils.InPlaceAppend(&keys, iter.Key())
	}
	return keys
}
