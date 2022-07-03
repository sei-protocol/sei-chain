package dex

import (
	"errors"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// TickSizeMultipleDecorator check if the place order tx's price is multiple of
// tick size
type TickSizeMultipleDecorator struct {
	dexKeeper     keeper.Keeper
}

// NewSpammingPreventionDecorator returns new spamming prevention decorator instance
func NewTickSizeMultipleDecorator(dexKeeper keeper.Keeper) TickSizeMultipleDecorator {
	return TickSizeMultipleDecorator{
		dexKeeper:     dexKeeper,
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

// CheckTickSizeMultiple checks whether the msgs are 
func (tsmd TickSizeMultipleDecorator) CheckTickSizeMultiple(ctx sdk.Context, msgs []sdk.Msg) error {
	for _, msg := range msgs {
		switch msg.(type) {
		case *types.MsgPlaceOrders:
			msgPlaceOrders := msg.(*types.MsgPlaceOrders)
			for _, order := range msgPlaceOrders.Orders {
				tickSize, found := tsmd.dexKeeper.GetTickSizeForPair(ctx, types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom})
				// todo may not need to throw err if ticksize unfound?
				if !found {
					return sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no ticksize configured", order.PriceDenom.String(), order.AssetDenom.String())
				}
				val, err := order.Price.Float64()
				if err != nil {
					// todo put customized error together
					return sdkerrors.Wrapf(errors.New("ErrParsePriceErr"), "fail to parse price of the order")
				}
				if val < float64(tickSize) || math.Mod(val, float64(tickSize)) != 0 {
					return sdkerrors.Wrapf(errors.New("ErrPriceNotMultipleOfTickSize"), "price need to be multiple of tick size")
				}
			}
			continue
		case *types.MsgCancelOrders:
			msgCancelOrders := msg.(*types.MsgCancelOrders)
			for _, orderCancellation := range msgCancelOrders.OrderCancellations {
				tickSize, found := tsmd.dexKeeper.GetTickSizeForPair(ctx, types.Pair{PriceDenom: orderCancellation.PriceDenom, AssetDenom: orderCancellation.AssetDenom})
				// todo may not need to throw err if ticksize unfound?
				if !found {
					return sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no ticksize configured", orderCancellation.PriceDenom.String(), orderCancellation.AssetDenom.String())
				}
				val, err := orderCancellation.Price.Float64()
				if err != nil {
					// todo put customized error together
					return sdkerrors.Wrapf(errors.New("ErrParsePriceErr"), "fail to parse price of the order")
				}
				if val < float64(tickSize) || math.Mod(val, float64(tickSize)) != 0 {
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