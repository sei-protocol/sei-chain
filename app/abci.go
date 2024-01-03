package app

import (
	"context"
	"fmt"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"math/big"
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
	app.Logger().Info("CheckTx", "i", 1)
	_, span := app.GetBaseApp().TracingInfo.Start("CheckTx")
	defer span.End()

	app.Logger().Info("CheckTx", "i", 2)

	resp, err := app.GetBaseApp().CheckTx(ctx, req)
	if err != nil {
		app.Logger().Error("CheckTx error", "error", err.Error())
		return resp, err
	}

	app.Logger().Info("CheckTx", "i", 3)

	// For EVM, populate sender on the response so that mempool can decide to dedupe
	tx, err := app.txDecoder(req.Tx)
	if err != nil {
		app.Logger().Info("CheckTx", "i", 4)
		return resp, err
	}

	if err := app.populateEVMSender(tx, resp); err != nil {
		app.Logger().Info("CheckTx", "i", 5, "error", err.Error())
		return resp, err
	}
	app.Logger().Info("CheckTx", "i", 6)
	return resp, nil
}

func (app *App) populateEVMSender(tx sdk.Tx, resp *abci.ResponseCheckTxV2) error {
	// if not evm message, no properties are needed
	if isEVM, err := ante.IsEVMMessage(tx); err != nil || !isEVM {
		fmt.Println("cannot ask whether this tx is evm message")
		return err
	}

	// recover from address
	evmMsg := types.MustGetEVMTransactionMessage(tx)
	evmTx, _ := evmMsg.AsTransaction()
	sdkCtx := app.NewContext(true, tmproto.Header{})
	evmCfg := app.EvmKeeper.GetChainConfig(sdkCtx).EthereumConfig(app.EvmKeeper.ChainID(sdkCtx))
	signer := ethtypes.MakeSigner(
		evmCfg,
		big.NewInt(sdkCtx.BlockHeight()),
		uint64(sdkCtx.BlockTime().Second()),
	)

	fromAddress, err := signer.Sender(evmTx)
	if err != nil {
		fmt.Println("cannot recover address from signature")
		return err
	}

	resp.EVMTxProperties = &abci.EVMTxProperties{
		FromAddressHex: fromAddress.Hex(),
		Nonce:          evmTx.Nonce(),
	}

	// sender is used for de-duping in the mempool (key in a map)
	resp.Sender = resp.EvmKey()
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
