package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers"
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"     // run init()s to register JS tracers
	_ "github.com/ethereum/go-ethereum/eth/tracers/native" // run init()s to register native tracers
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"golang.org/x/sync/errgroup"
)

const (
	IsPanicCacheSize = 5000
	IsPanicCacheTTL  = 1 * time.Minute
)

type DebugAPI struct {
	tracersAPI         *tracers.API
	tmClient           rpcclient.Client
	keeper             *keeper.Keeper
	ctxProvider        func(int64) sdk.Context
	txConfigProvider   func(int64) client.TxConfig
	connectionType     ConnectionType
	backend            *Backend
	isPanicCache       *expirable.LRU[common.Hash, bool] // hash to isPanic
	traceCallSemaphore chan struct{}                     // Semaphore for limiting concurrent trace calls
	maxBlockLookback   int64
	traceTimeout       time.Duration
}

// acquireTraceSemaphore attempts to acquire a slot from the traceCallSemaphore.
// It returns a function that must be called (typically with defer) to release the semaphore.
// If the semaphore is nil (unlimited concurrency), it does nothing and returns a no-op release function.
func (api *DebugAPI) acquireTraceSemaphore() func() {
	if api.traceCallSemaphore != nil {
		api.traceCallSemaphore <- struct{}{}
		return func() { <-api.traceCallSemaphore }
	}
	return func() {} // No-op if semaphore is not active
}

type SeiDebugAPI struct {
	*DebugAPI
}

func NewDebugAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	earliestVersion func() int64,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
	debugCfg Config,
) *DebugAPI {
	backend := NewBackend(ctxProvider, k, txConfigProvider, earliestVersion, tmClient, config, app, antehandler)
	tracersAPI := tracers.NewAPI(backend)
	evictCallback := func(key common.Hash, value bool) {}
	isPanicCache := expirable.NewLRU[common.Hash, bool](IsPanicCacheSize, evictCallback, IsPanicCacheTTL)

	var sem chan struct{}
	if debugCfg.MaxConcurrentTraceCalls > 0 {
		sem = make(chan struct{}, debugCfg.MaxConcurrentTraceCalls)
	}

	return &DebugAPI{
		tracersAPI:         tracersAPI,
		tmClient:           tmClient,
		keeper:             k,
		ctxProvider:        ctxProvider,
		txConfigProvider:   txConfigProvider,
		connectionType:     connectionType,
		isPanicCache:       isPanicCache,
		traceCallSemaphore: sem,
		maxBlockLookback:   debugCfg.MaxTraceLookbackBlocks,
		traceTimeout:       debugCfg.TraceTimeout,
	}
}

func NewSeiDebugAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	earliestVersion func() int64,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
	debugCfg Config,
) *SeiDebugAPI {
	backend := NewBackend(ctxProvider, k, txConfigProvider, earliestVersion, tmClient, config, app, antehandler)
	tracersAPI := tracers.NewAPI(backend)

	var sem chan struct{}
	if debugCfg.MaxConcurrentTraceCalls > 0 {
		sem = make(chan struct{}, debugCfg.MaxConcurrentTraceCalls)
	}
	// Note: The embedded DebugAPI here does not get its own isPanicCache initialized
	// This is consistent with the original code. If it needs one, it should be added.
	embeddedDebugAPI := &DebugAPI{
		tracersAPI:         tracersAPI,
		tmClient:           tmClient,
		keeper:             k,
		ctxProvider:        ctxProvider,
		txConfigProvider:   txConfigProvider,
		connectionType:     connectionType,
		traceCallSemaphore: sem,
		maxBlockLookback:   debugCfg.MaxTraceLookbackBlocks,
		traceTimeout:       debugCfg.TraceTimeout,
		// isPanicCache: nil, // Explicitly nil as per original structure for SeiDebugAPI's embedded DebugAPI
	}

	return &SeiDebugAPI{
		DebugAPI: embeddedDebugAPI,
	}
}

