package antedecorators

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/tracing"
)

type TracedAnteDecorator struct {
	wrapped sdk.AnteDecorator

	traceName   string
	tracingInfo *tracing.Info
}

func NewTracedAnteDecorator(wrapped sdk.AnteDecorator, tracingInfo *tracing.Info) TracedAnteDecorator {
	return TracedAnteDecorator{wrapped: wrapped, traceName: fmt.Sprintf("%T", wrapped), tracingInfo: tracingInfo}
}

func (d TracedAnteDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if d.tracingInfo != nil {
		if ctx.TxBytes()[0]%100 == 0 {
			_, span := (*d.tracingInfo.Tracer).Start(d.tracingInfo.TracerContext, d.traceName)
			defer span.End()
		}
	}
	return d.wrapped.AnteHandle(ctx, tx, simulate, next)
}
