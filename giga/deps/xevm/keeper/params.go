package keeper

import (
	"math"
	"math/big"
	"strings"

	"github.com/sei-protocol/sei-chain/giga/deps/xevm/config"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
)

const BaseDenom = "usei"

var MaxUint64BigInt = new(big.Int).SetUint64(math.MaxUint64)

func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.Paramstore.SetParamSet(ctx, &params)
}

func (k *Keeper) GetParams(ctx sdk.Context) (params types.Params) {
	return k.GetParamsIfExists(ctx)
}

func (k *Keeper) GetParamsPreV580(ctx sdk.Context) (params types.ParamsPreV580) {
	return k.GetParamsPreV580IfExists(ctx)
}

func (k *Keeper) GetParamsPreV600(ctx sdk.Context) (params types.ParamsPreV600) {
	return k.GetParamsPreV600IfExists(ctx)
}

func (k *Keeper) GetParamsPreV601(ctx sdk.Context) (params types.ParamsPreV601) {
	return k.GetParamsPreV601IfExists(ctx)
}

func (k *Keeper) GetParamsPreV606(ctx sdk.Context) (params types.ParamsPreV606) {
	return k.GetParamsPreV606IfExists(ctx)
}

func (k *Keeper) GetParamsIfExists(ctx sdk.Context) types.Params {
	params := types.Params{}
	k.Paramstore.GetParamSetIfExists(ctx, &params)
	return params
}

func (k *Keeper) GetParamsPreV580IfExists(ctx sdk.Context) types.ParamsPreV580 {
	params := types.ParamsPreV580{}
	k.Paramstore.GetParamSetIfExists(ctx, &params)
	return params
}

func (k *Keeper) GetParamsPreV600IfExists(ctx sdk.Context) types.ParamsPreV600 {
	params := types.ParamsPreV600{}
	k.Paramstore.GetParamSetIfExists(ctx, &params)
	return params
}

func (k *Keeper) GetParamsPreV601IfExists(ctx sdk.Context) types.ParamsPreV601 {
	params := types.ParamsPreV601{}
	k.Paramstore.GetParamSetIfExists(ctx, &params)
	return params
}

func (k *Keeper) GetParamsPreV606IfExists(ctx sdk.Context) types.ParamsPreV606 {
	params := types.ParamsPreV606{}
	k.Paramstore.GetParamSetIfExists(ctx, &params)
	return params
}

func (k *Keeper) GetBaseDenom(ctx sdk.Context) string {
	return BaseDenom
}

func (k *Keeper) GetPriorityNormalizer(ctx sdk.Context) sdk.Dec {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).PriorityNormalizer
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v5.8.0") < 0:
		return k.GetParamsPreV580(ctx).PriorityNormalizer
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.0") < 0:
		return k.GetParamsPreV600(ctx).PriorityNormalizer
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.1") < 0:
		return k.GetParamsPreV601(ctx).PriorityNormalizer
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		return k.GetParamsPreV606(ctx).PriorityNormalizer
	default:
		return k.GetParams(ctx).PriorityNormalizer
	}
}

func (k *Keeper) GetBaseFeePerGas(ctx sdk.Context) sdk.Dec {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).BaseFeePerGas
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v5.8.0") < 0:
		return k.GetParamsPreV580(ctx).BaseFeePerGas
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.0") < 0:
		return k.GetParamsPreV600(ctx).BaseFeePerGas
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.1") < 0:
		return k.GetParamsPreV601(ctx).BaseFeePerGas
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		return k.GetParamsPreV606(ctx).BaseFeePerGas
	default:
		return k.GetParams(ctx).BaseFeePerGas
	}
}

func (k *Keeper) GetMaxDynamicBaseFeeUpwardAdjustment(ctx sdk.Context) sdk.Dec {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).MaxDynamicBaseFeeUpwardAdjustment
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.0") < 0:
		// Not present in pre-6.0.0 params; use default
		return types.DefaultMaxDynamicBaseFeeUpwardAdjustment
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.1") < 0:
		return k.GetParamsPreV601(ctx).MaxDynamicBaseFeeUpwardAdjustment
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		return k.GetParamsPreV606(ctx).MaxDynamicBaseFeeUpwardAdjustment
	default:
		return k.GetParams(ctx).MaxDynamicBaseFeeUpwardAdjustment
	}
}

func (k *Keeper) GetMaxDynamicBaseFeeDownwardAdjustment(ctx sdk.Context) sdk.Dec {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).MaxDynamicBaseFeeDownwardAdjustment
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.0") < 0:
		// Not present in pre-6.0.0 params; use default
		return types.DefaultMaxDynamicBaseFeeDownwardAdjustment
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.1") < 0:
		return k.GetParamsPreV601(ctx).MaxDynamicBaseFeeDownwardAdjustment
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		return k.GetParamsPreV606(ctx).MaxDynamicBaseFeeDownwardAdjustment
	default:
		return k.GetParams(ctx).MaxDynamicBaseFeeDownwardAdjustment
	}
}

