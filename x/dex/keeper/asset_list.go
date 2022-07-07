package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k Keeper) SetAssetMetadata(ctx sdk.Context, assetMetadata types.AssetMetadata) {
	store := ctx.KVStore(k.storeKey)
	// Even if asset exists already, overwrite the store with new metadata
	b := k.Cdc.MustMarshal(&assetMetadata)

	store.Set(types.AssetListPrefix(assetMetadata.Metadata.Base), b)
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
