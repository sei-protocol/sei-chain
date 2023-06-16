package abci

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (w KeeperWrapper) HandleEBCancelOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) error {
	_, span := (*tracer).Start(ctx, "SudoCancelOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))

	typedContractAddr := types.ContractAddress(contractAddr)
	msg := w.getCancelSudoMsg(sdkCtx, typedContractAddr, registeredPairs)
	if len(msg.OrderCancellations.IdsToCancel) == 0 {
		return nil
	}
	userProvidedGas := w.GetParams(sdkCtx).DefaultGasPerCancel * uint64(len(msg.OrderCancellations.IdsToCancel))
	if _, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, msg, userProvidedGas); err != nil {
		sdkCtx.Logger().Error(fmt.Sprintf("Error during cancellation: %s", err.Error()))
		return err
	}
	span.End()
	return nil
}

func (w KeeperWrapper) getCancelSudoMsg(sdkCtx sdk.Context, typedContractAddr types.ContractAddress, registeredPairs []types.Pair) types.SudoOrderCancellationMsg {
	idsToCancel := []uint64{}
	for _, pair := range registeredPairs {
		for _, cancel := range dexutils.GetMemState(sdkCtx.Context()).GetBlockCancels(sdkCtx, typedContractAddr, pair).Get() {
			idsToCancel = append(idsToCancel, cancel.Id)
		}
	}
	return types.SudoOrderCancellationMsg{
		OrderCancellations: types.OrderCancellationMsgDetails{
			IdsToCancel: idsToCancel,
		},
	}
}
