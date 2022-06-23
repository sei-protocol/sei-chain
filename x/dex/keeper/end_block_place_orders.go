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

const MAX_ORDERS_PER_SUDO_CALL = 50000

func (k *Keeper) HandleEBPlaceOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) {
	_, span := (*tracer).Start(ctx, "SudoPlaceOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))

	msgs := k.GetPlaceSudoMsg(contractAddr, registeredPairs)
	k.CallContractSudo(sdkCtx, contractAddr, msgs[0]) // deposit

	responses := []types.SudoOrderPlacementResponse{}
	for _, msg := range msgs[1:] {
		data := k.CallContractSudo(sdkCtx, contractAddr, msg)
		response := types.SudoOrderPlacementResponse{}
		json.Unmarshal(data, &response)
		sdkCtx.Logger().Info(fmt.Sprintf("Sudo response data: %s", response))
		responses = append(responses, response)
	}
	for _, pair := range registeredPairs {
		pairStr := pair.String()
		if orderPlacements, ok := k.OrderPlacements[contractAddr][pairStr]; ok {
			for _, response := range responses {
				orderPlacements.FilterOutIds(response.UnsuccessfulOrderIds)
			}
			for _, orderPlacement := range orderPlacements.Orders {
				k.AddOrderFromOrderPlacement(contractAddr, pairStr, orderPlacement)
			}
		}
	}
	span.End()
}

func (k *Keeper) AddOrderFromOrderPlacement(contractAddr string, pairStr string, orderPlacement dexcache.OrderPlacement) {
	switch orderPlacement.OrderType {
	case types.OrderType_LIMIT:
		k.Orders[contractAddr][pairStr].AddLimitOrder(dexcache.LimitOrder{
			Creator:   orderPlacement.Creator,
			Price:     orderPlacement.Price,
			Quantity:  orderPlacement.Quantity,
			Direction: orderPlacement.Direction,
			Effect:    orderPlacement.Effect,
			Leverage:  orderPlacement.Leverage,
		})
	case types.OrderType_MARKET:
		k.Orders[contractAddr][pairStr].AddMarketOrder(dexcache.MarketOrder{
			Creator:       orderPlacement.Creator,
			WorstPrice:    orderPlacement.Price,
			Quantity:      orderPlacement.Quantity,
			Direction:     orderPlacement.Direction,
			Effect:        orderPlacement.Effect,
			Leverage:      orderPlacement.Leverage,
			IsLiquidation: false,
		})
	case types.OrderType_LIQUIDATION:
		k.Orders[contractAddr][pairStr].AddMarketOrder(dexcache.MarketOrder{
			Creator:       orderPlacement.Creator,
			WorstPrice:    orderPlacement.Price,
			Quantity:      orderPlacement.Quantity,
			Direction:     orderPlacement.Direction,
			Effect:        orderPlacement.Effect,
			Leverage:      orderPlacement.Leverage,
			IsLiquidation: true,
		})
	}
}

func (k *Keeper) GetPlaceSudoMsg(contractAddr string, registeredPairs []types.Pair) []types.SudoOrderPlacementMsg {
	contractDepositInfo := []types.ContractDepositInfo{}
	for _, depositInfo := range k.DepositInfo[contractAddr].DepositInfoList {
		contractDepositInfo = append(contractDepositInfo, dexcache.ToContractDepositInfo(depositInfo))
	}
	contractOrderPlacements := []types.ContractOrderPlacement{}
	msgs := []types.SudoOrderPlacementMsg{
		{
			OrderPlacements: types.OrderPlacementMsgDetails{
				Orders:   []types.ContractOrderPlacement{},
				Deposits: contractDepositInfo,
			},
		},
	}
	for _, pair := range registeredPairs {
		if orderPlacements, ok := k.OrderPlacements[contractAddr][pair.String()]; ok {
			for _, orderPlacement := range orderPlacements.Orders {
				if orderPlacement.OrderType != types.OrderType_LIQUIDATION {
					contractOrderPlacements = append(contractOrderPlacements, dexcache.ToContractOrderPlacement(orderPlacement))
					if len(contractOrderPlacements) == MAX_ORDERS_PER_SUDO_CALL {
						msgs = append(msgs, types.SudoOrderPlacementMsg{
							OrderPlacements: types.OrderPlacementMsgDetails{
								Orders:   contractOrderPlacements,
								Deposits: []types.ContractDepositInfo{},
							},
						})
						contractOrderPlacements = []types.ContractOrderPlacement{}
					}
				}
			}
		}
	}
	msgs = append(msgs, types.SudoOrderPlacementMsg{
		OrderPlacements: types.OrderPlacementMsgDetails{
			Orders:   contractOrderPlacements,
			Deposits: []types.ContractDepositInfo{},
		},
	})
	return msgs
}
