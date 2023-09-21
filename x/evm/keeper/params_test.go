package keeper

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestParams(t *testing.T) {
	k, ctx := MockEVMKeeper()
	require.Equal(t, types.DefaultChainConfig(), k.GetChainConfig(ctx))
	require.Equal(t, types.DefaultGasMultiplier, k.GetGasMultiplier(ctx))
}
