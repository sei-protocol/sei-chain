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

func (k *Keeper) HandleEBPlaceOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string) {
	_, span := (*tracer).Start(ctx, "SudoPlaceOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))

	msg := k.getPlaceSudoMsg(contractAddr)
	data := k.CallContractSudo(sdkCtx, contractAddr, msg)
	response := types.SudoOrderPlacementResponse{}
	json.Unmarshal(data, &response)
	sdkCtx.Logger().Info(fmt.Sprintf("Sudo response data: %s", response))
	for pair, orderPlacements := range k.OrderPlacements[contractAddr] {
		orderPlacements.FilterOutIds(response.UnsuccessfulOrderIds)
		for _, orderPlacement := range orderPlacements.Orders {
			if orderPlacement.Limit {
				k.Orders[contractAddr][pair].AddLimitOrder(dexcache.LimitOrder{
					Creator:  orderPlacement.Creator,
					Price:    orderPlacement.Price,
					Quantity: orderPlacement.Quantity,
					Long:     orderPlacement.Long,
					Open:     orderPlacement.Open,
					Leverage: orderPlacement.Leverage,
				})
			} else {
				k.Orders[contractAddr][pair].AddMarketOrder(dexcache.MarketOrder{
					Creator:    orderPlacement.Creator,
					WorstPrice: orderPlacement.Price,
					Quantity:   orderPlacement.Quantity,
					Long:       orderPlacement.Long,
					Open:       orderPlacement.Open,
					Leverage:   orderPlacement.Leverage,
				})
			}
		}
	}
	span.End()
}

func (k *Keeper) getPlaceSudoMsg(contractAddr string) types.SudoOrderPlacementMsg {
	pairToOrderPlacements := k.OrderPlacements[contractAddr]
	contractOrderPlacements := []types.ContractOrderPlacement{}
	for _, orderPlacements := range pairToOrderPlacements {
		for _, orderPlacement := range orderPlacements.Orders {
			if !orderPlacement.Liquidation {
				contractOrderPlacements = append(contractOrderPlacements, dexcache.ToContractOrderPlacement(orderPlacement))
			}
		}
	}
	contractDepositInfo := []types.ContractDepositInfo{}
	for _, depositInfo := range k.DepositInfo[contractAddr].DepositInfoList {
		contractDepositInfo = append(contractDepositInfo, dexcache.ToContractDepositInfo(depositInfo))
	}
	return types.SudoOrderPlacementMsg{
		OrderPlacements: types.OrderPlacementMsgDetails{
			Orders:   contractOrderPlacements,
			Deposits: contractDepositInfo,
		},
	}
}