func (k *Keeper) GetMinimumFeePerGas(ctx sdk.Context) sdk.Dec {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).MinimumFeePerGas
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v5.8.0") < 0:
		return k.GetParamsPreV580(ctx).MinimumFeePerGas
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.0") < 0:
		return k.GetParamsPreV600(ctx).MinimumFeePerGas
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.1") < 0:
		return k.GetParamsPreV601(ctx).MinimumFeePerGas
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		return k.GetParamsPreV606(ctx).MinimumFeePerGas
	default:
		return k.GetParams(ctx).MinimumFeePerGas
	}
}

func (k *Keeper) GetMaximumFeePerGas(ctx sdk.Context) sdk.Dec {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).MaximumFeePerGas
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.1") < 0:
		// Not present in pre-6.0.1 params; use default
		return types.DefaultMaxFeePerGas
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		return k.GetParamsPreV606(ctx).MaximumFeePerGas
	default:
		return k.GetParams(ctx).MaximumFeePerGas
	}
}

func (k *Keeper) GetTargetGasUsedPerBlock(ctx sdk.Context) uint64 {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).TargetGasUsedPerBlock
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.0") < 0:
		// Not present in pre-6.0.0 params; use default
		return types.DefaultTargetGasUsedPerBlock
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.1") < 0:
		return k.GetParamsPreV601(ctx).TargetGasUsedPerBlock
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		return k.GetParamsPreV606(ctx).TargetGasUsedPerBlock
	default:
		return k.GetParams(ctx).TargetGasUsedPerBlock
	}
}

func (k *Keeper) GetDeliverTxHookWasmGasLimit(ctx sdk.Context) uint64 {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).DeliverTxHookWasmGasLimit
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v5.8.0") < 0:
		// Not present in pre-5.8.0 params; use default
		return types.DefaultDeliverTxHookWasmGasLimit
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.0") < 0:
		return k.GetParamsPreV600(ctx).DeliverTxHookWasmGasLimit
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.1") < 0:
		return k.GetParamsPreV601(ctx).DeliverTxHookWasmGasLimit
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		return k.GetParamsPreV606(ctx).DeliverTxHookWasmGasLimit
	default:
		return k.GetParams(ctx).DeliverTxHookWasmGasLimit
	}
}

func (k *Keeper) GetRegisterPointerDisabled(ctx sdk.Context) bool {
	if !ctx.IsTracing() {
		return k.GetParams(ctx).RegisterPointerDisabled
	}
	switch {
	case strings.Compare(ctx.ClosestUpgradeName(), "v6.0.6") < 0:
		// Not present in pre-5.8.0 params; use default
		return types.DefaultRegisterPointerDisabled
	default:
		return k.GetParams(ctx).RegisterPointerDisabled
	}
}

func (k *Keeper) ChainID(ctx sdk.Context) *big.Int {
	// return mapped chain ID
	return config.GetEVMChainID(ctx.ChainID())

}

/*
*
sei gas = evm gas * multiplier
sei gas price = fee / sei gas = fee / (evm gas * multiplier) = evm gas / multiplier
*/
func (k *Keeper) GetEVMGasLimitFromCtx(ctx sdk.Context) uint64 {
	return k.getEvmGasLimitFromCtx(ctx)
}

func (k *Keeper) GetCosmosGasLimitFromEVMGas(ctx sdk.Context, evmGas uint64) uint64 {
	gasMultipler := k.GetPriorityNormalizer(ctx)
	gasLimitBigInt := sdk.NewDecFromInt(sdk.NewIntFromUint64(evmGas)).Mul(gasMultipler).TruncateInt().BigInt()
	if gasLimitBigInt.Cmp(utils.BigMaxU64) > 0 {
		gasLimitBigInt = utils.BigMaxU64
	}
	return gasLimitBigInt.Uint64()
}

func (k *Keeper) getEvmGasLimitFromCtx(ctx sdk.Context) uint64 {
	seiGasRemaining := ctx.GasMeter().Limit() - ctx.GasMeter().GasConsumedToLimit()
	if ctx.GasMeter().Limit() <= 0 {
		return math.MaxUint64
	}
	if ctx.ChainID() != Pacific1ChainID || ctx.BlockHeight() >= 119821526 {
		ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	}
	evmGasBig := sdk.NewDecFromInt(sdk.NewIntFromUint64(seiGasRemaining)).Quo(k.GetPriorityNormalizer(ctx)).TruncateInt().BigInt()
	if evmGasBig.Cmp(MaxUint64BigInt) > 0 {
		evmGasBig = MaxUint64BigInt
	}
	return evmGasBig.Uint64()
}
