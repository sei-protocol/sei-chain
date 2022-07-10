package keeper

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (k *Keeper) HandleEBLiquidation(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) {
	_, liquidationSpan := (*tracer).Start(ctx, "SudoLiquidation")
	liquidationSpan.SetAttributes(attribute.String("contractAddr", contractAddr))

	typedContractAddr := types.ContractAddress(contractAddr)
	msg := k.getLiquidationSudoMsg(typedContractAddr)
	data := k.CallContractSudo(sdkCtx, contractAddr, msg)
	response := types.SudoLiquidationResponse{}
	json.Unmarshal(data, &response)
	sdkCtx.Logger().Info(fmt.Sprintf("Sudo liquidate response data: %s", response))

	liquidatedAccountsActiveOrderIds := []uint64{}
	for _, account := range response.SuccessfulAccounts {
		liquidatedAccountsActiveOrderIds = append(liquidatedAccountsActiveOrderIds, k.GetAccountActiveOrders(sdkCtx, contractAddr, account).Ids...)
	}
	// Clear up all user-initiated order activities in the current block
	for _, pair := range registeredPairs {
		typedPairStr := types.GetPairString(&pair)
		k.MemState.GetBlockCancels(typedContractAddr, typedPairStr).FilterByIds(liquidatedAccountsActiveOrderIds)
		k.MemState.GetBlockOrders(typedContractAddr, typedPairStr).MarkFailedToPlaceByAccounts(response.SuccessfulAccounts)
	}
	// Cancel all outstanding orders of liquidated accounts, as denoted as cancelled via liquidation
	for id, order := range k.GetOrdersByIds(sdkCtx, contractAddr, liquidatedAccountsActiveOrderIds) {
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom}
		typedPairStr := types.GetPairString(&pair)
		k.MemState.GetBlockCancels(typedContractAddr, typedPairStr).AddOrderIdToCancel(id, types.CancellationInitiator_LIQUIDATED)
	}

	// Place liquidation orders
	k.placeLiquidationOrders(sdkCtx, contractAddr, response.LiquidationOrders)

	liquidationSpan.End()
}

func (k *Keeper) placeLiquidationOrders(ctx sdk.Context, contractAddr string, liquidationOrders []types.Order) {
	nextId := k.GetNextOrderId(ctx)
	for _, order := range liquidationOrders {
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom}
		orders := k.MemState.GetBlockOrders(types.ContractAddress(contractAddr), types.PairString(pair.String()))
		order.Id = nextId
		orders.AddOrder(order)
		nextId += 1
	}
	k.SetNextOrderId(ctx, nextId)
}

func (k *Keeper) getLiquidationSudoMsg(typedContractAddr types.ContractAddress) types.SudoLiquidationMsg {
	cachedLiquidationRequests := k.MemState.GetLiquidationRequests(typedContractAddr)
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
