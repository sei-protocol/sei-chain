package evm_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportImportGenesis(t *testing.T) {
	keeper := &testkeeper.EVMTestApp.EvmKeeper
	origctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	ctx := origctx.WithMultiStore(origctx.MultiStore().CacheMultiStore())
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	keeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	_, codeAddr := testkeeper.MockAddressPair()
	keeper.SetCode(ctx, codeAddr, []byte("abcde"))
	keeper.SetState(ctx, codeAddr, common.BytesToHash([]byte("123")), common.BytesToHash([]byte("456")))
	keeper.SetNonce(ctx, evmAddr, 2)
	keeper.MockReceipt(ctx, common.BytesToHash([]byte("789")), &types.Receipt{TxType: 2})
	keeper.SetBlockBloom(ctx, []ethtypes.Bloom{{1}})
	keeper.SetTxHashesOnHeight(ctx, 5, []common.Hash{common.BytesToHash([]byte("123"))})
	keeper.SetERC20CW20Pointer(ctx, "cw20addr", codeAddr)
	genesis := evm.ExportGenesis(ctx, keeper)
	assert.NoError(t, genesis.Validate())
	param := genesis.GetParams()
	assert.Equal(t, types.DefaultParams().PriorityNormalizer, param.PriorityNormalizer)
	assert.Equal(t, types.DefaultParams().BaseFeePerGas, param.BaseFeePerGas)
	assert.Equal(t, types.DefaultParams().MinimumFeePerGas, param.MinimumFeePerGas)
	assert.Equal(t, types.DefaultParams().WhitelistedCwCodeHashesForDelegateCall, param.WhitelistedCwCodeHashesForDelegateCall)
	assert.Equal(t, types.DefaultParams().MaxDynamicBaseFeeUpwardAdjustment, param.MaxDynamicBaseFeeUpwardAdjustment)
	assert.Equal(t, types.DefaultParams().MaxDynamicBaseFeeDownwardAdjustment, param.MaxDynamicBaseFeeDownwardAdjustment)
	evm.InitGenesis(origctx, keeper, *genesis)
	require.Equal(t, evmAddr, keeper.GetEVMAddressOrDefault(origctx, seiAddr))
	require.Equal(t, keeper.GetCode(ctx, codeAddr), keeper.GetCode(origctx, codeAddr))
	require.Equal(t, keeper.GetCodeHash(ctx, codeAddr), keeper.GetCodeHash(origctx, codeAddr))
	require.Equal(t, keeper.GetCodeSize(ctx, codeAddr), keeper.GetCodeSize(origctx, codeAddr))
	require.Equal(t, keeper.GetState(ctx, codeAddr, common.BytesToHash([]byte("123"))), keeper.GetState(origctx, codeAddr, common.BytesToHash([]byte("123"))))
	require.Equal(t, keeper.GetNonce(ctx, evmAddr), keeper.GetNonce(origctx, evmAddr))
	_, err := keeper.GetReceipt(origctx, common.BytesToHash([]byte("789")))
	require.Nil(t, err)
	require.Equal(t, keeper.GetBlockBloom(ctx), keeper.GetBlockBloom(origctx))
	require.Equal(t, keeper.GetTxHashesOnHeight(ctx, 5), keeper.GetTxHashesOnHeight(origctx, 5))
	_, _, exists := keeper.GetERC20CW20Pointer(origctx, "cw20addr")
	require.True(t, exists)
}
