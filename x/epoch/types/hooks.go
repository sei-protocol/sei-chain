package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
)

type EpochHooks interface {
	// AfterEpochEnd defines the first block whose timestamp is after the duration
	// is counted as the end of the epoch.
	AfterEpochEnd(ctx sdk.Context, epoch Epoch)
	// BeforeEpochStart defines the new epoch is next block of epoch EndBlock.
	BeforeEpochStart(ctx sdk.Context, epoch Epoch)
}

var _ EpochHooks = MultiEpochHooks{}

type MultiEpochHooks []EpochHooks

func NewMultiEpochHooks(hooks ...EpochHooks) MultiEpochHooks {
	return hooks
}

// AfterEpochEnd is called when epoch is going to be ended, epochNumber is the
// number of epoch that is ending.
func (h MultiEpochHooks) AfterEpochEnd(ctx sdk.Context, epoch Epoch) {
	for i := range h {
		panicCatchingEpochHook(ctx, h[i].AfterEpochEnd, epoch)
	}
}

// BeforeEpochStart is called when epoch is going to be started, epochNumber is
// the number of epoch that is starting.
func (h MultiEpochHooks) BeforeEpochStart(ctx sdk.Context, epoch Epoch) {
	for i := range h {
		panicCatchingEpochHook(ctx, h[i].BeforeEpochStart, epoch)
	}
}

func panicCatchingEpochHook(ctx sdk.Context, hookFn func(sdk.Context, Epoch), epoch Epoch) {
	defer utils.PanicHandler(func(r any) {
		utils.LogPanicCallback(ctx, r)
	})()

	// cache the context and only write if no panic (which is caught above)
	cacheCtx, write := ctx.CacheContext()
	hookFn(cacheCtx, epoch)
	write()
}
