package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) GetNextOrderID(ctx sdk.Context, contractAddr string) uint64 {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.NextOrderIDPrefix(contractAddr))
	byteKey := types.KeyPrefix(types.NextOrderIDKey)
	bz := store.Get(byteKey)
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func (k Keeper) SetNextOrderID(ctx sdk.Context, contractAddr string, nextID uint64) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.NextOrderIDPrefix(contractAddr))
	byteKey := types.KeyPrefix(types.NextOrderIDKey)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, nextID)
	store.Set(byteKey, bz)
}

func (k Keeper) DeleteNextOrderID(ctx sdk.Context, contractAddr string) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.NextOrderIDPrefix(contractAddr))
	byteKey := types.KeyPrefix(types.NextOrderIDKey)
	store.Delete(byteKey)
}
