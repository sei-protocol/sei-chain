package wasm

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/query"
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
	wrapper := query.KeeperWrapper{Keeper: &handler.dexKeeper}
	return wrapper.GetTwaps(c, req)
}

func (handler DexWasmQueryHandler) GetOrders(ctx sdk.Context, req *types.QueryGetOrdersRequest) (*types.QueryGetOrdersResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: &handler.dexKeeper}
	return wrapper.GetOrders(c, req)
}

func (handler DexWasmQueryHandler) GetOrderByID(ctx sdk.Context, req *types.QueryGetOrderByIDRequest) (*types.QueryGetOrderByIDResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: &handler.dexKeeper}
	return wrapper.GetOrder(c, req)
}

func (handler DexWasmQueryHandler) GetOrderSimulation(ctx sdk.Context, req *types.QueryOrderSimulationRequest) (*types.QueryOrderSimulationResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: &handler.dexKeeper}
	return wrapper.GetOrderSimulation(c, req)
}

func (handler DexWasmQueryHandler) GetLatestPrice(ctx sdk.Context, req *types.QueryGetLatestPriceRequest) (*types.QueryGetLatestPriceResponse, error) {
	c := sdk.WrapSDKContext(ctx)
	wrapper := query.KeeperWrapper{Keeper: &handler.dexKeeper}
	return wrapper.GetLatestPrice(c, req)
}
