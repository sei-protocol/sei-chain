package antedecorators

import (
	"fmt"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("app", "antedecorators")

type GaslessDecorator struct {
	wrapped      []sdk.AnteDecorator
	oracleKeeper oraclekeeper.Keeper
	evmKeeper    *evmkeeper.Keeper
}

func NewGaslessDecorator(wrapped []sdk.AnteDecorator, oracleKeeper oraclekeeper.Keeper, evmKeeper *evmkeeper.Keeper) GaslessDecorator {
	return GaslessDecorator{wrapped: wrapped, oracleKeeper: oracleKeeper, evmKeeper: evmKeeper}
}

func (gd GaslessDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	originalGasMeter := ctx.GasMeter()
	// eagerly set infinite gas meter so that queries performed by IsTxGasless will not incur gas cost
	ctx = ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter())

	isGasless, err := IsTxGasless(tx, ctx, gd.oracleKeeper, gd.evmKeeper)
	if err != nil {
		return ctx, err
	}
	if !isGasless {
		ctx = ctx.WithGasMeter(originalGasMeter)
	}
	isDeliverTx := !ctx.IsCheckTx() && !ctx.IsReCheckTx() && !simulate
	if isDeliverTx || !isGasless {
		// In the case of deliverTx, we want to deduct fees regardless of whether the tx is considered gasless or not, since
		// gasless txs will be subject to application-specific fee requirements in later stage of ante, for which the payment
		// of those app-specific fees happens here. Note that the minimum fee check in the wrapped deduct fee handler is only
		// performed if the context is for CheckTx, so the check will be skipped for deliverTx and the deduct fee handler will
		// only deduct fee without checking.
		// Otherwise (i.e. in the case of checkTx), we only want to perform fee checks and fee deduction if the tx is not considered
		// gasless, or if it specifies a non-zero gas limit even if it is considered gasless, so that the wrapped deduct fee
		// handler will assign an appropriate priority to it.
		return gd.handleWrapped(ctx, tx, simulate, next)
	}

	return next(ctx, tx, simulate)
}

func (gd GaslessDecorator) handleWrapped(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// AnteHandle always takes a `next` so we need a no-op to execute only one handler at a time
	terminatorHandler := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}
	// iterating instead of recursing the handler for readability
	for _, handler := range gd.wrapped {
		newCtx, err := handler.AnteHandle(ctx, tx, simulate, terminatorHandler)
		if err != nil {
			return newCtx, err
		}
		// We need to replace with the new context returned by the handler otherwise we could be losing data
		ctx = newCtx
	}
	return next(ctx, tx, simulate)
}

func IsTxGasless(tx sdk.Tx, ctx sdk.Context, _ oraclekeeper.Keeper, evmKeeper *evmkeeper.Keeper) (isGasless bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("panic recovered in IsTxGasless", "err", r)
			err = fmt.Errorf("panic in IsTxGasless: %v", r)
			isGasless = false
		}
	}()

	if !evmtypes.IsTxMsgAssociate(tx) {
		return false, nil
	}
	if !evmAssociateIsGasless(tx.GetMsgs()[0].(*evmtypes.MsgAssociate), ctx, evmKeeper) {
		return false, nil
	}
	return true, nil
}

func evmAssociateIsGasless(msg *evmtypes.MsgAssociate, ctx sdk.Context, keeper *evmkeeper.Keeper) bool {
	// not gasless if already associated
	seiAddr := sdk.MustAccAddressFromBech32(msg.Sender)
	_, associated := keeper.GetEVMAddress(ctx, seiAddr)
	return !associated
}
