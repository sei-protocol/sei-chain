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
	return types.DefaultParams()
	// params := types.Params{}
	// k.Paramstore.GetParamSet(ctx, &params)
	// return params
}

func (k *Keeper) GetBaseDenom(ctx sdk.Context) string {
	return types.DefaultBaseDenom
	// return k.GetParams(ctx).BaseDenom
}

func (k *Keeper) GetChainConfig(ctx sdk.Context) types.ChainConfig {
	return types.DefaultChainConfig()
	// return k.GetParams(ctx).ChainConfig
}

func (k *Keeper) GetPriorityNormalizer(ctx sdk.Context) sdk.Dec {
	return types.DefaultPriorityNormalizer
	// return k.GetParams(ctx).PriorityNormalizer
}

func (k *Keeper) GetBaseFeePerGas(ctx sdk.Context) sdk.Dec {
	return types.DefaultBaseFeePerGas
	// return k.GetParams(ctx).BaseFeePerGas
}

func (k *Keeper) GetMinimumFeePerGas(ctx sdk.Context) sdk.Dec {
	return types.DefaultMinFeePerGas
	// return k.GetParams(ctx).MinimumFeePerGas
}

func (k *Keeper) ChainID(ctx sdk.Context) *big.Int {
	return types.DefaultChainID.BigInt()
	// return k.GetParams(ctx).ChainId.BigInt()
}

func (k *Keeper) WhitelistedCodehashesBankSend(ctx sdk.Context) []string {
	return types.DefaultWhitelistedCodeHashesBankSend
	// return k.GetParams(ctx).WhitelistedCodehashesBankSend
}

func (k *Keeper) WhitelistedCwCodeHashesForDelegateCall(ctx sdk.Context) [][]byte {
	return types.DefaultWhitelistedCwCodeHashesForDelegateCall
	// return k.GetParams(ctx).WhitelistedCwCodeHashesForDelegateCall
}
