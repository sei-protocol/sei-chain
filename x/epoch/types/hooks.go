package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type EpochHooks interface {
	// the first block whose timestamp is after the duration is counted as the end of the epoch
	AfterEpochEnd(ctx sdk.Context, epoch Epoch)
	// new epoch is next block of epoch end block
	BeforeEpochStart(ctx sdk.Context, epoch Epoch)
}

var _ EpochHooks = MultiEpochHooks{}

type MultiEpochHooks []EpochHooks

func NewMultiEpochHooks(hooks ...EpochHooks) MultiEpochHooks {
	return hooks
}

// AfterEpochEnd is called when epoch is going to be ended, epochNumber is the number of epoch that is ending.
func (h MultiEpochHooks) AfterEpochEnd(ctx sdk.Context, epoch Epoch) {
	for i := range h {
		h[i].AfterEpochEnd(ctx, epoch)
	}
}

// BeforeEpochStart is called when epoch is going to be started, epochNumber is the number of epoch that is starting.
func (h MultiEpochHooks) BeforeEpochStart(ctx sdk.Context, epoch Epoch) {
	for i := range h {
		h[i].BeforeEpochStart(ctx, epoch)
	}
}
