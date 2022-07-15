package wasm

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type DexWasmQueryHandler struct {
	dexKeeper keeper.Keeper
}

func NewDexWasmQueryHandler(keeper *keeper.Keeper) *DexWasmQueryHandler {
	return &DexWasmQueryHandler{
		dexKeeper: *keeper,
	}
}

func (handler DexWasmQueryHandler) GetDexTwap(ctx sdk.Context, req *types.QueryGetTwapsRequest) (*types.QueryGetTwapsResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	return handler.dexKeeper.GetTwaps(c, req)
}

func (handler DexWasmQueryHandler) GetOrders(ctx sdk.Context, req *types.QueryGetOrdersRequest) (*types.QueryGetOrdersResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	return handler.dexKeeper.GetOrders(c, req)
}

func (handler DexWasmQueryHandler) GetOrderByID(ctx sdk.Context, req *types.QueryGetOrderByIDRequest) (*types.QueryGetOrderByIDResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	return handler.dexKeeper.GetOrderByID(c, req)
}
