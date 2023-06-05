package abci

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	seiutils "github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

func (w KeeperWrapper) HandleEBDeposit(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, message types.SudoOrderPlacementMsg) error {
	_, span := (*tracer).Start(ctx, "SudoDeposit")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))
	defer span.End()

	_, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, message, dexutils.ZeroUserProvidedGas) // deposit
	if err != nil {
		sdkCtx.Logger().Error(fmt.Sprintf("Error during deposit: %s", err.Error()))
		return err
	}

	return nil
}

func (w KeeperWrapper) GetDepositSudoMsg(ctx sdk.Context, typedContractAddr types.ContractAddress) types.SudoOrderPlacementMsg {
	depositMemState := dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, typedContractAddr).Get()

	// If there's no amount to send, exit early and avoid additional processing
	if len(depositMemState) == 0 {
		return types.SudoOrderPlacementMsg{
			OrderPlacements: types.OrderPlacementMsgDetails{
				Orders:   []types.Order{},
				Deposits: []types.ContractDepositInfo{},
			},
		}
	}

	contractDepositInfo := seiutils.Map(
		depositMemState,
		func(d *types.DepositInfoEntry) types.ContractDepositInfo { return d.ToContractDepositInfo() },
	)
	escrowed := sdk.NewCoins()
	for _, deposit := range depositMemState {
		escrowed = escrowed.Add(sdk.NewCoin(deposit.Denom, deposit.Amount.TruncateInt()))
	}
	contractAddr, err := sdk.AccAddressFromBech32(string(typedContractAddr))
	if err != nil {
		panic(err)
	}
	if err := w.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, contractAddr, escrowed); err != nil {
		panic(err)
	}
	return types.SudoOrderPlacementMsg{
		OrderPlacements: types.OrderPlacementMsgDetails{
			Orders:   []types.Order{},
			Deposits: contractDepositInfo,
		},
	}
}
