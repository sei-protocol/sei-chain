package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/assert"
)

func TestTickSizeGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	// TEST_PAIR = atom/usdc pair
	ticksize, found := keeper.GetTickSizeForPair(ctx, TEST_PAIR)
	assert.Equal(t, ticksize, float32(-1))
	assert.False(t, found)
	keeper.SetTickSizeForPair(ctx, TEST_PAIR, 2)
	ticksize, found = keeper.GetTickSizeForPair(ctx, TEST_PAIR)
	assert.Equal(t, ticksize, float32(2))
	assert.True(t, found)
}
