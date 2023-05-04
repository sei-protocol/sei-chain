package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceTickSizeGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	// TEST_PAIR = atom/usdc pair
	contractAddr := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	ticksize, found := keeper.GetPriceTickSizeForPair(ctx, contractAddr, keepertest.TestPair)
	assert.Equal(t, ticksize, sdk.ZeroDec())
	assert.False(t, found)

	keeper.AddRegisteredPair(ctx, contractAddr, keepertest.TestPair)
	err := keeper.SetPriceTickSizeForPair(ctx, contractAddr, keepertest.TestPair, sdk.NewDec(2))
	require.NoError(t, err)
	ticksize, found = keeper.GetPriceTickSizeForPair(ctx, contractAddr, keepertest.TestPair)
	assert.Equal(t, ticksize, sdk.NewDec(2))
	assert.True(t, found)
}

func TestQuantityTickSizeGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	// TEST_PAIR = atom/usdc pair
	contractAddr := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	ticksize, found := keeper.GetQuantityTickSizeForPair(ctx, contractAddr, keepertest.TestPair)
	assert.Equal(t, ticksize, sdk.ZeroDec())
	assert.False(t, found)

	keeper.AddRegisteredPair(ctx, contractAddr, keepertest.TestPair)
	err := keeper.SetQuantityTickSizeForPair(ctx, contractAddr, keepertest.TestPair, sdk.NewDec(2))
	require.NoError(t, err)
	ticksize, found = keeper.GetQuantityTickSizeForPair(ctx, contractAddr, keepertest.TestPair)
	assert.Equal(t, ticksize, sdk.NewDec(2))
	assert.True(t, found)
}
