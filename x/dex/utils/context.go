package utils

import (
	"context"

	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
)

const DexMemStateContextKey = "dex-memstate"

func GetMemState(ctx context.Context) *dexcache.MemState {
	if val := ctx.Value(DexMemStateContextKey); val != nil {
		return val.(*dexcache.MemState)
	}
	panic("cannot find mem state in context")
}
