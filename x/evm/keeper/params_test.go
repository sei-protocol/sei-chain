package keeper_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestParams(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	require.Equal(t, types.DefaultChainConfig(), k.GetChainConfig(ctx))
	require.Equal(t, types.DefaultBaseDenom, k.GetBaseDenom(ctx))
	require.Equal(t, types.DefaultPriorityNormalizer, k.GetPriorityNormalizer(ctx))
	require.Equal(t, types.DefaultBaseFeePerGas, k.GetBaseFeePerGas(ctx))
	require.Equal(t, types.DefaultMinFeePerGas, k.GetMinimumFeePerGas(ctx))
	require.Equal(t, types.DefaultWhitelistedCodeHashesBankSend, k.WhitelistedCodehashesBankSend(ctx))

	require.Nil(t, k.GetParams(ctx).Validate())
}
