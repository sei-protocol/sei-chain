package abci

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
	_, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, msg, 0) // deposit
	if err != nil {
		sdkCtx.Logger().Error(fmt.Sprintf("Error during deposit: %s", err.Error()))
		return err
	}

	return nil
}

func (w KeeperWrapper) GetDepositSudoMsg(ctx sdk.Context, typedContractAddr typesutils.ContractAddress) wasm.SudoOrderPlacementMsg {
	contractDepositInfo := []types.ContractDepositInfo{}
	for _, depositInfo := range dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, typedContractAddr).Get() {
		fund := sdk.NewCoins(sdk.NewCoin(depositInfo.Denom, depositInfo.Amount.RoundInt()))
		sender, err := sdk.AccAddressFromBech32(depositInfo.Creator)
		if err != nil {
			ctx.Logger().Error("Invalid deposit creator")
		}
		receiver, err := sdk.AccAddressFromBech32(string(typedContractAddr))
		if err != nil {
			ctx.Logger().Error("Invalid deposit contract")
		}
		if err := w.BankKeeper.SendCoins(ctx, sender, receiver, fund); err == nil {
			contractDepositInfo = append(contractDepositInfo, depositInfo.ToContractDepositInfo())
		} else {
			ctx.Logger().Error(err.Error())
		}
	}
	return wasm.SudoOrderPlacementMsg{
		OrderPlacements: wasm.OrderPlacementMsgDetails{
			Orders:   []types.Order{},
			Deposits: contractDepositInfo,
		},
	}
}
