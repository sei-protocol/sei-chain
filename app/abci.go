package app

import (
	"context"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	abci "github.com/tendermint/tendermint/abci/types"
	"go.opentelemetry.io/otel/attribute"
)

func (app *App) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) (res abci.ResponseBeginBlock) {
	tracectx, topSpan := (*app.tracingInfo.Tracer).Start(context.Background(), "Block")
	topSpan.SetAttributes(attribute.Int64("height", req.Header.Height))
	app.tracingInfo.BlockSpan = &topSpan
	app.tracingInfo.TracerContext = tracectx
	_, beginBlockSpan := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "BeginBlock")
	defer beginBlockSpan.End()
	return app.BaseApp.BeginBlock(ctx, req)
}

func (app *App) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	_, span := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "EndBlock")
	defer span.End()
	return app.BaseApp.EndBlock(ctx, req)
}

func (app *App) CheckTx(ctx context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	_, span := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "CheckTx")
	defer span.End()
	return app.BaseApp.CheckTx(ctx, req)
}

func (app *App) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTx) abci.ResponseDeliverTx {
	defer metrics.MeasureDeliverTxDuration(time.Now())
	return app.BaseApp.DeliverTx(ctx, req)
}

func (app *App) Commit(ctx context.Context) (res *abci.ResponseCommit, err error) {
	if app.tracingInfo.BlockSpan != nil {
		defer (*app.tracingInfo.BlockSpan).End()
	}
	_, span := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "Commit")
	defer span.End()
	app.tracingInfo.TracerContext = context.Background()
	app.tracingInfo.BlockSpan = nil
	return app.BaseApp.Commit(ctx)
}

func (app *App) RunTx(ctx sdk.Context, mode baseapp.RunTxMode, txBytes []byte) (gInfo sdk.GasInfo, result *sdk.Result, anteEvents []abci.Event, priority int64, err error) {
	_, span := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "DeliverTx")
	defer span.End()
	return app.BaseApp.RunTx(ctx, mode, txBytes)
}
