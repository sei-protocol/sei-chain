package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
)

// MonolithicEVMAnteHandler is a single-function ante handler that performs all EVM
// transaction validation in sequence without the overhead of the decorator chain pattern.
// This eliminates closure allocation, wrapper overhead, and gaps between decorators.
type MonolithicEVMAnteHandler struct {
	handlers []sdk.AnteDecorator
}

func NewMonolithicEVMAnteHandler(handlers []sdk.AnteDecorator) *MonolithicEVMAnteHandler {
	return &MonolithicEVMAnteHandler{
		handlers: handlers,
	}
}

// AnteHandle performs all EVM ante checks in sequence without decorator chain overhead
func (m *MonolithicEVMAnteHandler) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	// Add tracing span for the entire monolithic handler
	if ctx.IsTracing() && ctx.TracingInfo() != nil {
		if tracingInfo, ok := ctx.TracingInfo().(*tracing.Info); ok {
			spanCtx, span := tracingInfo.StartWithContext("MonolithicEVMAnteHandler", ctx.TraceSpanContext())
			ctx = ctx.WithTraceSpanContext(spanCtx)
			defer tracing.CloseSpan(span)
		}
	}

	var err error

	// Iterate through all handlers sequentially
	for _, handler := range m.handlers {
		ctx, err = handler.AnteHandle(ctx, tx, simulate, noopNext)
		if err != nil {
			return ctx, err
		}
	}

	return ctx, nil
}

// noopNext is a no-op ante handler used when we don't want to chain to the next handler
func noopNext(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	return ctx, nil
}
