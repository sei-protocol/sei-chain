package dex

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// TODO: migrate this into params state
var WHITELISTED_GASLESS_CANCELLATION_ADDRS = []sdk.AccAddress{}

type GaslessDecoratorWrapper struct {
	wrapped sdk.AnteDecorator
}

func NewGaslessDecoratorWrapper(wrapped sdk.AnteDecorator) GaslessDecoratorWrapper {
	return GaslessDecoratorWrapper{wrapped: wrapped}
}

func (gd GaslessDecoratorWrapper) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if !isTxGasless(tx) {
		return gd.wrapped.AnteHandle(ctx, tx, simulate, next)
	}

	// skip the wrapped if gasless
	return next(ctx, tx, simulate)
}

type GaslessDecorator struct{}

func NewGaslessDecorator() GaslessDecorator {
	return GaslessDecorator{}
}

func (gd GaslessDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if !isTxGasless(tx) {
		return next(ctx, tx, simulate)
	}
	gaslessMeter := sdk.NewInfiniteGasMeter()

	return next(ctx.WithGasMeter(gaslessMeter), tx, simulate)
}

func isTxGasless(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *types.MsgPlaceOrders:
			continue
		case *types.MsgCancelOrders:
			if allSignersWhitelisted(msg) {
				continue
			} else {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func allSignersWhitelisted(msg sdk.Msg) bool {
	for _, signer := range msg.GetSigners() {
		isWhitelisted := false
		for _, whitelisted := range WHITELISTED_GASLESS_CANCELLATION_ADDRS {
			if bytes.Compare(signer, whitelisted) == 0 {
				isWhitelisted = true
				break
			}
		}
		if !isWhitelisted {
			return false
		}
	}
	return true
}
