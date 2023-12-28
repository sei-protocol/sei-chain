package app

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"go.opentelemetry.io/otel/attribute"
)

func (app *App) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) (res abci.ResponseBeginBlock) {
	tracectx, topSpan := app.GetBaseApp().TracingInfo.Start("Block")
	topSpan.SetAttributes(attribute.Int64("height", req.Header.Height))
	app.GetBaseApp().TracingInfo.BlockSpan = &topSpan
	app.GetBaseApp().TracingInfo.SetContext(tracectx)
	_, beginBlockSpan := (*app.GetBaseApp().TracingInfo.Tracer).Start(app.GetBaseApp().TracingInfo.GetContext(), "BeginBlock")
	defer beginBlockSpan.End()
	return app.BaseApp.BeginBlock(ctx, req)
}

func (app *App) MidBlock(ctx sdk.Context, height int64) []abci.Event {
	_, span := app.GetBaseApp().TracingInfo.Start("MidBlock")
	defer span.End()
	return app.BaseApp.MidBlock(ctx, height)
}

func (app *App) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	_, span := app.GetBaseApp().TracingInfo.Start("EndBlock")
	defer span.End()
	return app.BaseApp.EndBlock(ctx, req)
}

func (app *App) CheckTx(ctx context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTxV2, error) {
	_, span := app.GetBaseApp().TracingInfo.Start("CheckTx")
	defer span.End()
	resp, err := app.BaseApp.CheckTx(ctx, req)
	if err != nil {
		return resp, err
	}

	// For EVM, populate sender on the response so that mempool can decide to dedupe
	tx, err := app.txDecoder(req.Tx)
	if err != nil {
		return nil, err
	}

	if err := populateEVMSender(tx, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func populateEVMSender(tx sdk.Tx, resp *abci.ResponseCheckTxV2) error {
	isEVM, err := ante.IsEVMMessage(tx)
	if err != nil || !isEVM {
		return err
	}

	evmMsg := types.MustGetEVMTransactionMessage(tx)
	evmTx, _ := evmMsg.AsTransaction()
	fromAddress, err := types.RecoverAddressFromSignature(evmTx)
	if err != nil {
		return err
	}
	// sender is used for de-duping in the mempool (key in a map)
	resp.Sender = fmt.Sprintf("%s|%d", fromAddress.Hex(), evmTx.Nonce())
	return nil
}

func (app *App) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx {
	defer metrics.MeasureDeliverTxDuration(time.Now())
	// ensure we carry the initial context from tracer here
	ctx = ctx.WithTraceSpanContext(app.GetBaseApp().TracingInfo.GetContext())
	spanCtx, span := app.GetBaseApp().TracingInfo.StartWithContext("DeliverTx", ctx.TraceSpanContext())
	defer span.End()
	// update context with trace span new context
	ctx = ctx.WithTraceSpanContext(spanCtx)
	return app.BaseApp.DeliverTx(ctx, req, tx, checksum)
}

func (app *App) Commit(ctx context.Context) (res *abci.ResponseCommit, err error) {
	if app.GetBaseApp().TracingInfo.BlockSpan != nil {
		defer (*app.GetBaseApp().TracingInfo.BlockSpan).End()
	}
	_, span := app.GetBaseApp().TracingInfo.Start("Commit")
	defer span.End()
	app.GetBaseApp().TracingInfo.SetContext(context.Background())
	app.GetBaseApp().TracingInfo.BlockSpan = nil
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
