package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/epoch/keeper"
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
	k := keeper.Keeper{}
	hooks := &mockEpochHooks{}
	k.SetHooks(hooks)

	ctx := sdk.Context{}   // setup context as required
	epoch := types.Epoch{} // setup epoch as required

	k.AfterEpochEnd(ctx, epoch)
	require.True(t, hooks.afterEpochEndCalled)

	hooks.afterEpochEndCalled = false // reset for the next test

	k.BeforeEpochStart(ctx, epoch)
	require.True(t, hooks.beforeEpochStartCalled)
}

func TestMultiHooks(t *testing.T) {
	hooks := &mockEpochHooks{}
	multiHooks := types.MultiEpochHooks{
		hooks,
	}

	ctx := sdk.Context{}   // setup context as required
	epoch := types.Epoch{} // setup epoch as required

	multiHooks.AfterEpochEnd(ctx, epoch)
	require.True(t, hooks.afterEpochEndCalled)

	hooks.afterEpochEndCalled = false // reset for the next test

	multiHooks.BeforeEpochStart(ctx, epoch)
	require.True(t, hooks.beforeEpochStartCalled)
}
