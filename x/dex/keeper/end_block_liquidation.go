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

func (k *Keeper) HandleEBLiquidation(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string) {
	_, liquidationSpan := (*tracer).Start(ctx, "SudoLiquidation")
	liquidationSpan.SetAttributes(attribute.String("contractAddr", contractAddr))

	msg := k.getLiquidationSudoMsg(contractAddr)
	data := k.CallContractSudo(sdkCtx, contractAddr, msg)
	response := types.SudoLiquidationResponse{}
	json.Unmarshal(data, &response)
	sdkCtx.Logger().Info(fmt.Sprintf("Sudo liquidate response data: %s", response))

	for _, cancellations := range k.OrderCancellations[contractAddr] {
		cancellations.UpdateForLiquidation(response.SuccessfulAccounts)
	}

	for _, placements := range k.OrderPlacements[contractAddr] {
		placements.FilterOutAccounts(response.SuccessfulAccounts)
	}
	k.placeLiquidationOrders(sdkCtx, contractAddr, response.LiquidationOrders)

	liquidationSpan.End()
}

func (k *Keeper) placeLiquidationOrders(ctx sdk.Context, contractAddr string, liquidationOrders []types.LiquidationOrder) {
	nextId := k.GetNextOrderId(ctx)
	for _, order := range liquidationOrders {
		pair := types.Pair{PriceDenom: types.Denom(types.Denom_value[order.PriceDenom]), AssetDenom: types.Denom(types.Denom_value[order.AssetDenom])}
		orderPlacements := k.OrderPlacements[contractAddr][pair.String()]
		orderPlacements.Orders = append(orderPlacements.Orders, dexcache.FromLiquidationOrder(order, nextId))
		nextId += 1
	}
	k.SetNextOrderId(ctx, nextId)
}

func (k *Keeper) getLiquidationSudoMsg(contractAddr string) types.SudoLiquidationMsg {
	liquidationRequestorToAccounts := k.LiquidationRequests[contractAddr]
	liquidationRequests := []types.LiquidationRequest{}
	for requestor, account := range liquidationRequestorToAccounts {
		liquidationRequests = append(liquidationRequests, types.LiquidationRequest{
			Requestor: requestor,
			Account:   account,
		})
	}
	return types.SudoLiquidationMsg{
		Liquidation: types.ContractLiquidationDetails{
			Requests: liquidationRequests,
		},
	}
}
