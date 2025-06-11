package migrations_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigrateBaseFeeOffByOne(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)
	bf := sdk.NewDec(100)
	k.SetNextBaseFeePerGas(ctx, bf)
	require.Equal(t, k.GetMinimumFeePerGas(ctx), k.GetNextBaseFeePerGas(ctx))
	// do the migration
	require.Nil(t, migrations.MigrateBaseFeeOffByOne(ctx, &k))
	require.Equal(t, bf, k.GetNextBaseFeePerGas(ctx))
	require.Equal(t, bf, k.GetNextBaseFeePerGas(ctx))
}
