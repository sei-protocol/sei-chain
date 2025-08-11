package utils

import (
	sdk "github.com/sei-protocol/sei-chain/cosmos-sdk/types"
)

func CacheTxContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}
