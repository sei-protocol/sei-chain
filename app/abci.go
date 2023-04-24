package app

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	abci "github.com/tendermint/tendermint/abci/types"
	"go.opentelemetry.io/otel/attribute"
)

func (app *App) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) (res abci.ResponseBeginBlock) {
	tracectx, topSpan := app.tracingInfo.Start("Block")
	topSpan.SetAttributes(attribute.Int64("height", req.Header.Height))
	app.tracingInfo.BlockSpan = &topSpan
	app.tracingInfo.SetContext(tracectx)
	_, beginBlockSpan := (*app.tracingInfo.Tracer).Start(app.tracingInfo.GetContext(), "BeginBlock")
	defer beginBlockSpan.End()
	return app.BaseApp.BeginBlock(ctx, req)
}

func (app *App) MidBlock(ctx sdk.Context, height int64) []abci.Event {
	_, span := app.tracingInfo.Start("MidBlock")
	defer span.End()
	return app.BaseApp.MidBlock(ctx, height)
}

func (app *App) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	_, span := app.tracingInfo.Start("EndBlock")
	defer span.End()
	return app.BaseApp.EndBlock(ctx, req)
}

func (app *App) CheckTx(ctx context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	_, span := app.tracingInfo.Start("CheckTx")
	defer span.End()
	return app.BaseApp.CheckTx(ctx, req)
}

func (app *App) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTx) abci.ResponseDeliverTx {
	defer metrics.MeasureDeliverTxDuration(time.Now())
	_, span := app.tracingInfo.Start("DeliverTx")
	defer span.End()
	return app.BaseApp.DeliverTx(ctx, req)
}

func (app *App) Commit(ctx context.Context) (res *abci.ResponseCommit, err error) {
	if app.tracingInfo.BlockSpan != nil {
		defer (*app.tracingInfo.BlockSpan).End()
	}
	_, span := app.tracingInfo.Start("Commit")
	defer span.End()
	app.tracingInfo.SetContext(context.Background())
	app.tracingInfo.BlockSpan = nil
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
