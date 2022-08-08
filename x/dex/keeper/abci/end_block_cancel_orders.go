package abci

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (w KeeperWrapper) HandleEBCancelOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) error {
	_, span := (*tracer).Start(ctx, "SudoCancelOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))

	typedContractAddr := typesutils.ContractAddress(contractAddr)
	msg := w.getCancelSudoMsg(sdkCtx, typedContractAddr, registeredPairs)
	if _, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, msg); err != nil {
		sdkCtx.Logger().Error(fmt.Sprintf("Error during cancellation: %s", err.Error()))
		return err
	}
	span.End()
	return nil
}

func (w KeeperWrapper) getCancelSudoMsg(sdkCtx sdk.Context, typedContractAddr typesutils.ContractAddress, registeredPairs []types.Pair) wasm.SudoOrderCancellationMsg {
	idsToCancel := []uint64{}
	for _, pair := range registeredPairs {
		typedPairStr := typesutils.GetPairString(&pair) //nolint:gosec // THIS MAY BE CAUSE FOR CONCERN AND WE MIGHT WANT TO REFACTOR.
		for _, cancel := range w.MemState.GetBlockCancels(sdkCtx, typedContractAddr, typedPairStr).Get() {
			idsToCancel = append(idsToCancel, cancel.Id)
		}
	}
	return wasm.SudoOrderCancellationMsg{
		OrderCancellations: wasm.OrderCancellationMsgDetails{
			IdsToCancel: idsToCancel,
		},
	}
}
