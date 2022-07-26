package abci

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (w KeeperWrapper) HandleEBLiquidation(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) error {
	_, liquidationSpan := (*tracer).Start(ctx, "SudoLiquidation")
	liquidationSpan.SetAttributes(attribute.String("contractAddr", contractAddr))

	typedContractAddr := typesutils.ContractAddress(contractAddr)
	msg := w.getLiquidationSudoMsg(typedContractAddr)
	data, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, msg)
	if err != nil {
		return err
	}
	response := wasm.SudoLiquidationResponse{}
	if err := json.Unmarshal(data, &response); err != nil {
		sdkCtx.Logger().Error("Failed to parse liquidation response")
		return err
	}
	sdkCtx.Logger().Info(fmt.Sprintf("Sudo liquidate response data: %s", response))

	liquidatedAccountsActiveOrderIds := []uint64{}
	for _, account := range response.SuccessfulAccounts {
		liquidatedAccountsActiveOrderIds = append(liquidatedAccountsActiveOrderIds, w.GetAccountActiveOrders(sdkCtx, contractAddr, account).Ids...)
	}
	// Clear up all user-initiated order activities in the current block
	for _, pair := range registeredPairs {
		typedPairStr := typesutils.GetPairString(&pair) //nolint:gosec // USING THE POINTER HERE COULD BE BAD LET'S CHECK IT.
		w.MemState.GetBlockCancels(typedContractAddr, typedPairStr).FilterByIds(liquidatedAccountsActiveOrderIds)
		w.MemState.GetBlockOrders(typedContractAddr, typedPairStr).MarkFailedToPlaceByAccounts(response.SuccessfulAccounts)
	}
	// Cancel all outstanding orders of liquidated accounts, as denoted as cancelled via liquidation
	for id, order := range w.GetOrdersByIds(sdkCtx, contractAddr, liquidatedAccountsActiveOrderIds) {
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom}
		typedPairStr := typesutils.GetPairString(&pair)
		w.MemState.GetBlockCancels(typedContractAddr, typedPairStr).AddCancel(types.Cancellation{
			Id:        id,
			Initiator: types.CancellationInitiator_LIQUIDATED,
		})
	}

	// Place liquidation orders
	w.PlaceLiquidationOrders(sdkCtx, contractAddr, response.LiquidationOrders)

	liquidationSpan.End()
	return nil
}

func (w KeeperWrapper) PlaceLiquidationOrders(ctx sdk.Context, contractAddr string, liquidationOrders []types.Order) {
	ctx.Logger().Info("Placing liquidation orders...")
	nextID := w.GetNextOrderID(ctx)
	for _, order := range liquidationOrders {
		ctx.Logger().Info(fmt.Sprintf("Liquidation order %s", order.String()))
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom}
		orders := w.MemState.GetBlockOrders(typesutils.ContractAddress(contractAddr), typesutils.GetPairString(&pair))
		order.Id = nextID
		orders.AddOrder(order)
		nextID++
	}
	w.SetNextOrderID(ctx, nextID)
}

func (w KeeperWrapper) getLiquidationSudoMsg(typedContractAddr typesutils.ContractAddress) wasm.SudoLiquidationMsg {
	cachedLiquidationRequests := w.MemState.GetLiquidationRequests(typedContractAddr)
	liquidationRequests := []wasm.LiquidationRequest{}
	for _, cachedLiquidationRequest := range *cachedLiquidationRequests {
		liquidationRequests = append(liquidationRequests, wasm.LiquidationRequest{
			Requestor: cachedLiquidationRequest.Requestor,
			Account:   cachedLiquidationRequest.AccountToLiquidate,
		})
	}
	return wasm.SudoLiquidationMsg{
		Liquidation: wasm.ContractLiquidationDetails{
			Requests: liquidationRequests,
		},
	}
}
