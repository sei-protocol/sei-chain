package keeper_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetParams(t *testing.T) {
	k, ctx := testkeeper.DexKeeper(t)
	params := types.DefaultParams()

	k.SetParams(ctx, params)

	require.EqualValues(t, params, k.GetParams(ctx))
}

func TestGetSettlementGasAllowance(t *testing.T) {
	k, ctx := testkeeper.DexKeeper(t)
	gasAllowance := k.GetSettlementGasAllowance(ctx, 10)
	require.Equal(t, uint64(10)*types.DefaultGasAllowancePerSettlement, gasAllowance)
}
