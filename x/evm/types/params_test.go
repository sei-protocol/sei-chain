package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultParams(t *testing.T) {
	require.Equal(t, types.Params{
		PriorityNormalizer:                     types.DefaultPriorityNormalizer,
		BaseFeePerGas:                          types.DefaultBaseFeePerGas,
		MinimumFeePerGas:                       types.DefaultMinFeePerGas,
		DeliverTxHookWasmGasLimit:              types.DefaultDeliverTxHookWasmGasLimit,
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

func TestBaseFeeMinimumFee(t *testing.T) {
	params := types.DefaultParams()
	params.MinimumFeePerGas = sdk.NewDec(1)
	params.BaseFeePerGas = params.MinimumFeePerGas.Add(sdk.NewDec(1))
	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "minimum fee cannot be lower than base fee")
}

func TestValidateParamsInvalidDeliverTxHookWasmGasLimit(t *testing.T) {
	params := types.DefaultParams()
	params.DeliverTxHookWasmGasLimit = 0 // Set to invalid value (0)

	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid deliver_tx_hook_wasm_gas_limit: must be greater than 0")
}

func TestValidateParamsValidDeliverTxHookWasmGasLimit(t *testing.T) {
	params := types.DefaultParams()

	require.Equal(t, params.DeliverTxHookWasmGasLimit, types.DefaultDeliverTxHookWasmGasLimit)

	params.DeliverTxHookWasmGasLimit = 100000 // Set to valid value

	err := params.Validate()
	require.NoError(t, err)
}
