package keeper_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestParamsDefault(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	require.Equal(t, types.DefaultChainConfig(), k.GetChainConfig(ctx))
	require.Equal(t, types.DefaultBaseDenom, k.GetBaseDenom(ctx))
	require.Equal(t, types.DefaultPriorityNormalizer, k.GetPriorityNormalizer(ctx))
	require.Equal(t, types.DefaultBaseFeePerGas, k.GetBaseFeePerGas(ctx))
	require.Equal(t, types.DefaultMinFeePerGas, k.GetMinimumFeePerGas(ctx))
	require.Equal(t, types.DefaultWhitelistedCodeHashesBankSend, k.WhitelistedCodehashesBankSend(ctx))

	require.Nil(t, k.GetParams(ctx).Validate())
}

func TestParamsWithOverride(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	overrides := types.Params{
		BaseDenom:          "test",
		PriorityNormalizer: sdk.NewDec(1),
		BaseFeePerGas:      sdk.NewDec(2),
		MinimumFeePerGas:   sdk.NewDec(3),
		ChainConfig: types.ChainConfig{
			CancunTime: 1,
			PragueTime: 2,
			VerkleTime: 3,
		},
		ChainId:                       sdk.NewInt(4),
		WhitelistedCodehashesBankSend: []string{"test"},
	}
	k.SetParams(ctx, overrides)
	require.Equal(t, overrides.ChainConfig, k.GetChainConfig(ctx))
	require.Equal(t, overrides.BaseDenom, k.GetBaseDenom(ctx))
	require.Equal(t, overrides.PriorityNormalizer, k.GetPriorityNormalizer(ctx))
	require.Equal(t, overrides.BaseFeePerGas, k.GetBaseFeePerGas(ctx))
	require.Equal(t, overrides.MinimumFeePerGas, k.GetMinimumFeePerGas(ctx))
	require.Equal(t, overrides.WhitelistedCodehashesBankSend, k.WhitelistedCodehashesBankSend(ctx))

	require.Nil(t, k.GetParams(ctx).Validate())
}
