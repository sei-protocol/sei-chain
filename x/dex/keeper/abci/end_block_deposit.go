package abci

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	seiutils "github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (w KeeperWrapper) HandleEBDeposit(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string) error {
	_, span := (*tracer).Start(ctx, "SudoDeposit")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))
	defer span.End()

	typedContractAddr := typesutils.ContractAddress(contractAddr)
	msg := w.GetDepositSudoMsg(sdkCtx, typedContractAddr)
	_, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, msg, dexutils.ZeroUserProvidedGas) // deposit
	if err != nil {
		sdkCtx.Logger().Error(fmt.Sprintf("Error during deposit: %s", err.Error()))
		return err
	}

	return nil
}

func (w KeeperWrapper) GetDepositSudoMsg(ctx sdk.Context, typedContractAddr typesutils.ContractAddress) wasm.SudoOrderPlacementMsg {
	contractDepositInfo := seiutils.Map(
		dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, typedContractAddr).Get(),
		func(d *types.DepositInfoEntry) types.ContractDepositInfo { return d.ToContractDepositInfo() },
	)
	return wasm.SudoOrderPlacementMsg{
		OrderPlacements: wasm.OrderPlacementMsgDetails{
			Orders:   []types.Order{},
			Deposits: contractDepositInfo,
		},
	}
}