func (api *DebugAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	release := api.acquireTraceSemaphore()
	defer release()

	ctx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	defer cancel()

	startTime := time.Now()
	defer recordMetrics("debug_traceTransaction", api.connectionType, startTime, returnErr == nil)

	sctx := api.ctxProvider(LatestCtxHeight)
	result, returnErr = api.tracersAPI.TraceTransaction(ctx, hash, config)

	if !isErrorableTrace(config) {
		return result, returnErr
	}

	if returnErr == nil {
		if traceResults, ok := result.(*tracers.TxTraceResult); ok && traceResults != nil {
			api.decorateWithErrors(sctx, traceResults)
		}
	}

	return
}

func (api *SeiDebugAPI) TraceBlockByNumberExcludeTraceFail(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	release := api.acquireTraceSemaphore() // Use the embedded DebugAPI's semaphore
	defer release()

	ctx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	defer cancel()

	latest := api.ctxProvider(LatestCtxHeight).BlockHeight()
	if api.maxBlockLookback >= 0 && number.Int64() < latest-api.maxBlockLookback {
		return nil, fmt.Errorf("block number %d is beyond max lookback of %d", number.Int64(), api.maxBlockLookback)
	}

	startTime := time.Now()
	defer recordMetrics("sei_traceBlockByNumberExcludeTraceFail", api.connectionType, startTime, returnErr == nil)
	// Accessing tracersAPI from the embedded DebugAPI
	result, returnErr = api.DebugAPI.tracersAPI.TraceBlockByNumber(ctx, number, config)
	if returnErr != nil {
		return
	}
	traces, ok := result.([]*tracers.TxTraceResult)
	if !ok {
		return nil, fmt.Errorf("unexpected type: %T", result)
	}
	finalTraces := make([]*tracers.TxTraceResult, 0, len(traces))
	for _, trace := range traces {
		if len(trace.Error) > 0 {
			continue
		}
		finalTraces = append(finalTraces, trace)
	}
	return finalTraces, nil
}

func (api *SeiDebugAPI) TraceBlockByHashExcludeTraceFail(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	release := api.acquireTraceSemaphore() // Use the embedded DebugAPI's semaphore
	defer release()

	ctx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	defer cancel()

	startTime := time.Now()
	defer recordMetrics("sei_traceBlockByHashExcludeTraceFail", api.connectionType, startTime, returnErr == nil)
	// Accessing tracersAPI from the embedded DebugAPI
	result, returnErr = api.DebugAPI.tracersAPI.TraceBlockByHash(ctx, hash, config)
	if returnErr != nil {
		return
	}
	traces, ok := result.([]*tracers.TxTraceResult)
	if !ok {
		return nil, fmt.Errorf("unexpected type: %T", result)
	}
	finalTraces := make([]*tracers.TxTraceResult, 0, len(traces))
	for _, trace := range traces {
		if len(trace.Error) > 0 {
			continue
		}
		finalTraces = append(finalTraces, trace)
	}
	return finalTraces, nil
}

