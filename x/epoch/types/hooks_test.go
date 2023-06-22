package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/epoch/keeper"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

type mockEpochHooks struct {
	afterEpochEndCalled    bool
	beforeEpochStartCalled bool
	shouldPanic            bool
}

func (h *mockEpochHooks) AfterEpochEnd(_ sdk.Context, _ types.Epoch) {
	if h.shouldPanic {
		panic("AfterEpochEnd")
	}

	h.afterEpochEndCalled = true
}

func (h *mockEpochHooks) BeforeEpochStart(_ sdk.Context, _ types.Epoch) {
	if h.shouldPanic {
		panic("BeforeEpochStart")
	}

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

	db := tmdb.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ctx := sdk.NewContext(ms, tmproto.Header{}, false, nil)
	epoch := types.Epoch{}

	multiHooks.AfterEpochEnd(ctx, epoch)
	require.True(t, hooks.afterEpochEndCalled)

	hooks.afterEpochEndCalled = false // reset for the next test

	multiHooks.BeforeEpochStart(ctx, epoch)
	require.True(t, hooks.beforeEpochStartCalled)
}

func TestMultiHooks_Panic(t *testing.T) {
	hook1 := &mockEpochHooks{shouldPanic: false}
	hook2 := &mockEpochHooks{shouldPanic: true}
	hook3 := &mockEpochHooks{shouldPanic: false}
	multiHooks := types.MultiEpochHooks{
		hook1,
		hook2,
		hook3,
	}

	db := tmdb.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ctx := sdk.NewContext(ms, tmproto.Header{}, false, nil)
	epoch := types.Epoch{}

	multiHooks.AfterEpochEnd(ctx, epoch)
	require.True(t, hook1.afterEpochEndCalled)
	require.False(t, hook2.afterEpochEndCalled) // second hook should panic
	require.True(t, hook3.afterEpochEndCalled)  // third hook should still run after 2nd
}
