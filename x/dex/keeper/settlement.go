package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetSettlements(ctx sdk.Context, contractAddr string, priceDenom string, assetDenom string, settlements types.Settlements) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, uint64(ctx.BlockHeight())))
	b := k.Cdc.MustMarshal(&settlements)
	store.Set(types.PairPrefix(priceDenom, assetDenom), b)
}

func (k Keeper) GetSettlements(ctx sdk.Context, contractAddr string, blockHeight uint64, priceDenom string, assetDenom string) (val types.Settlements, found bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, blockHeight))
	b := store.Get(types.PairPrefix(priceDenom, assetDenom))
	val = types.Settlements{}
	if b == nil {
		return val, false
	}
	k.Cdc.MustUnmarshal(b, &val)
	return val, true
}

func (k Keeper) GetAllSettlements(ctx sdk.Context, contractAddr string, blockHeight uint64) (list []types.SettlementEntry) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.SettlementEntryPrefix(contractAddr, uint64(ctx.BlockHeight())))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.SettlementEntry
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}
