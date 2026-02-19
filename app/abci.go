package app

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/sei-cosmos/tasks"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/legacytm"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/utils/metrics"
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

func (app *App) EndBlock(ctx sdk.Context, height int64, blockGasUsed int64) (res abci.ResponseEndBlock) {
	spanCtx, span := app.GetBaseApp().TracingInfo.StartWithContext("EndBlock", ctx.TraceSpanContext())
	defer span.End()
	ctx = ctx.WithTraceSpanContext(spanCtx)
	defer telemetry.MeasureSince(time.Now(), "abci", "end_block")
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	defer telemetry.MeasureSince(time.Now(), "module", "total_end_block")
	res.ValidatorUpdates = legacyabci.EndBlock(ctx, height, blockGasUsed, app.EndBlockKeepers)
	res.Events = sdk.MarkEventsToIndex(ctx.EventManager().ABCIEvents(), app.IndexEvents)
	if cp := app.GetConsensusParams(ctx); cp != nil {
		res.ConsensusParamUpdates = legacytm.ABCIToLegacyConsensusParams(cp)
	}
	return res
}

func (app *App) CheckTx(ctx context.Context, req *abci.RequestCheckTxV2) (*abci.ResponseCheckTxV2, error) {
	_, span := app.GetBaseApp().TracingInfo.StartWithContext("CheckTx", ctx)
	defer span.End()
	defer telemetry.MeasureSince(time.Now(), "abci", "check_tx")
	sdkCtx := app.GetCheckTxContext(req.Tx, req.Type == abci.CheckTxTypeV2Recheck)
	tx, err := app.txDecoder(req.Tx)
	if err != nil {
		res := sdkerrors.ResponseCheckTx(err, 0, 0, false)
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &res}, err
	}
	checksum := sha256.Sum256(req.Tx)
	gInfo, result, txCtx, err := legacyabci.CheckTx(sdkCtx, tx, app.GetTxConfig(), &app.CheckTxKeepers, checksum, func(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
		return app.CacheTxContext(ctx, checksum)
	}, app.GetCheckCtx, app.TracingInfo)
	if err != nil {
		res := sdkerrors.ResponseCheckTx(err, gInfo.GasWanted, gInfo.GasUsed, false)
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &res}, err
	}

	res := &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			GasWanted:    int64(gInfo.GasWanted), //nolint:gosec
			Data:         result.Data,
			Priority:     txCtx.Priority(),
			GasEstimated: int64(gInfo.GasEstimate), //nolint:gosec
		},
		ExpireTxHandler:  txCtx.ExpireTxHandler(),
		CheckTxCallback:  txCtx.CheckTxCallback(),
		EVMNonce:         txCtx.EVMNonce(),
		EVMSenderAddress: txCtx.EVMSenderAddress(),
		IsEVM:            txCtx.IsEVM(),
		Priority:         txCtx.Priority(),
	}
	if txCtx.PendingTxChecker() != nil {
		res.IsPendingTransaction = true
		res.Checker = txCtx.PendingTxChecker()
	}

	return res, nil
}

func (app *App) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTxV2, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx {
	defer metrics.MeasureDeliverTxDuration(time.Now())
	// ensure we carry the initial context from tracer here
	spanCtx, span := app.GetBaseApp().TracingInfo.StartWithContext("DeliverTx", ctx.TraceSpanContext())
	defer span.End()
	// update context with trace span new context
	ctx = ctx.WithTraceSpanContext(spanCtx)
	defer telemetry.MeasureSince(time.Now(), "abci", "deliver_tx")

	gInfo := sdk.GasInfo{}
	resultStr := "successful"

	defer func() {
		telemetry.IncrCounter(1, "tx", "count")
		telemetry.IncrCounter(1, "tx", resultStr)
		telemetry.SetGauge(float32(gInfo.GasUsed), "tx", "gas", "used")
		telemetry.SetGauge(float32(gInfo.GasWanted), "tx", "gas", "wanted")
	}()
	gInfo, result, anteEvents, resCtx, err := legacyabci.DeliverTx(ctx.WithTxBytes(req.Tx).WithTxSum(checksum), tx, app.GetTxConfig(), &app.DeliverTxKeepers, checksum, func(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
		return app.CacheTxContext(ctx, checksum)
	}, app.RunMsgs, app.TracingInfo, app.AddCosmosEventsToEVMReceiptIfApplicable)
	if err != nil {
		resultStr = "failed"
		// if we have a result, use those events instead of just the anteEvents
		if result != nil {
			return sdkerrors.ResponseDeliverTxWithEvents(err, gInfo.GasWanted, gInfo.GasUsed, sdk.MarkEventsToIndex(result.Events, app.IndexEvents), false)
		}
		return sdkerrors.ResponseDeliverTxWithEvents(err, gInfo.GasWanted, gInfo.GasUsed, sdk.MarkEventsToIndex(anteEvents, app.IndexEvents), false)
	}

	res := abci.ResponseDeliverTx{
		GasWanted: int64(gInfo.GasWanted), //nolint:gosec
		GasUsed:   int64(gInfo.GasUsed),   //nolint:gosec
		Log:       result.Log,
		Data:      result.Data,
		Events:    sdk.MarkEventsToIndex(result.Events, app.IndexEvents),
	}
	if resCtx.IsEVM() {
		res.EvmTxInfo = &abci.EvmTxInfo{
			SenderAddress: resCtx.EVMSenderAddress(),
			Nonce:         resCtx.EVMNonce(),
			TxHash:        resCtx.EVMTxHash(),
			VmError:       result.EvmError,
		}
		// TODO: populate error data for EVM err
		if result.EvmError != "" {
			evmErr := sdkerrors.Wrap(sdkerrors.ErrEVMVMError, result.EvmError)
			res.Codespace, res.Code, res.Log = sdkerrors.ABCIInfo(evmErr, false)
			resultStr = "failed"
			return res
		}
	}
	return res
}

// DeliverTxBatch is not part of the ABCI specification, but this is here for code convention
func (app *App) DeliverTxBatch(ctx sdk.Context, req sdk.DeliverTxBatchRequest) (res sdk.DeliverTxBatchResponse) {
	defer metrics.MeasureDeliverBatchTxDuration(time.Now())
	spanCtx, span := app.GetBaseApp().TracingInfo.StartWithContext("DeliverTxBatch", ctx.TraceSpanContext())
	defer span.End()
	// update context with trace span new context
	ctx = ctx.WithTraceSpanContext(spanCtx)
	responses := make([]*sdk.DeliverTxResult, 0, len(req.TxEntries))

	if len(req.TxEntries) == 0 {
		return sdk.DeliverTxBatchResponse{Results: responses}
	}

	// avoid overhead for empty batches
	scheduler := tasks.NewScheduler(app.ConcurrencyWorkers(), app.TracingInfo, app.DeliverTx)
	txRes, err := scheduler.ProcessAll(ctx, req.TxEntries)
	if err != nil {
		ctx.Logger().Error("error while processing scheduler", "err", err)
		panic(err)
	}
	for _, tx := range txRes {
		responses = append(responses, &sdk.DeliverTxResult{Response: tx})
	}

	return sdk.DeliverTxBatchResponse{Results: responses}
}

func (app *App) Commit(ctx context.Context) (res *abci.ResponseCommit, err error) {
	_, span := app.GetBaseApp().TracingInfo.StartWithContext("Commit", ctx)
	defer span.End()
	start := time.Now()
	res, err = app.BaseApp.Commit(ctx)
	app.RecordBenchmarkCommitTime(time.Since(start))
	return res, err
}
