package migrations_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigrateRemoveCurrBlockBaseFee(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})

	// Set a test base fee
	testBaseFee := sdk.NewDec(100)
	k.SetCurrBaseFeePerGas(ctx, testBaseFee)

	// Verify initial state
	require.Equal(t, testBaseFee, k.GetCurrBaseFeePerGas(ctx))
	require.Equal(t, k.GetMinimumFeePerGas(ctx), k.GetNextBaseFeePerGas(ctx))

	// Run the migration
	err := migrations.MigrateRemoveCurrBlockBaseFee(ctx, &k)
	require.NoError(t, err)

	// Verify the migration worked correctly
	require.Equal(t, testBaseFee, k.GetNextBaseFeePerGas(ctx))
	require.Equal(t, k.GetMinimumFeePerGas(ctx), k.GetCurrBaseFeePerGas(ctx))
}
