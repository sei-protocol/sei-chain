package migrations_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate15to16(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	// write old params
	prevParams := types.DefaultParams()
	prevParams.DefaultGasPerOrderDataByte = 0
	dexkeeper.SetParams(ctx, prevParams)

	// migrate to default params
	err := migrations.V15ToV16(ctx, *dexkeeper)
	require.NoError(t, err)
	params := dexkeeper.GetParams(ctx)
	require.Equal(t, params.DefaultGasPerOrderDataByte, uint64(30))
}
