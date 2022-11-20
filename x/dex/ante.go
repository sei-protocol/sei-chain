package dex

import (
	"errors"

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
		switch msg.(type) { //nolint:gocritic,gosimple // the linter is telling us we can make this faster, and this should be addressed later.
		case *types.MsgPlaceOrders:
			msgPlaceOrders := msg.(*types.MsgPlaceOrders) //nolint:gosimple // the linter is telling us we can make this faster, and this should be addressed later.
			contractAddr := msgPlaceOrders.ContractAddr
			for _, order := range msgPlaceOrders.Orders {
				priceTickSize, found := tsmd.dexKeeper.GetPriceTickSizeForPair(ctx, contractAddr,
					types.Pair{
						PriceDenom: order.PriceDenom,
						AssetDenom: order.AssetDenom,
					})
				// todo may not need to throw err if ticksize unfound?
				if !found {
					return sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no price ticksize configured", order.PriceDenom, order.AssetDenom)
				}
				if !IsDecimalMultipleOf(order.Price, priceTickSize) {
					return sdkerrors.Wrapf(errors.New("ErrPriceNotMultipleOfTickSize"), "price need to be multiple of tick size")
				}
				quantityTickSize, found := tsmd.dexKeeper.GetQuantityTickSizeForPair(ctx, contractAddr,
					types.Pair{
						PriceDenom: order.PriceDenom,
						AssetDenom: order.AssetDenom,
					})
				// todo may not need to throw err if ticksize unfound?
				if !found {
					return sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no quantity ticksize configured", order.PriceDenom, order.AssetDenom)
				}
				if !IsDecimalMultipleOf(order.Quantity, quantityTickSize) {
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
