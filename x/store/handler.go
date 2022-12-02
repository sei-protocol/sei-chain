package store

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func GetCachedContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}
