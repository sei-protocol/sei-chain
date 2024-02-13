package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultParams(t *testing.T) {
	require.Equal(t, types.Params{
		PriorityNormalizer:                     types.DefaultPriorityNormalizer,
		BaseFeePerGas:                          types.DefaultBaseFeePerGas,
		MinimumFeePerGas:                       types.DefaultMinFeePerGas,
		ChainId:                                types.DefaultChainID,
		WhitelistedCwCodeHashesForDelegateCall: types.DefaultWhitelistedCwCodeHashesForDelegateCall,
	}, types.DefaultParams())

	require.Nil(t, types.DefaultParams().Validate())
}
