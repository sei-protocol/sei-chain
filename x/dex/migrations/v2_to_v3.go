package migrations

import (
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

/**
 * No `dex` state exists in any public chain at the time this data type update happened.
 * Any new chain (including local ones) should be based on a Sei version newer than this update
 * and therefore doesn't need this migration
 */
func DataTypeUpdate(ctx sdk.Context, storeKey sdk.StoreKey, _ codec.BinaryCodec) error {
	ClearStore(ctx, storeKey)
	return nil
}

/**
 * CAUTION: this function clears up the entire `dex` module store, so it should only ever
 *          be used outside of a production setting.
 */
func ClearStore(ctx sdk.Context, storeKey sdk.StoreKey) {
	for i := 0; i < 256; i++ {
		clearStoreForByte(ctx, storeKey, byte(i))
	}
}

func clearStoreForByte(ctx sdk.Context, storeKey sdk.StoreKey, b byte) {
	store := ctx.KVStore(storeKey)
	iterator := sdk.KVStorePrefixIterator(store, []byte{b})
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		store.Delete(iterator.Key())
	}
}
