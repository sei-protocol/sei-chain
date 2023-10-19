package keeper

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestParams(t *testing.T) {
	k, _, ctx := MockEVMKeeper()
	require.Equal(t, types.DefaultChainConfig(), k.GetChainConfig(ctx))
	require.Equal(t, types.DefaultBaseDenom, k.GetBaseDenom(ctx))
	require.Equal(t, types.DefaultPriorityNormalizer, k.GetPriorityNormalizer(ctx))
	require.Equal(t, types.DefaultBaseFeePerGas, k.GetBaseFeePerGas(ctx))
	require.Equal(t, types.DefaultMinFeePerGas, k.GetMinimumFeePerGas(ctx))
}
