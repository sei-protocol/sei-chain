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
	json.Unmarshal(data, &response) //nolint:errcheck // ignore error
	sdkCtx.Logger().Info(fmt.Sprintf("Sudo liquidate response data: %s", response))

	liquidatedAccountsActiveOrderIds := []uint64{}
	for _, account := range response.SuccessfulAccounts {
		liquidatedAccountsActiveOrderIds = append(liquidatedAccountsActiveOrderIds, k.GetAccountActiveOrders(sdkCtx, contractAddr, account).Ids...)
	}
	// Clear up all user-initiated order activities in the current block
	for _, pair := range registeredPairs {
		typedPairStr := types.GetPairString(&pair) //nolint:gosec // USING THE POINTER HERE COULD BE BAD LET'S CHECK IT.
		k.MemState.GetBlockCancels(typedContractAddr, typedPairStr).FilterByIds(liquidatedAccountsActiveOrderIds)
		k.MemState.GetBlockOrders(typedContractAddr, typedPairStr).MarkFailedToPlaceByAccounts(response.SuccessfulAccounts)
	}
	// Cancel all outstanding orders of liquidated accounts, as denoted as cancelled via liquidation
	for id, order := range k.GetOrdersByIds(sdkCtx, contractAddr, liquidatedAccountsActiveOrderIds) {
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom}
		typedPairStr := types.GetPairString(&pair)
		k.MemState.GetBlockCancels(typedContractAddr, typedPairStr).AddOrderIDToCancel(id, types.CancellationInitiator_LIQUIDATED)
	}

	// Place liquidation orders
	k.placeLiquidationOrders(sdkCtx, contractAddr, response.LiquidationOrders)

	liquidationSpan.End()
}

func (k *Keeper) placeLiquidationOrders(ctx sdk.Context, contractAddr string, liquidationOrders []types.Order) {
	nextID := k.GetNextOrderID(ctx)
	for _, order := range liquidationOrders {
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom}
		orders := k.MemState.GetBlockOrders(types.ContractAddress(contractAddr), types.PairString(pair.String()))
		order.Id = nextID
		orders.AddOrder(order)
		nextID++
	}
	k.SetNextOrderID(ctx, nextID)
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
