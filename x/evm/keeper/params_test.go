package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestParams(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	require.Equal(t, "usei", k.GetBaseDenom(ctx))
	require.Equal(t, types.DefaultPriorityNormalizer, k.GetPriorityNormalizer(ctx))
	require.Equal(t, types.DefaultMinFeePerGas, k.GetNextBaseFeePerGas(ctx))
	require.Equal(t, types.DefaultBaseFeePerGas, k.GetBaseFeePerGas(ctx))
	require.Equal(t, types.DefaultMinFeePerGas, k.GetMinimumFeePerGas(ctx))
	require.Equal(t, types.DefaultMaxFeePerGas, k.GetMaximumFeePerGas(ctx))
	require.True(t, k.GetMinimumFeePerGas(ctx).LTE(k.GetMaximumFeePerGas(ctx)))
	require.Equal(t, types.DefaultDeliverTxHookWasmGasLimit, k.GetDeliverTxHookWasmGasLimit(ctx))
	require.Equal(t, types.DefaultMaxDynamicBaseFeeUpwardAdjustment, k.GetMaxDynamicBaseFeeUpwardAdjustment(ctx))
	require.Equal(t, types.DefaultMaxDynamicBaseFeeDownwardAdjustment, k.GetMaxDynamicBaseFeeDownwardAdjustment(ctx))
	require.Nil(t, k.GetParams(ctx).Validate())
}

func TestGetParamsIfExists(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())

	// Define the expected parameters
	expectedParams := types.Params{
		PriorityNormalizer: sdk.NewDec(1),
		BaseFeePerGas:      sdk.NewDec(1),
	}

	// Set only a subset of the parameters in the keeper
	k.Paramstore.Set(ctx, types.KeyPriorityNormalizer, expectedParams.PriorityNormalizer)
	k.Paramstore.Set(ctx, types.KeyBaseFeePerGas, expectedParams.BaseFeePerGas)

	// Retrieve the parameters using GetParamsIfExists
	params := k.GetParamsIfExists(ctx)

	// Assert that the retrieved parameters match the expected parameters
	require.Equal(t, expectedParams.PriorityNormalizer, params.PriorityNormalizer)
	require.Equal(t, expectedParams.BaseFeePerGas, params.BaseFeePerGas)

	// Assert that the missing parameter has its default value
	require.Equal(t, types.DefaultParams().DeliverTxHookWasmGasLimit, params.DeliverTxHookWasmGasLimit)
}

func TestParamGettersTracingVersions(t *testing.T) {
	k, baseCtx := testkeeper.MockEVMKeeper(t)

	// custom values to distinguish from defaults
	customBaseFee := sdk.NewDec(123456)
	customMinFee := sdk.NewDec(654321)
	customMaxFee := sdk.NewDec(987654)
	customUpward := sdk.NewDecWithPrec(123, 2)  // 1.23
	customDownward := sdk.NewDecWithPrec(45, 2) // 0.45
	customTargetGas := uint64(111111)
	customDeliverTxGasLimit := uint64(222222)
	customRegisterPointerDisabled := true

	// Populate Paramstore with custom values (these keys are shared across all versioned Param structs)
	k.Paramstore.Set(baseCtx, types.KeyBaseFeePerGas, customBaseFee)
	k.Paramstore.Set(baseCtx, types.KeyMinFeePerGas, customMinFee)
	k.Paramstore.Set(baseCtx, types.KeyMaxFeePerGas, customMaxFee)
	k.Paramstore.Set(baseCtx, types.KeyMaxDynamicBaseFeeUpwardAdjustment, customUpward)
	k.Paramstore.Set(baseCtx, types.KeyMaxDynamicBaseFeeDownwardAdjustment, customDownward)
	k.Paramstore.Set(baseCtx, types.KeyTargetGasUsedPerBlock, customTargetGas)
	k.Paramstore.Set(baseCtx, types.KeyDeliverTxHookWasmGasLimit, customDeliverTxGasLimit)
	k.Paramstore.Set(baseCtx, types.KeyRegisterPointerDisabled, customRegisterPointerDisabled)

	// ---- Pre-v5.8.0 (ParamsPreV580 path) ----
	ctxPre580 := baseCtx.WithIsTracing(true).WithClosestUpgradeName("v5.7.0")

	require.Equal(t, customBaseFee, k.GetBaseFeePerGas(ctxPre580))
	require.Equal(t, customMinFee, k.GetMinimumFeePerGas(ctxPre580))
	// Not supported pre-5.8.0, should fall back to defaults
	require.Equal(t, types.DefaultMaxDynamicBaseFeeUpwardAdjustment, k.GetMaxDynamicBaseFeeUpwardAdjustment(ctxPre580))
	require.Equal(t, types.DefaultMaxDynamicBaseFeeDownwardAdjustment, k.GetMaxDynamicBaseFeeDownwardAdjustment(ctxPre580))
	require.Equal(t, types.DefaultMaxFeePerGas, k.GetMaximumFeePerGas(ctxPre580))
	require.Equal(t, types.DefaultTargetGasUsedPerBlock, k.GetTargetGasUsedPerBlock(ctxPre580))
	require.Equal(t, types.DefaultDeliverTxHookWasmGasLimit, k.GetDeliverTxHookWasmGasLimit(ctxPre580))
	require.Equal(t, types.DefaultRegisterPointerDisabled, k.GetRegisterPointerDisabled(ctxPre580))

	// ---- Between v5.8.0 and v6.0.6 (ParamsPreV606 path) ----
	ctxPre606 := baseCtx.WithIsTracing(true).WithClosestUpgradeName("v6.0.5")

	require.Equal(t, customBaseFee, k.GetBaseFeePerGas(ctxPre606))
	require.Equal(t, customMinFee, k.GetMinimumFeePerGas(ctxPre606))
	require.Equal(t, customUpward, k.GetMaxDynamicBaseFeeUpwardAdjustment(ctxPre606))
	require.Equal(t, customDownward, k.GetMaxDynamicBaseFeeDownwardAdjustment(ctxPre606))
	require.Equal(t, customMaxFee, k.GetMaximumFeePerGas(ctxPre606))
	require.Equal(t, customTargetGas, k.GetTargetGasUsedPerBlock(ctxPre606))
	require.Equal(t, customDeliverTxGasLimit, k.GetDeliverTxHookWasmGasLimit(ctxPre606))
	// RegisterPointerDisabled is unavailable pre-6.0.6 → default
	require.Equal(t, types.DefaultRegisterPointerDisabled, k.GetRegisterPointerDisabled(ctxPre606))

	// ---- v6.0.6 and later (current Params path) ----
	ctxPost606 := baseCtx.WithIsTracing(true).WithClosestUpgradeName("v6.1.0")

	require.Equal(t, customBaseFee, k.GetBaseFeePerGas(ctxPost606))
	require.Equal(t, customMinFee, k.GetMinimumFeePerGas(ctxPost606))
	require.Equal(t, customUpward, k.GetMaxDynamicBaseFeeUpwardAdjustment(ctxPost606))
	require.Equal(t, customDownward, k.GetMaxDynamicBaseFeeDownwardAdjustment(ctxPost606))
	require.Equal(t, customMaxFee, k.GetMaximumFeePerGas(ctxPost606))
	require.Equal(t, customTargetGas, k.GetTargetGasUsedPerBlock(ctxPost606))
	require.Equal(t, customDeliverTxGasLimit, k.GetDeliverTxHookWasmGasLimit(ctxPost606))
	require.Equal(t, customRegisterPointerDisabled, k.GetRegisterPointerDisabled(ctxPost606))
}
