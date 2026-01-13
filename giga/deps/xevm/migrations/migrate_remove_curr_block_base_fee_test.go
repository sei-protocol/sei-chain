package migrations_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigrateRemoveCurrBlockBaseFee(t *testing.T) {
	k := testkeeper.EVMTestApp.GigaEvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})

	// Set a test base fee
	testBaseFee := sdk.NewDec(100)
	testNextBaseFee := sdk.NewDec(101)
	k.SetCurrBaseFeePerGas(ctx, testBaseFee)
	k.SetNextBaseFeePerGas(ctx, testNextBaseFee)

	// Verify initial state
	require.Equal(t, testBaseFee, k.GetCurrBaseFeePerGas(ctx))
	require.Equal(t, testNextBaseFee, k.GetNextBaseFeePerGas(ctx))

	// Run the migration
	err := migrations.MigrateRemoveCurrBlockBaseFee(ctx, &k)
	require.NoError(t, err)

	// Verify the migration worked correctly
	require.Equal(t, testBaseFee, k.GetNextBaseFeePerGas(ctx))
	require.Equal(t, k.GetMinimumFeePerGas(ctx), k.GetCurrBaseFeePerGas(ctx))
}
