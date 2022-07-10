package dex

import (
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// TickSizeMultipleDecorator check if the place order tx's price is multiple of
// tick size
type TickSizeMultipleDecorator struct {
	dexKeeper keeper.Keeper
}

// NewTickSizeMultipleDecorator returns new ticksize multiple check decorator instance
func NewTickSizeMultipleDecorator(dexKeeper keeper.Keeper) TickSizeMultipleDecorator {
	return TickSizeMultipleDecorator{
		dexKeeper: dexKeeper,
	}
}

// AnteHandle is the interface called in RunTx() function, which CheckTx() calls with the runTxModeCheck mode
func (tsmd TickSizeMultipleDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if ctx.IsReCheckTx() {
		return next(ctx, tx, simulate)
	}
	if !simulate {
		if ctx.IsCheckTx() {
			err := tsmd.CheckTickSizeMultiple(ctx, tx.GetMsgs())
			if err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}

// CheckTickSizeMultiple checks whether the msgs comply with ticksize
func (tsmd TickSizeMultipleDecorator) CheckTickSizeMultiple(ctx sdk.Context, msgs []sdk.Msg) error {
	for _, msg := range msgs {
		switch msg.(type) {
		case *types.MsgPlaceOrders:
			msgPlaceOrders := msg.(*types.MsgPlaceOrders)
			contractAddr := msgPlaceOrders.ContractAddr
			for _, order := range msgPlaceOrders.Orders {
				tickSize, found := tsmd.dexKeeper.GetTickSizeForPair(ctx, contractAddr,
					types.Pair{PriceDenom: order.PriceDenom,
						AssetDenom: order.AssetDenom})
				fmt.Println(contractAddr)
				// todo may not need to throw err if ticksize unfound?
				if !found {
					return sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no ticksize configured", order.PriceDenom, order.AssetDenom)
				}
				if !IsDecimalMultipleOf(order.Price, tickSize) {
					return sdkerrors.Wrapf(errors.New("ErrPriceNotMultipleOfTickSize"), "price need to be multiple of tick size")
				}
			}
			continue
		default:
			// e.g. liquidation order don't come with price so always pass this check
			return nil
		}
	}

	return nil
}

// Check whether decimal a is multiple of decimal b
func IsDecimalMultipleOf(a, b sdk.Dec) bool {
	if a.LT(b) {
		return false
	}
	quotient := sdk.NewDecFromBigInt(a.Quo(b).RoundInt().BigInt())
	return quotient.Mul(b).Equal(a)
}