// isPanicOrSyntheticTx returns true if the tx is a panic tx or if it is a synthetic tx. Used in the *ExcludeTraceFail endpoints.
// This method itself is not directly rate-limited by the semaphore here, but calls to it might be from a rate-limited method.
// If this method's internal trace call needs to be subject to the *same* semaphore, it would require passing it down or careful structuring.
// For now, we assume the top-level RPC calls are what we're limiting.
func (api *DebugAPI) isPanicOrSyntheticTx(ctx context.Context, hash common.Hash) (isPanic bool, err error) {
	sdkctx := api.ctxProvider(LatestCtxHeight)
	receipt, err := api.keeper.GetReceipt(sdkctx, hash)
	if err != nil {
		return false, err
	}
	height := receipt.BlockNumber

	// Check cache only if it's initialized
	if api.isPanicCache != nil {
		isPanic, ok := api.isPanicCache.Get(hash)
		if ok {
			return isPanic, nil
		}
	}

	callTracer := "callTracer"
	// This internal trace call is not directly acquiring the DebugAPI's semaphore.
	tracersResult, err := api.tracersAPI.TraceBlockByNumber(ctx, rpc.BlockNumber(height), &tracers.TraceConfig{
		Tracer: &callTracer,
	})
	if err != nil {
		return false, err
	}

	found := false
	result := false
	for _, trace := range tracersResult {
		if trace.TxHash == hash {
			found = true
			result = len(trace.Error) > 0
		}
		// for each tx, add to cache to avoid re-tracing, only if cache is initialized
		if api.isPanicCache != nil {
			if len(trace.Error) > 0 {
				api.isPanicCache.Add(trace.TxHash, true)
			} else {
				api.isPanicCache.Add(trace.TxHash, false)
			}
		}
	}

	if !found { // likely a synthetic tx
		return true, nil
	}

	return result, nil
}

// represents whether this trace config can have an error in the reusult
func isErrorableTrace(config *tracers.TraceConfig) bool {
	if config.Tracer == nil {
		return false
	}
	return *config.Tracer == "callTracer" || *config.Tracer == "flatCallTracer"
}

// this is a temporary patch to force errors to appear in the error output for failed receipts.
func (api *DebugAPI) decorateWithErrors(ctx sdk.Context, r *tracers.TxTraceResult) {
	rct, err := api.keeper.GetReceipt(ctx, r.TxHash)
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("debug_traceTransaction: unable to find receipt for hash %s", r.TxHash.Hex()))
		return
	}

	if uint64(rct.Status) == types.ReceiptStatusSuccessful {
		return
	}

	// Check if we need to set an error - only if no error fields are currently set
	hasError := r.Error != ""

	// Check if the result contains trace entries and look for existing errors
	if !hasError && r.Result != nil {
		var resultsArray []interface{}

		// Handle json.RawMessage by unmarshaling it first
		if rawMsg, ok := r.Result.(json.RawMessage); ok {
			if err := json.Unmarshal(rawMsg, &resultsArray); err != nil {
				return
			}
		} else if directArray, ok := r.Result.([]interface{}); ok {
			// Handle case where it's already unmarshaled
			resultsArray = directArray
		} else {
			return
		}

		if len(resultsArray) > 0 {
			// Check if any entry has an error field set
			for _, entry := range resultsArray {
				if entryMap, ok := entry.(map[string]interface{}); ok {
					if errorField, exists := entryMap["error"]; exists && errorField != nil && errorField != "" {
						hasError = true
						break
					}
				}
			}

			// if already has an error, no need to inject one
			if hasError {
				return
			}

			// set an error on the last entry
			lastEntry := resultsArray[len(resultsArray)-1]
			if lastEntryMap, ok := lastEntry.(map[string]interface{}); ok {
				ctx.Logger().With("tx", r.TxHash.Hex()).Info("decorateWithErrors: injecting error in call")
				lastEntryMap["error"] = "Failed"

				// Marshal the modified array back to json.RawMessage
				if modifiedJSON, err := json.Marshal(resultsArray); err == nil {
					r.Result = json.RawMessage(modifiedJSON)
				}
			}
		}

		// Fallback: if we couldn't process the result structure, set error on the TxTraceResult itself
		if r.Result == nil {
			ctx.Logger().Info("decorateWithErrors: r.Result is nil, setting r.Error")
			r.Error = "transaction failed"
		}
	}
}

func (api *DebugAPI) decorateAllWithErrors(sctx sdk.Context, results []*tracers.TxTraceResult) {
	errgrp, _ := errgroup.WithContext(sctx.Context())
	for _, r := range results {
		r := r // Capture loop variable
		errgrp.Go(func() error {
			api.decorateWithErrors(sctx, r)
			return nil
		})
	}
	if err := errgrp.Wait(); err != nil {
		sctx.Logger().Error("should be impossible to reach this")
	}
}

