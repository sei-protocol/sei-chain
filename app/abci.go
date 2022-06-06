package app

import (
	"context"

	abci "github.com/tendermint/tendermint/abci/types"
	"go.opentelemetry.io/otel/attribute"
)

func (app *App) BeginBlock(req abci.RequestBeginBlock) (res abci.ResponseBeginBlock) {
	ctx, topSpan := (*app.tracingInfo.Tracer).Start(context.Background(), "Block")
	topSpan.SetAttributes(attribute.Int64("height", req.Header.Height))
	app.tracingInfo.BlockSpan = &topSpan
	app.tracingInfo.TracerContext = ctx
	_, beginBlockSpan := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "BeginBlock")
	defer beginBlockSpan.End()
	return app.BaseApp.BeginBlock(req)
}

func (app *App) EndBlock(req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	_, span := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "BeginBlock")
	defer span.End()
	return app.BaseApp.EndBlock(req)
}

func (app *App) CheckTx(req abci.RequestCheckTx) abci.ResponseCheckTx {
	_, span := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "CheckTx")
	defer span.End()
	return app.BaseApp.CheckTx(req)
}

func (app *App) DeliverTx(req abci.RequestDeliverTx) abci.ResponseDeliverTx {
	ctx, span := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "DeliverTx")
	oldCtx := app.tracingInfo.TracerContext
	app.tracingInfo.TracerContext = ctx
	defer span.End()
	defer func() { app.tracingInfo.TracerContext = oldCtx }()
	return app.BaseApp.DeliverTx(req)
}

func (app *App) Commit() (res abci.ResponseCommit) {
	if app.tracingInfo.BlockSpan != nil {
		defer (*app.tracingInfo.BlockSpan).End()
	}
	_, span := (*app.tracingInfo.Tracer).Start(app.tracingInfo.TracerContext, "Commit")
	defer span.End()
	app.tracingInfo.TracerContext = context.Background()
	app.tracingInfo.BlockSpan = nil
	return app.BaseApp.Commit()
}
