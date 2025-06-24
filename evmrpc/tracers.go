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
	"github.com/ethereum/go-ethereum/eth/tracers"
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"     // run init()s to register JS tracers
	_ "github.com/ethereum/go-ethereum/eth/tracers/native" // run init()s to register native tracers
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
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
	txDecoder          sdk.TxDecoder
	connectionType     ConnectionType
	isPanicCache       *expirable.LRU[common.Hash, bool] // hash to isPanic
	backend            *Backend
	traceCallSemaphore chan struct{} // Semaphore for limiting concurrent trace calls
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
	txConfig client.TxConfig,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
	debugCfg Config,
) *DebugAPI {
	backend := NewBackend(ctxProvider, k, txConfig, tmClient, config, app, antehandler)
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
		txDecoder:          txConfig.TxDecoder(),
		connectionType:     connectionType,
		isPanicCache:       isPanicCache,
		backend:            backend,
		traceCallSemaphore: sem,
		maxBlockLookback:   debugCfg.MaxTraceLookbackBlocks,
		traceTimeout:       debugCfg.TraceTimeout,
	}
}

func NewSeiDebugAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfig client.TxConfig,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
	debugCfg Config,
) *SeiDebugAPI {
	backend := NewBackend(ctxProvider, k, txConfig, tmClient, config, app, antehandler)
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
		txDecoder:          txConfig.TxDecoder(),
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
	result, returnErr = api.tracersAPI.TraceTransaction(ctx, hash, config)
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

func (api *DebugAPI) TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	release := api.acquireTraceSemaphore()
	defer release()

	ctx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	defer cancel()

	latest := api.ctxProvider(LatestCtxHeight).BlockHeight()
	if api.maxBlockLookback >= 0 && number.Int64() < latest-api.maxBlockLookback {
		return nil, fmt.Errorf("block number %d is beyond max lookback of %d", number.Int64(), api.maxBlockLookback)
	}

	startTime := time.Now()
	defer recordMetrics("debug_traceBlockByNumber", api.connectionType, startTime, returnErr == nil)
	result, returnErr = api.tracersAPI.TraceBlockByNumber(ctx, number, config)
	return
}

func (api *DebugAPI) TraceBlockByHash(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	release := api.acquireTraceSemaphore()
	defer release()

	ctx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	defer cancel()

	startTime := time.Now()
	defer recordMetrics("debug_traceBlockByHash", api.connectionType, startTime, returnErr == nil)
	result, returnErr = api.tracersAPI.TraceBlockByHash(ctx, hash, config)
	return
}

func (api *DebugAPI) TraceCall(ctx context.Context, args export.TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, config *tracers.TraceCallConfig) (result interface{}, returnErr error) {
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
	_, tx, blockHash, blockNumber, index, err := api.backend.GetTransaction(ctx, hash)
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
