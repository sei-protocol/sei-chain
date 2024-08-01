package migrations_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate14to15(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	// write old params
	prevParams := types.DefaultParams()
	prevParams.MaxOrderPerPrice = 0
	prevParams.MaxPairsPerContract = 0
	dexkeeper.SetParams(ctx, prevParams)

	// migrate to default params
	err := migrations.V14ToV15(ctx, *dexkeeper)
	require.NoError(t, err)
	params := dexkeeper.GetParams(ctx)
	require.Equal(t, params.MaxOrderPerPrice, uint64(types.DefaultMaxOrderPerPrice))
	require.Equal(t, params.MaxPairsPerContract, uint64(types.DefaultMaxPairsPerContract))
}
