package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (k *Keeper) HandleEBLiquidation(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) {
	_, liquidationSpan := (*tracer).Start(ctx, "SudoLiquidation")
	liquidationSpan.SetAttributes(attribute.String("contractAddr", contractAddr))

	msg := k.getLiquidationSudoMsg(contractAddr)
	data := k.CallContractSudo(sdkCtx, contractAddr, msg)
	response := types.SudoLiquidationResponse{}
	json.Unmarshal(data, &response)
	sdkCtx.Logger().Info(fmt.Sprintf("Sudo liquidate response data: %s", response))

	for _, pair := range registeredPairs {
		if cancellations, ok := k.OrderCancellations[contractAddr][pair.String()]; ok {
			cancellations.UpdateForLiquidation(response.SuccessfulAccounts)
		}
		if placements, ok := k.OrderPlacements[contractAddr][pair.String()]; ok {
			placements.FilterOutAccounts(response.SuccessfulAccounts)
		}
	}
	k.placeLiquidationOrders(sdkCtx, contractAddr, response.LiquidationOrders)

	liquidationSpan.End()
}

func (k *Keeper) placeLiquidationOrders(ctx sdk.Context, contractAddr string, liquidationOrders []types.LiquidationOrder) {
	nextId := k.GetNextOrderId(ctx)
	for _, order := range liquidationOrders {
		priceDenom, _, err := types.GetDenomFromStr(order.PriceDenom)
		if err != nil {
			panic(err)
		}
		assetDenom, _, err := types.GetDenomFromStr(order.AssetDenom)
		if err != nil {
			panic(err)
		}
		pair := types.Pair{PriceDenom: priceDenom, AssetDenom: assetDenom}
		orderPlacements := k.OrderPlacements[contractAddr][pair.String()]
		orderPlacements.Orders = append(orderPlacements.Orders, dexcache.FromLiquidationOrder(order, nextId))
		nextId += 1
	}
	k.SetNextOrderId(ctx, nextId)
}

func (k *Keeper) getLiquidationSudoMsg(contractAddr string) types.SudoLiquidationMsg {
	cachedLiquidationRequests := k.LiquidationRequests[contractAddr]
	liquidationRequests := []types.LiquidationRequest{}
	for _, cachedLiquidationRequest := range *cachedLiquidationRequests {
		liquidationRequests = append(liquidationRequests, types.LiquidationRequest{
			Requestor: cachedLiquidationRequest.Requestor,
			Account:   cachedLiquidationRequest.AccountToLiquidate,
		})
	}
	return types.SudoLiquidationMsg{
		Liquidation: types.ContractLiquidationDetails{
			Requests: liquidationRequests,
		},
	}
}
