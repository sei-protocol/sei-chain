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

// There is a limit on how many bytes can be passed to wasm VM in a single call,
// so we shouldn't bump this number unless there is an upgrade to wasm VM
const MAX_ORDERS_PER_SUDO_CALL = 50000

func (k *Keeper) HandleEBPlaceOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) {
	_, span := (*tracer).Start(ctx, "SudoPlaceOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))

	typedContractAddr := types.ContractAddress(contractAddr)
	msgs := k.GetPlaceSudoMsg(typedContractAddr, registeredPairs)
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
		typedPairStr := types.PairString(pair.String())
		if orders, ok := k.BlockOrders[typedContractAddr][typedPairStr]; ok {
			for _, response := range responses {
				orders.MarkFailedToPlaceByIds(response.UnsuccessfulOrderIds)
			}
		}
	}
	span.End()
}

func (k *Keeper) GetPlaceSudoMsg(typedContractAddr types.ContractAddress, registeredPairs []types.Pair) []types.SudoOrderPlacementMsg {
	contractDepositInfo := []types.ContractDepositInfo{}
	for _, depositInfo := range k.DepositInfo[typedContractAddr].DepositInfoList {
		contractDepositInfo = append(contractDepositInfo, dexcache.ToContractDepositInfo(depositInfo))
	}
	contractOrderPlacements := []types.Order{}
	msgs := []types.SudoOrderPlacementMsg{
		{
			OrderPlacements: types.OrderPlacementMsgDetails{
				Orders:   []types.Order{},
				Deposits: contractDepositInfo,
			},
		},
	}
	for _, pair := range registeredPairs {
		typedPairStr := types.PairString(pair.String())
		if orders, ok := k.BlockOrders[typedContractAddr][typedPairStr]; ok {
			for _, order := range *orders {
				if order.OrderType != types.OrderType_LIQUIDATION {
					contractOrderPlacements = append(contractOrderPlacements, order)
					if len(contractOrderPlacements) == MAX_ORDERS_PER_SUDO_CALL {
						msgs = append(msgs, types.SudoOrderPlacementMsg{
							OrderPlacements: types.OrderPlacementMsgDetails{
								Orders:   contractOrderPlacements,
								Deposits: []types.ContractDepositInfo{},
							},
						})
						contractOrderPlacements = []types.Order{}
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
