package store

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func GetCachedContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}
