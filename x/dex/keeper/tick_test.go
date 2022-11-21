package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/assert"
)

func TestPriceTickSizeGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	// TEST_PAIR = atom/usdc pair
	ticksize, found := keeper.GetPriceTickSizeForPair(ctx, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", keepertest.TestPair)
	assert.Equal(t, ticksize, sdk.ZeroDec())
	assert.False(t, found)
	keeper.SetPriceTickSizeForPair(ctx, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", keepertest.TestPair, sdk.NewDec(2))
	ticksize, found = keeper.GetPriceTickSizeForPair(ctx, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", keepertest.TestPair)
	assert.Equal(t, ticksize, sdk.NewDec(2))
	assert.True(t, found)
}

func TestQuantityTickSizeGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	// TEST_PAIR = atom/usdc pair
	ticksize, found := keeper.GetQuantityTickSizeForPair(ctx, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", keepertest.TestPair)
	assert.Equal(t, ticksize, sdk.ZeroDec())
	assert.False(t, found)
	keeper.SetQuantityTickSizeForPair(ctx, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", keepertest.TestPair, sdk.NewDec(2))
	ticksize, found = keeper.GetQuantityTickSizeForPair(ctx, "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m", keepertest.TestPair)
	assert.Equal(t, ticksize, sdk.NewDec(2))
	assert.True(t, found)
}