func (api *DebugAPI) TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	release := api.acquireTraceSemaphore()
	defer release()

	ctx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	defer cancel()

	sctx := api.ctxProvider(LatestCtxHeight)
	latest := sctx.BlockHeight()
	if api.maxBlockLookback >= 0 && number.Int64() < latest-api.maxBlockLookback {
		return nil, fmt.Errorf("block number %d is beyond max lookback of %d", number.Int64(), api.maxBlockLookback)
	}

	startTime := time.Now()
	defer recordMetrics("debug_traceBlockByNumber", api.connectionType, startTime, returnErr == nil)
	result, returnErr = api.tracersAPI.TraceBlockByNumber(ctx, number, config)

	if !isErrorableTrace(config) {
		return result, returnErr
	}

	if returnErr == nil {
		if traceResults, ok := result.([]*tracers.TxTraceResult); ok && traceResults != nil {
			api.decorateAllWithErrors(sctx, traceResults)
		}
	}
	return result, returnErr
}

func (api *DebugAPI) TraceBlockByHash(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	release := api.acquireTraceSemaphore()
	defer release()

	ctx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	defer cancel()

	sctx := api.ctxProvider(LatestCtxHeight)
	startTime := time.Now()
	defer recordMetrics("debug_traceBlockByHash", api.connectionType, startTime, returnErr == nil)
	result, returnErr = api.tracersAPI.TraceBlockByHash(ctx, hash, config)

	if !isErrorableTrace(config) {
		return result, returnErr
	}

	if returnErr == nil {
		if traceResults, ok := result.([]*tracers.TxTraceResult); ok && traceResults != nil {
			api.decorateAllWithErrors(sctx, traceResults)
		}
	}
	return result, returnErr
}

func (api *DebugAPI) TraceCall(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, config *tracers.TraceCallConfig) (result interface{}, returnErr error) {
	release := api.acquireTraceSemaphore()
	defer release()

	ctx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	defer cancel()

	startTime := time.Now()
	defer recordMetrics("debug_traceCall", api.connectionType, startTime, returnErr == nil)
	result, returnErr = api.tracersAPI.TraceCall(ctx, args, blockNrOrHash, config)
	return
}

type StateAccessResponse struct {
	AppState        json.RawMessage `json:"app"`
	TendermintState json.RawMessage `json:"tendermint"`
	Receipt         json.RawMessage `json:"receipt"`
}

func (api *DebugAPI) TraceStateAccess(ctx context.Context, hash common.Hash) (result interface{}, returnErr error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			debug.PrintStack()
			returnErr = fmt.Errorf("panic occurred: %v, could not trace tx state: %s", r, hash.Hex())
		}
	}()
	tendermintTraces := &TendermintTraces{Traces: []TendermintTrace{}}
	ctx = WithTendermintTraces(ctx, tendermintTraces)
	receiptTraces := &ReceiptTraces{Traces: []RawResponseReceipt{}}
	ctx = WithReceiptTraces(ctx, receiptTraces)
	tx, blockHash, blockNumber, index, err := api.backend.GetTransaction(ctx, hash)
	if err != nil {
		return nil, err
	}
	// Only mined txes are supported
	if tx == nil {
		return nil, errors.New("transaction not found")
	}
	// It shouldn't happen in practice.
	if blockNumber == 0 {
		return nil, errors.New("genesis is not traceable")
	}
	block, _, err := api.backend.BlockByHash(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	stateDB, _, err := api.backend.ReplayTransactionTillIndex(ctx, block, int(index))
	if err != nil {
		return nil, err
	}
	response := StateAccessResponse{
		AppState:        stateDB.(*state.DBImpl).Ctx().StoreTracer().DerivePrestateToJson(),
		TendermintState: tendermintTraces.MustMarshalToJson(),
		Receipt:         receiptTraces.MustMarshalToJson(),
	}
	return response, nil
}
