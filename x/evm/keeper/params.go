package keeper

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.Paramstore.SetParamSet(ctx, &params)
}

func (k *Keeper) GetParams(ctx sdk.Context) types.Params {
	params := types.Params{}
	k.Paramstore.GetParamSet(ctx, &params)
	return params
}

func (k *Keeper) GetBaseDenom(ctx sdk.Context) string {
	return k.GetParams(ctx).BaseDenom
}

func (k *Keeper) GetChainConfig(ctx sdk.Context) types.ChainConfig {
	return k.GetParams(ctx).ChainConfig
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

func (k *Keeper) ChainID(ctx sdk.Context) *big.Int {
	return k.GetParams(ctx).ChainId.BigInt()
}
