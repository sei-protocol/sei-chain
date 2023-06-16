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
