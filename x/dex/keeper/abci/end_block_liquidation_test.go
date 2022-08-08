package abci_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/stretchr/testify/require"
)

func TestPlaceLiquidationOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	liquidationOrder := types.Order{
		PriceDenom: keepertest.TestPair.PriceDenom,
		AssetDenom: keepertest.TestPair.AssetDenom,
	}
	wrapper := abci.KeeperWrapper{Keeper: keeper}
	wrapper.PlaceLiquidationOrders(ctx, keepertest.TestContract, []types.Order{liquidationOrder})
	require.Equal(t, 1, len(keeper.MemState.GetBlockOrders(ctx, typesutils.ContractAddress(keepertest.TestContract), typesutils.GetPairString(&keepertest.TestPair)).Get()))
}
