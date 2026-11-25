package keeper_test

import (
	"testing"

	pk "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"

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

// TestMigrate2to3 migrates a params store that does not hold the disabled message type URLs.
func TestMigrate2to3(t *testing.T) {
	_, ctx, _, _, keeper := testComponents()
	subspace, ok := keeper.GetSubspace(types.ModuleName)
	require.True(t, ok)
	require.False(t, subspace.Has(ctx, types.ParamStoreKeyDisabledMsgTypeURLs))

	require.NoError(t, pk.NewMigrator(keeper).Migrate2to3(ctx))

	require.True(t, subspace.Has(ctx, types.ParamStoreKeyDisabledMsgTypeURLs))
	require.Empty(t, keeper.GetDisabledMsgTypeURLs(ctx))
}
