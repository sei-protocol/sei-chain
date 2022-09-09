package antedecorators

import sdk "github.com/cosmos/cosmos-sdk/types"

type WasCheckedKeyType string

const WasCheckedKey WasCheckedKeyType = WasCheckedKeyType("was-checked")

type FastPathDecorator struct {
	wrapped sdk.AnteDecorator
}

func NewFastPathDecorator(wrapped sdk.AnteDecorator) FastPathDecorator {
	return FastPathDecorator{wrapped: wrapped}
}

func (d FastPathDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	wasChecked := ctx.Context().Value(WasCheckedKey)
	if wasChecked != nil {
		if typedWasChecked, ok := wasChecked.(bool); ok && typedWasChecked {
			return next(ctx, tx, simulate)
		}
	}
	return d.wrapped.AnteHandle(ctx, tx, simulate, next)
}
