package keeper

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetPairCount(ctx sdk.Context, contractAddr string, count uint64) {
	store := prefix.NewStore(
		ctx.KVStore(k.StoreKey),
		types.RegisteredPairCountPrefix(),
	)
	countBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(countBytes, count)
	store.Set(types.KeyPrefix(contractAddr), countBytes)
}

func (k Keeper) GetPairCount(ctx sdk.Context, contractAddr string) uint64 {
	store := prefix.NewStore(
		ctx.KVStore(k.StoreKey),
		types.RegisteredPairCountPrefix(),
	)
	cnt := store.Get(types.KeyPrefix(contractAddr))
	if cnt == nil {
		return 0
	}
	return binary.BigEndian.Uint64(cnt)
}

func (k Keeper) AddRegisteredPair(ctx sdk.Context, contractAddr string, pair types.Pair) bool {
	// Only add pairs that have not been added before
	for _, prevPair := range k.GetAllRegisteredPairs(ctx, contractAddr) {
		if pair.PriceDenom == prevPair.PriceDenom && pair.AssetDenom == prevPair.AssetDenom {
			return false
		}
	}
	oldPairCnt := k.GetPairCount(ctx, contractAddr)
	store := prefix.NewStore(ctx.KVStore(k.StoreKey), types.RegisteredPairPrefix(contractAddr))
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, oldPairCnt)
	store.Set(keyBytes, k.Cdc.MustMarshal(&pair))
	k.SetPairCount(ctx, contractAddr, oldPairCnt+1)

	return true
}

func (k Keeper) GetAllRegisteredPairs(ctx sdk.Context, contractAddr string) []types.Pair {
	store := prefix.NewStore(ctx.KVStore(k.StoreKey), types.RegisteredPairPrefix(contractAddr))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	list := []types.Pair{}
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Pair
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return list
}
