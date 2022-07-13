package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (k *Keeper) HandleEBCancelOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) error {
	_, span := (*tracer).Start(ctx, "SudoCancelOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))

	typedContractAddr := types.ContractAddress(contractAddr)
	msg := k.getCancelSudoMsg(typedContractAddr, registeredPairs)
	if _, err := k.CallContractSudo(sdkCtx, contractAddr, msg); err != nil {
		sdkCtx.Logger().Error(fmt.Sprintf("Error during cancellation: %s", err.Error()))
		return err
	}
	span.End()
	return nil
}

func (k *Keeper) getCancelSudoMsg(typedContractAddr types.ContractAddress, registeredPairs []types.Pair) types.SudoOrderCancellationMsg {
	idsToCancel := []uint64{}
	for _, pair := range registeredPairs {
		typedPairStr := types.GetPairString(&pair) //nolint:gosec // THIS MAY BE CAUSE FOR CONCERN AND WE MIGHT WANT TO REFACTOR.
		for _, cancel := range *k.MemState.GetBlockCancels(typedContractAddr, typedPairStr) {
			idsToCancel = append(idsToCancel, cancel.Id)
		}
	}
	return types.SudoOrderCancellationMsg{
		OrderCancellations: types.OrderCancellationMsgDetails{
			IdsToCancel: idsToCancel,
		},
	}
}
