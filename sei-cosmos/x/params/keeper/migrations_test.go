package keeper_test

import (
	"testing"

	pk "github.com/cosmos/cosmos-sdk/x/params/keeper"
	"github.com/cosmos/cosmos-sdk/x/params/types"

	"github.com/stretchr/testify/require"
)

func TestMigrate1to2(t *testing.T) {
	_, ctx, _, _, keeper := testComponents()
	m := pk.NewMigrator(keeper)
	err := m.Migrate1to2(ctx)
	require.Nil(t, err)
	cosmosGasParams := keeper.GetCosmosGasParams(ctx)
	// ensure set to defaults
	defaultParams := types.DefaultGenesis()
	require.Equal(t, defaultParams.CosmosGasParams.CosmosGasMultiplierNumerator, cosmosGasParams.CosmosGasMultiplierNumerator)
	require.Equal(t, defaultParams.CosmosGasParams.CosmosGasMultiplierDenominator, cosmosGasParams.CosmosGasMultiplierDenominator)
}
