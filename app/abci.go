package app

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	abci "github.com/tendermint/tendermint/abci/types"
)

func (app *App) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) (res abci.ResponseBeginBlock) {
	spanCtx, beginBlockSpan := app.GetBaseApp().TracingInfo.Start("BeginBlock")
	defer beginBlockSpan.End()
	ctx = ctx.WithTraceSpanContext(spanCtx)
	return app.BaseApp.BeginBlock(ctx, req)
}

func (app *App) MidBlock(ctx sdk.Context, height int64) []abci.Event {
	_, span := app.GetBaseApp().TracingInfo.Start("MidBlock")
	defer span.End()
	return app.BaseApp.MidBlock(ctx, height)
}

func (app *App) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	spanCtx, span := app.GetBaseApp().TracingInfo.Start("EndBlock")
	defer span.End()
	ctx = ctx.WithTraceSpanContext(spanCtx)
	return app.BaseApp.EndBlock(ctx, req)
}

func (app *App) CheckTx(ctx context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTxV2, error) {
	_, span := app.GetBaseApp().TracingInfo.Start("CheckTx")
	defer span.End()
	return app.BaseApp.CheckTx(ctx, req)
}

func (app *App) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx {
	defer metrics.MeasureDeliverTxDuration(time.Now())
	// ensure we carry the initial context from tracer here
	spanCtx, span := app.GetBaseApp().TracingInfo.Start("DeliverTx")
	defer span.End()
	// update context with trace span new context
	ctx = ctx.WithTraceSpanContext(spanCtx)
	return app.BaseApp.DeliverTx(ctx, req, tx, checksum)
}

// DeliverTxBatch is not part of the ABCI specification, but this is here for code convention
func (app *App) DeliverTxBatch(ctx sdk.Context, req sdk.DeliverTxBatchRequest) (res sdk.DeliverTxBatchResponse) {
	defer metrics.MeasureDeliverBatchTxDuration(time.Now())
	spanCtx, span := app.GetBaseApp().TracingInfo.Start("DeliverTxBatch")
	defer span.End()
	// update context with trace span new context
	ctx = ctx.WithTraceSpanContext(spanCtx)
	return app.BaseApp.DeliverTxBatch(ctx, req)
}

func (app *App) Commit(ctx context.Context) (res *abci.ResponseCommit, err error) {
	_, span := app.GetBaseApp().TracingInfo.Start("Commit")
	defer span.End()
	return app.BaseApp.Commit(ctx)
}

func (app *App) LoadLatest(ctx context.Context, req *abci.RequestLoadLatest) (*abci.ResponseLoadLatest, error) {
	err := app.ReloadDB()
	if err != nil {
		return nil, err
	}
	app.mounter()
	return app.BaseApp.LoadLatest(ctx, req)
}
