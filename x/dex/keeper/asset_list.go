package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetAssetMetadata(ctx sdk.Context, assetMetadata types.AssetMetadata) {
	store := ctx.KVStore(k.storeKey)
	// Have one base denom for a canonical “display”.
	// Even if asset exists already, overwrite the store with new metadata
	// Asset list is decided through governance
	b := k.Cdc.MustMarshal(&assetMetadata)

	store.Set(types.AssetListPrefix(assetMetadata.Metadata.Display), b)
}

func (k Keeper) GetAssetMetadataByDenom(ctx sdk.Context, assetDenom string) (val types.AssetMetadata, found bool) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.AssetListPrefix(assetDenom))
	if b == nil {
		return types.AssetMetadata{}, false
	}
	metadata := types.AssetMetadata{}
	k.Cdc.MustUnmarshal(b, &metadata)
	return metadata, true
}

func (k Keeper) GetAllAssetMetadata(ctx sdk.Context) []types.AssetMetadata {
	store := ctx.KVStore(k.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, types.KeyPrefix(types.AssetListKey))

	list := []types.AssetMetadata{}
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.AssetMetadata
		k.Cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return list
}
