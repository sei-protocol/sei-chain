package evm_test

import (
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/assert"
)

func TestExportGenesis(t *testing.T) {
	keeper, ctx := testkeeper.MockEVMKeeper()
	genesis := evm.ExportGenesis(ctx, keeper)
	assert.NoError(t, genesis.Validate())
	param := genesis.GetParams()
	assert.Equal(t, types.DefaultParams().PriorityNormalizer, param.PriorityNormalizer)
	assert.Equal(t, types.DefaultParams().BaseFeePerGas, param.BaseFeePerGas)
	assert.Equal(t, types.DefaultParams().MinimumFeePerGas, param.MinimumFeePerGas)
	assert.Equal(t, types.DefaultParams().WhitelistedCwCodeHashesForDelegateCall, param.WhitelistedCwCodeHashesForDelegateCall)
}
