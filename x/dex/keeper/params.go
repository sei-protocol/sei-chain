package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// GetParams get all parameters as types.Params
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	params := types.Params{}
	k.Paramstore.GetParamSet(ctx, &params)
	return params
}

func (k Keeper) GetSettlementGasAllowance(ctx sdk.Context, numSettlements int) uint64 {
	return k.GetParams(ctx).GasAllowancePerSettlement * uint64(numSettlements)
}

func (k Keeper) GetMinProcessableRent(ctx sdk.Context) uint64 {
	return k.GetParams(ctx).MinProcessableRent
}

func (k Keeper) GetOrderBookEntriesPerLoad(ctx sdk.Context) uint64 {
	return k.GetParams(ctx).OrderBookEntriesPerLoad
}

func (k Keeper) GetContractUnsuspendCost(ctx sdk.Context) uint64 {
	return k.GetParams(ctx).ContractUnsuspendCost
}

func (k Keeper) GetMaxOrderPerPrice(ctx sdk.Context) uint64 {
	return k.GetParams(ctx).MaxOrderPerPrice
}

func (k Keeper) GetMaxPairsPerContract(ctx sdk.Context) uint64 {
	return k.GetParams(ctx).MaxPairsPerContract
}

// SetParams set the params
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	k.Paramstore.SetParamSet(ctx, &params)
}
