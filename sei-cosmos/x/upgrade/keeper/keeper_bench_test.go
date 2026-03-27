package keeper_test

import (
	"fmt"
	"testing"
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

// BenchmarkGetDoneHeightCached measures GetDoneHeight with a warm in-memory cache
// (current behavior after the fix). Every iteration is a sync.Map lookup.
func BenchmarkGetDoneHeightCached(b *testing.B) {
	app := seiapp.Setup(b, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Height: 100, Time: time.Now()})
	app.UpgradeKeeper.SetDone(ctx, "test-upgrade")
	// Warm the cache with one read before measuring.
	app.UpgradeKeeper.GetDoneHeight(ctx, "test-upgrade")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.UpgradeKeeper.GetDoneHeight(ctx, "test-upgrade")
	}
}

// BenchmarkGetDoneHeightUncached measures GetDoneHeight with a cold cache,
// simulating the pre-fix behavior where every call hits the KV store.
// A unique upgrade name is used per iteration so the cache is always cold,
// but the value exists in the KV store — reproducing the pre-fix hot path.
func BenchmarkGetDoneHeightUncached(b *testing.B) {
	app := seiapp.Setup(b, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Height: 100, Time: time.Now()})

	// Pre-populate N unique upgrade names so each iteration is a guaranteed
	// cache miss on a fresh keeper (empty cache, value present in KV store).
	names := make([]string, b.N)
	for i := range names {
		names[i] = fmt.Sprintf("test-upgrade-%d", i)
		app.UpgradeKeeper.SetDone(ctx, names[i])
	}

	// Single fresh keeper: cache is empty, all names are in the KV store.
	k := keeper.NewKeeper(
		make(map[int64]bool),
		app.GetKey(types.StoreKey),
		app.AppCodec(),
		b.TempDir(),
		app.BaseApp,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k.GetDoneHeight(ctx, names[i])
	}
}
