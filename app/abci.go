package app

import (
	"context"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	abci "github.com/tendermint/tendermint/abci/types"
)

func (app *App) BeginBlock(
	ctx sdk.Context,
	height int64,
	votes []abci.VoteInfo,
	byzantineValidators []abci.Misbehavior,
	checkHeight bool,
) (res abci.ResponseBeginBlock) {
	spanCtx, beginBlockSpan := app.GetBaseApp().TracingInfo.StartWithContext("BeginBlock", ctx.TraceSpanContext())
	defer beginBlockSpan.End()
	ctx = ctx.WithTraceSpanContext(spanCtx)
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	defer telemetry.MeasureSince(time.Now(), "abci", "begin_block")
	// inline begin block
	if checkHeight {
		if err := app.ValidateHeight(height); err != nil {
			panic(err)
		}
	}
	metrics.GaugeSeidVersionAndCommit(app.versionInfo.Version, app.versionInfo.GitCommit)
	// check if we've reached a target height, if so, execute any applicable handlers
	if app.forkInitializer != nil {
		app.forkInitializer(ctx)
		app.forkInitializer = nil
	}
	if app.HardForkManager.TargetHeightReached(ctx) {
		app.HardForkManager.ExecuteForTargetHeight(ctx)
	}
	legacyabci.BeginBlock(ctx, height, votes, byzantineValidators, app.BeginBlockKeepers)
	return abci.ResponseBeginBlock{
		Events: sdk.MarkEventsToIndex(ctx.EventManager().ABCIEvents(), app.IndexEvents),
	}
}

func (app *App) MidBlock(ctx sdk.Context, height int64) []abci.Event {
	_, span := app.GetBaseApp().TracingInfo.StartWithContext("MidBlock", ctx.TraceSpanContext())
	defer span.End()
	return app.BaseApp.MidBlock(ctx, height)
}

func (app *App) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	spanCtx, span := app.GetBaseApp().TracingInfo.StartWithContext("EndBlock", ctx.TraceSpanContext())
	defer span.End()
	ctx = ctx.WithTraceSpanContext(spanCtx)
	return app.BaseApp.EndBlock(ctx, req)
}

func (app *App) CheckTx(ctx context.Context, req *abci.RequestCheckTxV2) (*abci.ResponseCheckTxV2, error) {
	_, span := app.GetBaseApp().TracingInfo.StartWithContext("CheckTx", ctx)
	defer span.End()
	return app.BaseApp.CheckTx(ctx, req)
}

func (app *App) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTxV2, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx {
	defer metrics.MeasureDeliverTxDuration(time.Now())
	// ensure we carry the initial context from tracer here
	spanCtx, span := app.GetBaseApp().TracingInfo.StartWithContext("DeliverTx", ctx.TraceSpanContext())
	defer span.End()
	// update context with trace span new context
	ctx = ctx.WithTraceSpanContext(spanCtx)
	return app.BaseApp.DeliverTx(ctx, req, tx, checksum)
}

// DeliverTxBatch is not part of the ABCI specification, but this is here for code convention
func (app *App) DeliverTxBatch(ctx sdk.Context, req sdk.DeliverTxBatchRequest) (res sdk.DeliverTxBatchResponse) {
	defer metrics.MeasureDeliverBatchTxDuration(time.Now())
	spanCtx, span := app.GetBaseApp().TracingInfo.StartWithContext("DeliverTxBatch", ctx.TraceSpanContext())
	defer span.End()
	// update context with trace span new context
	ctx = ctx.WithTraceSpanContext(spanCtx)
	return app.BaseApp.DeliverTxBatch(ctx, req)
}

func (app *App) Commit(ctx context.Context) (res *abci.ResponseCommit, err error) {
	_, span := app.GetBaseApp().TracingInfo.StartWithContext("Commit", ctx)
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
