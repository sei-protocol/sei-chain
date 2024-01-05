package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultParams(t *testing.T) {
	require.Equal(t, types.Params{
		BaseDenom:                     types.DefaultBaseDenom,
		PriorityNormalizer:            types.DefaultPriorityNormalizer,
		BaseFeePerGas:                 types.DefaultBaseFeePerGas,
		MinimumFeePerGas:              types.DefaultMinFeePerGas,
		ChainConfig:                   types.DefaultChainConfig(),
		ChainId:                       types.DefaultChainID,
		WhitelistedCodehashesBankSend: types.DefaultWhitelistedCodeHashesBankSend,
	}, types.DefaultParams())

	require.Nil(t, types.DefaultParams().Validate())
}
