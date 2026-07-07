package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"go.opentelemetry.io/otel/attribute"
	otelmetrics "go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/app/migration"
	"github.com/sei-protocol/sei-chain/sei-cosmos/tasks"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/legacytm"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	utilmetrics "github.com/sei-protocol/sei-chain/utils/metrics"
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
	beginBlockStart := time.Now()
	defer func() {
		telemetry.MeasureSince(beginBlockStart, "abci", "begin_block") // TODO(PLT-327): remove once app_abci_begin_block_duration_seconds verified
		appMetrics.beginBlockDuration.Record(ctx.Context(), time.Since(beginBlockStart).Seconds())
	}()
	// inline begin block
	if checkHeight {
		if err := app.ValidateHeight(height); err != nil {
			panic(err)
		}
	}
	utilmetrics.GaugeSeidVersionAndCommit(app.versionInfo.Version, app.versionInfo.GitCommit) // TODO(PLT-327): remove once app_build_info observable gauge verified
	// check if we've reached a target height, if so, execute any applicable handlers
	if app.forkInitializer != nil {
		app.forkInitializer(ctx)
		app.forkInitializer = nil
	}
	if app.HardForkManager.TargetHeightReached(ctx) {
		app.HardForkManager.ExecuteForTargetHeight(ctx)
	}
	app.applyMigrationBatchSize(ctx)
	legacyabci.BeginBlock(ctx, height, votes, byzantineValidators, app.BeginBlockKeepers)
	return abci.ResponseBeginBlock{
		Events: sdk.MarkEventsToIndex(ctx.EventManager().ABCIEvents(), app.IndexEvents),
	}
}

// applyMigrationBatchSize paces the SC store's background data migration at the network-agreed rate.
// The NumKeysToMigratePerBlock gov param is read from chain state so every node
// applies the same value each block; a per-node rate would diverge the
// AppHash. 0 (the default until a gov proposal raises it) leaves the migration
// paused; it is the sole source of the rate (there is no node-local fallback).
func (app *App) applyMigrationBatchSize(ctx sdk.Context) {
	if app.rootStore == nil {
		return
	}
	numKeys := migration.DefaultNumKeysToMigratePerBlock
	if subspace, ok := app.ParamsKeeper.GetSubspace(migration.SubspaceName); ok {
		// The migration subspace has no owning module to seed it in InitGenesis,
		// so lazily persist the default the first time we see it unset. This is
		// deterministic across nodes (every node runs BeginBlock identically) and
		// makes the param visible to gov: ParameterChangeProposal submission only
		// accepts a change when subspace.Has reports the key is already stored.
		if !subspace.Has(ctx, migration.KeyNumKeysToMigratePerBlock) {
			subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, migration.DefaultNumKeysToMigratePerBlock)
		}
		subspace.GetIfExists(ctx, migration.KeyNumKeysToMigratePerBlock, &numKeys)
	}
	// Defense-in-depth: gov validation already rejects values above
	// MaxNumKeysToMigratePerBlock, but clamp here too so an out-of-range value
	// reaching state via any path can never overflow the int cast or trigger an
	// oversized preallocation in the migration iterator. The clamp is
	// deterministic across nodes.
	if numKeys > migration.MaxNumKeysToMigratePerBlock {
		numKeys = migration.MaxNumKeysToMigratePerBlock
	}
	if err := app.rootStore.SetMigrationBatchSize(int(numKeys)); err != nil {
		// Never panic on the migration-rate update: log and continue. AppHash
		// verification is the safety net. If the rate/mode update fails on only
		// some nodes, those nodes' AppHash diverges and the normal AppHash
		// comparison halts them at the next block — no proactive panic needed.
		// If it fails on every node, all stay in the same (old) mode with an
		// identical AppHash, so the chain keeps moving and the level-triggered
		// trigger re-fires on a later block. Panicking here would needlessly
		// halt the whole chain in that all-fail case.
		logger.Error("failed to set SC migration batch size; continuing", "err", err)
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
	endBlockStart := time.Now()
	defer func() {
		telemetry.MeasureSince(endBlockStart, "abci", "end_block") // TODO(PLT-327): remove once app_abci_end_block_duration_seconds verified
		appMetrics.endBlockDuration.Record(ctx.Context(), time.Since(endBlockStart).Seconds())
	}()
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	moduleEndBlockStart := time.Now()
	defer func() {
		telemetry.MeasureSince(moduleEndBlockStart, "module", "total_end_block") // TODO(PLT-327): remove once app_abci_module_end_block_duration_seconds verified
		appMetrics.moduleEndBlockDuration.Record(ctx.Context(), time.Since(moduleEndBlockStart).Seconds())
	}()
	res.ValidatorUpdates = legacyabci.EndBlock(ctx, height, blockGasUsed, app.EndBlockKeepers)
	res.Events = sdk.MarkEventsToIndex(ctx.EventManager().ABCIEvents(), app.IndexEvents)
	if cp := app.GetConsensusParams(ctx); cp != nil {
		res.ConsensusParamUpdates = legacytm.ABCIToLegacyConsensusParams(cp)
	}
	return res
}

