package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/stretchr/testify/require"
)

type mockEpochHooks struct {
	afterEpochEndCalled    bool
	beforeEpochStartCalled bool
}

func (h *mockEpochHooks) AfterEpochEnd(_ sdk.Context, _ types.Epoch) {
	h.afterEpochEndCalled = true
}

func (h *mockEpochHooks) BeforeEpochStart(_ sdk.Context, _ types.Epoch) {
	h.beforeEpochStartCalled = true
}

func TestKeeperHooks(t *testing.T) {
	k := Keeper{}
	hooks := &mockEpochHooks{}
	k.SetHooks(hooks)

	// Can't set the same hook twice
	require.Panics(t, func() {
		k.SetHooks(hooks)
	})

	ctx := sdk.Context{}   // setup context as required
	epoch := types.Epoch{} // setup epoch as required

	k.AfterEpochEnd(ctx, epoch)
	require.True(t, hooks.afterEpochEndCalled)

	hooks.afterEpochEndCalled = false // reset for the next test

	k.BeforeEpochStart(ctx, epoch)
	require.True(t, hooks.beforeEpochStartCalled)
}
