package types_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
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

func TestValidateParamsInvalidPriorityNormalizer(t *testing.T) {
	params := types.DefaultParams()
	params.PriorityNormalizer = sdk.NewDec(-1) // Set to invalid negative value

	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonpositive priority normalizer")
}

func TestValidateParamsNegativeBaseFeePerGas(t *testing.T) {
	params := types.DefaultParams()
	params.BaseFeePerGas = sdk.NewDec(-1) // Set to invalid negative value

	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "negative base fee per gas")
}

func TestValidateParamsNegativeChainID(t *testing.T) {
	params := types.DefaultParams()
	params.ChainId = sdk.NewInt(-1) // Set to invalid negative value

	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "negative chain ID")
}

func TestBaseFeeMinimumFee(t *testing.T) {
	params := types.DefaultParams()
	params.MinimumFeePerGas = sdk.NewDec(1)
	params.BaseFeePerGas = params.MinimumFeePerGas.Add(sdk.NewDec(1))
	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "minimum fee cannot be lower than base fee")
}