func (app *App) CheckTx(ctx context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	wrapErr := func(err error) *abci.ResponseCheckTxV2 {
		space, code, _ := sdkerrors.ABCIInfo(err, false)
		return &abci.ResponseCheckTxV2{
			ResponseCheckTx: &abci.ResponseCheckTx{
				Codespace: space,
				Code:      code,
				Log:       err.Error(),
			},
		}
	}
	_, span := app.GetBaseApp().TracingInfo.StartWithContext("CheckTx", ctx)
	defer span.End()
	checkTxStart := time.Now()
	defer func() {
		telemetry.MeasureSince(checkTxStart, "abci", "check_tx") // TODO(PLT-327): remove once app_abci_check_tx_duration_seconds verified
		appMetrics.checkTxDuration.Record(ctx, time.Since(checkTxStart).Seconds())
	}()
	sdkCtx := app.GetCheckTxContext(req.Tx, req.Type == abci.CheckTxTypeV2Recheck)
	tx, err := app.txDecoder(req.Tx)
	if err != nil {
		return wrapErr(err)
	}
	checksum := sha256.Sum256(req.Tx)
	gInfo, result, txCtx, err := legacyabci.CheckTx(sdkCtx, tx, app.GetTxConfig(), &app.CheckTxKeepers, checksum, func(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
		return app.CacheTxContext(ctx, checksum)
	}, app.GetCheckCtx, app.TracingInfo)
	if err != nil {
		return wrapErr(err)
	}

	res := &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			GasWanted:    int64(gInfo.GasWanted), //nolint:gosec
			Data:         result.Data,
			Priority:     txCtx.Priority(),
			GasEstimated: int64(gInfo.GasEstimate), //nolint:gosec
		},
		EVMNonce:           txCtx.EVMNonce(),
		EVMHash:            txCtx.EVMTxHash(),
		EVMSenderAddress:   txCtx.EVMSenderAddress(),
		SeiSenderAddress:   txCtx.SeiSenderAddress(),
		IsEVM:              txCtx.IsEVM(),
		EVMRequiredBalance: bigIntToUint256(txCtx.EVMRequiredBalance()),
	}

	return res
}

func (app *App) EvmNonce(evmAddr common.Address) uint64 {
	return app.EvmKeeper.GetNonce(app.GetCheckCtx(), evmAddr)
}

func (app *App) EvmBalance(evmAddr common.Address, seiAddrBz []byte) uint256.Int {
	ctx := app.GetCheckCtx()
	balance := app.EvmKeeper.GetBalance(ctx, evmAddr[:])
	seiAddr := sdk.AccAddress(seiAddrBz)
	if !seiAddr.Equals(sdk.AccAddress(evmAddr[:])) {
		balance = new(big.Int).Add(balance, app.EvmKeeper.GetBalance(ctx, seiAddr))
	}
	// Under the mock_balances build tag, mirror the StateDB auto-top-off on this
	// committed-balance read path so the mempool's readiness gate admits load-test
	// txs from mock-funded accounts (no-op in production builds).
	return bigIntToUint256(mockTopOffBalance(balance))
}

func bigIntToUint256(x *big.Int) uint256.Int {
	if x == nil {
		return uint256.Int{}
	}
	y, overflow := uint256.FromBig(x)
	if overflow {
		panic(fmt.Sprintf("big.Int overflows uint256: %v", x))
	}
	return *y
}

func (app *App) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTxV2, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx {
	deliverTxStart := time.Now()
	// ensure we carry the initial context from tracer here
	spanCtx, span := app.GetBaseApp().TracingInfo.StartWithContext("DeliverTx", ctx.TraceSpanContext())
	defer span.End()
	// update context with trace span new context
	ctx = ctx.WithTraceSpanContext(spanCtx)
	defer func() {
		utilmetrics.MeasureDeliverTxDuration(deliverTxStart)         // TODO(PLT-327): remove once app_abci_deliver_tx_duration_seconds verified
		telemetry.MeasureSince(deliverTxStart, "abci", "deliver_tx") // TODO(PLT-327): remove once app_abci_deliver_tx_duration_seconds verified
		appMetrics.deliverTxDuration.Record(ctx.Context(), time.Since(deliverTxStart).Seconds())
	}()

	gInfo := sdk.GasInfo{}
	resultStr := "successful"

	defer func() {
		telemetry.IncrCounter(1, "tx", "count")                             // TODO(PLT-327): remove once app_tx_count_total verified
		telemetry.IncrCounter(1, "tx", resultStr)                           // TODO(PLT-327): remove once app_tx_count_total verified
		telemetry.SetGauge(float32(gInfo.GasUsed), "tx", "gas", "used")     // TODO(PLT-327): remove once app_tx_gas_used verified
		telemetry.SetGauge(float32(gInfo.GasWanted), "tx", "gas", "wanted") // TODO(PLT-327): remove once app_tx_gas_wanted verified
		appMetrics.txCount.Add(ctx.Context(), 1, otelmetrics.WithAttributes(attribute.String("result", resultStr)))
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
			SenderAddress: resCtx.EVMSenderAddress().Hex(),
			Nonce:         resCtx.EVMNonce(),
			TxHash:        resCtx.EVMTxHash().Hex(),
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
	deliverBatchStart := time.Now()
	defer func() {
		utilmetrics.MeasureDeliverBatchTxDuration(deliverBatchStart) // TODO(PLT-327): remove once app_abci_deliver_batch_tx_duration_seconds verified
		appMetrics.deliverBatchTxDuration.Record(ctx.Context(), time.Since(deliverBatchStart).Seconds())
	}()
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
		logger.Error("error while processing scheduler", "err", err)
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
	elapsed := time.Since(start)
	// legacy: telemetry.MeasureSince in sei-cosmos/baseapp/abci.go TODO(PLT-327)
	appMetrics.commitDuration.Record(ctx, elapsed.Seconds())
	app.RecordBenchmarkCommitTime(elapsed)
	// After a successful Commit, publish the pending eth_newHeads event
	// stashed by FinalizeBlocker. Subscribers see only committed state.
	// Header.AppHash is intentionally left unset (Tendermint convention:
	// it holds the previous block's hash); stateRoot is sourced from
	// response.AppHash by encodeCommittedBlock.
	if err == nil {
		if n, ok := app.blockHeaderNotifier.Get(); ok {
			n.PublishStashed()
		}
	}
	return res, err
}
