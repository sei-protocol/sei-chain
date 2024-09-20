package keeper

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const BaseDenom = "usei"

func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.Paramstore.SetParamSet(ctx, &params)
}

func (k *Keeper) GetParams(ctx sdk.Context) (params types.Params) {
	if ctx.IsCheckTx() {
		return k.GetParamsIfExists(ctx)
	}
	params = types.Params{}
	defer func() {
		if r := recover(); r != nil {
			// If panic occurs, try to get V590 params
			fmt.Printf("[Debug panic get paramset with height: %d\n", ctx.BlockHeight())
			params = k.GetV590Params(ctx)
		}
	}()
	k.Paramstore.GetParamSet(ctx, &params)
	return params
}

func (k *Keeper) GetV590Params(ctx sdk.Context) types.Params {
	v590Params := types.ParamsV590{}
	k.Paramstore.GetParamSet(ctx, &v590Params)
	// Convert GetV590Params to types.Params
	return types.Params{
		PriorityNormalizer:                     v590Params.PriorityNormalizer,
		BaseFeePerGas:                          v590Params.BaseFeePerGas,
		MinimumFeePerGas:                       v590Params.MinimumFeePerGas,
		WhitelistedCwCodeHashesForDelegateCall: v590Params.WhitelistedCwCodeHashesForDelegateCall,
		DeliverTxHookWasmGasLimit:              uint64(300000),
	}
}

func (k *Keeper) GetParamsIfExists(ctx sdk.Context) types.Params {
	params := types.Params{}
	k.Paramstore.GetParamSetIfExists(ctx, &params)
	return params
}

func (k *Keeper) GetBaseDenom(ctx sdk.Context) string {
	return BaseDenom
}

func (k *Keeper) GetPriorityNormalizer(ctx sdk.Context) sdk.Dec {
	return k.GetParams(ctx).PriorityNormalizer
}

func (k *Keeper) GetBaseFeePerGas(ctx sdk.Context) sdk.Dec {
	return k.GetParams(ctx).BaseFeePerGas
}

func (k *Keeper) GetMinimumFeePerGas(ctx sdk.Context) sdk.Dec {
	return k.GetParams(ctx).MinimumFeePerGas
}

func (k *Keeper) GetDeliverTxHookWasmGasLimit(ctx sdk.Context) uint64 {
	return k.GetParams(ctx).DeliverTxHookWasmGasLimit
}

func (k *Keeper) ChainID(ctx sdk.Context) *big.Int {
	if k.EthReplayConfig.Enabled || k.EthBlockTestConfig.Enabled {
		// replay is for eth mainnet so always return 1
		return utils.Big1
	}
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
