package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers"
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"     // run init()s to register JS tracers
	_ "github.com/ethereum/go-ethereum/eth/tracers/native" // run init()s to register native tracers
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	IsPanicCacheSize = 5000
	IsPanicCacheTTL  = 1 * time.Minute

	callTracerName     = "callTracer"
	prestateTracerName = "prestateTracer"
	flatCallTracerName = "flatCallTracer"
)

var errTraceConcurrencyLimit = errors.New("trace request rejected due to concurrency limit: server busy")

type DebugAPI struct {
	tracersAPI         *tracers.API
	tmClient           client.LocalClient
	keeper             *keeper.Keeper
	ctxProvider        func(int64) sdk.Context
	txConfigProvider   func(int64) client.TxConfig
	connectionType     ConnectionType
	isPanicCache       *expirable.LRU[common.Hash, bool] // hash to isPanic
	backend            *Backend
	traceCallSemaphore chan struct{} // Semaphore for limiting concurrent trace calls
	maxBlockLookback   int64
	traceTimeout       time.Duration
	profiledBlockTrace bool
}

// acquireTraceSemaphore attempts to acquire a slot from the traceCallSemaphore.
// It returns a function that must be called (typically with defer) to release the semaphore.
// If the semaphore is nil (unlimited concurrency), it does nothing and returns a no-op release function.
// The acquisition respects cancellation and fails fast if all trace slots are in use.
func (api *DebugAPI) acquireTraceSemaphore(ctx context.Context) (func(), error) {
	if api.traceCallSemaphore != nil {
		select {
		case api.traceCallSemaphore <- struct{}{}:
			// If cancellation won the race at the same time as semaphore acquisition,
			// release the slot and surface the context error.
			if err := ctx.Err(); err != nil {
				<-api.traceCallSemaphore
				return func() {}, err
			}
			return func() { <-api.traceCallSemaphore }, nil
		case <-ctx.Done():
			return func() {}, ctx.Err()
		default:
			return func() {}, errTraceConcurrencyLimit
		}
	}
	return func() {}, nil // No-op if semaphore is not active
}

// prepareTraceContext creates the trace timeout context and acquires a trace slot if one
// is immediately available, returning a cleanup function for acquired resources.
func (api *DebugAPI) prepareTraceContext(ctx context.Context) (context.Context, func(), error) {
	traceCtx, cancel := context.WithTimeout(ctx, api.traceTimeout)
	release, err := api.acquireTraceSemaphore(traceCtx)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return traceCtx, func() {
		release()
		cancel()
	}, nil
}

type SeiDebugAPI struct {
	*DebugAPI
}

func NewDebugAPI(
	tmClient client.LocalClient,
	k *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
	debugCfg evmrpcconfig.Config,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
	watermarks *WatermarkManager,
) *DebugAPI {
	backend := NewBackend(ctxProvider, k, beginBlockKeepers, txConfigProvider, tmClient, config, app, antehandler, globalBlockCache, cacheCreationMutex, watermarks)
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
		backend:            backend,
		traceCallSemaphore: sem,
		maxBlockLookback:   debugCfg.MaxTraceLookbackBlocks,
		traceTimeout:       debugCfg.TraceTimeout,
		profiledBlockTrace: debugCfg.EnableParallelizedBlockTrace,
	}
}

func NewSeiDebugAPI(
	tmClient client.LocalClient,
	k *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	config *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
	debugCfg evmrpcconfig.Config,
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
	watermarks *WatermarkManager,
) *SeiDebugAPI {
	backend := NewBackend(ctxProvider, k, beginBlockKeepers, txConfigProvider, tmClient, config, app, antehandler, globalBlockCache, cacheCreationMutex, watermarks)
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
		profiledBlockTrace: debugCfg.EnableParallelizedBlockTrace,
		backend:            backend,
		// isPanicCache: nil, // Explicitly nil as per original structure for SeiDebugAPI's embedded DebugAPI
	}

	return &SeiDebugAPI{
		DebugAPI: embeddedDebugAPI,
	}
}

func (api *DebugAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_traceTransaction", api.connectionType, startTime, returnErr, recover())
	}()

	if cached, ok := api.tryTraceCache(hash, config); ok {
		return cached, nil
	}

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	return api.tracersAPI.TraceTransaction(ctx, hash, config)
}

func (api *DebugAPI) tryTraceCache(hash common.Hash, config *tracers.TraceConfig) (interface{}, bool) {
	cache := api.keeper.TraceDB()
	if cache == nil {
		return nil, false
	}
	name := bakeableTracerName(config)
	if name == "" {
		return nil, false
	}
	receipt, err := api.keeper.GetReceipt(api.ctxProvider(LatestCtxHeight), hash)
	if err != nil || receipt == nil {
		return nil, false
	}
	bz, ok, err := cache.Get(int64(receipt.BlockNumber), name, hash) //nolint:gosec
	if err != nil || !ok {
		return nil, false
	}
	return bz, true
}

// blockTraceCacheGet assembles a per-tx hit; returns (nil, false) if any miss.
func blockTraceCacheGet(cache *keeper.TraceDB, height int64, txHashes []common.Hash, config *tracers.TraceConfig) ([]*tracers.TxTraceResult, bool) {
	if cache == nil {
		return nil, false
	}
	name := bakeableTracerName(config)
	if name == "" {
		return nil, false
	}
	out := make([]*tracers.TxTraceResult, 0, len(txHashes))
	for _, h := range txHashes {
		bz, ok, err := cache.Get(height, name, h)
		if err != nil || !ok {
			return nil, false
		}
		out = append(out, &tracers.TxTraceResult{TxHash: h, Result: bz})
	}
	return out, true
}

// tryBlockResultCache reads the per-block JSON in one seek. Preferred over
// blockTraceCacheGet which assembles N per-tx rows.
func tryBlockResultCache(cache *keeper.TraceDB, height int64, config *tracers.TraceConfig) (interface{}, bool) {
	name := bakeableTracerName(config)
	if cache == nil || name == "" {
		return nil, false
	}
	bz, ok, err := cache.GetBlock(height, name)
	if err != nil || !ok {
		return nil, false
	}
	return bz, true
}

func (api *DebugAPI) tryBlockTraceCacheByNumber(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) (interface{}, bool) {
	cache := api.keeper.TraceDB()
	if cache == nil || bakeableTracerName(config) == "" {
		return nil, false
	}
	block, _, err := api.backend.BlockByNumber(ctx, number)
	if err != nil || block == nil {
		return nil, false
	}
	height := int64(block.NumberU64()) //nolint:gosec
	if v, ok := tryBlockResultCache(cache, height, config); ok {
		return v, true
	}
	return blockTraceCacheGet(cache, height, txHashesOf(block.Transactions()), config)
}

func (api *DebugAPI) tryBlockTraceCacheByHash(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (interface{}, bool) {
	cache := api.keeper.TraceDB()
	if cache == nil || bakeableTracerName(config) == "" {
		return nil, false
	}
	block, _, err := api.backend.BlockByHash(ctx, hash)
	if err != nil || block == nil {
		return nil, false
	}
	height := int64(block.NumberU64()) //nolint:gosec
	if v, ok := tryBlockResultCache(cache, height, config); ok {
		return v, true
	}
	return blockTraceCacheGet(cache, height, txHashesOf(block.Transactions()), config)
}

// tryExcludeFailBlockTraceCacheByNumber reads the per-block JSON row, parses it,
// and drops entries with Error set. Per-tx rows are skipped — they omit Error.
func (api *DebugAPI) tryExcludeFailBlockTraceCacheByNumber(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) ([]*tracers.TxTraceResult, bool) {
	cache := api.keeper.TraceDB()
	name := bakeableTracerName(config)
	if cache == nil || name == "" {
		return nil, false
	}
	block, _, err := api.backend.BlockByNumber(ctx, number)
	if err != nil || block == nil {
		return nil, false
	}
	return filterExcludeFailFromBlockCache(cache, int64(block.NumberU64()), name) //nolint:gosec
}

func (api *DebugAPI) tryExcludeFailBlockTraceCacheByHash(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) ([]*tracers.TxTraceResult, bool) {
	cache := api.keeper.TraceDB()
	name := bakeableTracerName(config)
	if cache == nil || name == "" {
		return nil, false
	}
	block, _, err := api.backend.BlockByHash(ctx, hash)
	if err != nil || block == nil {
		return nil, false
	}
	return filterExcludeFailFromBlockCache(cache, int64(block.NumberU64()), name) //nolint:gosec
}

func filterExcludeFailFromBlockCache(cache *keeper.TraceDB, height int64, tracer string) ([]*tracers.TxTraceResult, bool) {
	bz, ok, err := cache.GetBlock(height, tracer)
	if err != nil || !ok {
		return nil, false
	}
	var traces []*tracers.TxTraceResult
	if err := json.Unmarshal(bz, &traces); err != nil {
		return nil, false
	}
	out := make([]*tracers.TxTraceResult, 0, len(traces))
	for _, t := range traces {
		if t == nil || len(t.Error) > 0 {
			continue
		}
		out = append(out, t)
	}
	return out, true
}

func txHashesOf(txs gethtypes.Transactions) []common.Hash {
	out := make([]common.Hash, len(txs))
	for i, tx := range txs {
		out[i] = tx.Hash()
	}
	return out
}

// bakeableTracerName returns the tracer name iff config matches what the
// baker produces (no per-call TracerConfig); empty means "fall through".
func bakeableTracerName(config *tracers.TraceConfig) string {
	if config == nil || config.Tracer == nil {
		return ""
	}
	if len(config.TracerConfig) > 0 {
		return ""
	}
	switch *config.Tracer {
	case callTracerName, prestateTracerName, flatCallTracerName:
		return *config.Tracer
	default:
		return ""
	}
}

func (api *DebugAPI) AsRawJSON(result interface{}) ([]byte, bool) {
	switch v := result.(type) {
	case json.RawMessage:
		return v, true
	case []byte:
		return v, true
	case string:
		return []byte(v), true
	default:
		bz, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		return bz, true
	}
}

func (api *SeiDebugAPI) TraceBlockByNumberExcludeTraceFail(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "sei_traceBlockByNumberExcludeTraceFail", api.connectionType, startTime, returnErr, recover())
	}()

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	latest := api.ctxProvider(LatestCtxHeight).BlockHeight()
	if api.maxBlockLookback >= 0 && number.Int64() < latest-api.maxBlockLookback {
		return nil, fmt.Errorf("block number %d is beyond max lookback of %d", number.Int64(), api.maxBlockLookback)
	}

	if cached, ok := api.tryExcludeFailBlockTraceCacheByNumber(ctx, number, config); ok {
		return cached, nil
	}

	if api.shouldUseProfiledBlockTrace(config) {
		result, returnErr = api.profiledTraceBlockByNumber(ctx, number, config)
	} else {
		result, returnErr = api.tracersAPI.TraceBlockByNumber(ctx, number, config)
	}
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
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "sei_traceBlockByHashExcludeTraceFail", api.connectionType, startTime, returnErr, recover())
	}()

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	if cached, ok := api.tryExcludeFailBlockTraceCacheByHash(ctx, hash, config); ok {
		return cached, nil
	}

	if api.shouldUseProfiledBlockTrace(config) {
		result, returnErr = api.profiledTraceBlockByHash(ctx, hash, config)
	} else {
		result, returnErr = api.tracersAPI.TraceBlockByHash(ctx, hash, config)
	}
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

// isPanicOrSyntheticTx returns true if the tx isn't traceable — used by the
// *ExcludeTraceFail endpoints to filter out txs whose trace would be empty
// or meaningless.
//
// Per evmrpc/README.md ("Tracing Failure Management Endpoints"), the target
// is txs "included in blocks but not executed" — pre-state-check failures
// (nonce mismatch, insufficient funds) and chain-generated synthetic txs.
// Reverts, OOG, and other in-VM failures all ran and produced traces; they
// stay in.
//
// Receipt fields are sufficient to discriminate. WriteReceipt
// (x/evm/keeper/receipt.go) populates EffectiveGasPrice from msg.GasPrice
// for any tx that reached the VM. The ante-deferred path
// (x/evm/keeper/abci.go) writes a stub receipt with EffectiveGasPrice=0
// for nonce-bumping ante failures. isReceiptFromAnteError captures that
// signal, and the same helper is used by filterTransactions for block
// endpoints — so block and tx ExcludeTraceFail filter the same set.
//
//   - GetReceipt error                          → no receipt yet           → exclude
//   - TxType == ShellEVMTxType (math.MaxUint32) → chain-generated synthetic → exclude
//   - isReceiptFromAnteError(receipt)           → never executed           → exclude
//   - anything else (success / revert / OOG)    → executed, has a trace    → include
func (api *DebugAPI) isPanicOrSyntheticTx(ctx context.Context, hash common.Hash) (isPanic bool, err error) {
	if api.isPanicCache != nil {
		if cached, ok := api.isPanicCache.Get(hash); ok {
			return cached, nil
		}
	}

	sdkctx := api.ctxProvider(LatestCtxHeight)
	receipt, rerr := api.keeper.GetReceipt(sdkctx, hash)
	if rerr != nil {
		// No receipt: treat as panic/synthetic. Not cached — the receipt
		// store can lag the RPC for a freshly committed tx, so this answer
		// may flip to "include" once the write lands.
		return true, nil
	}

	exclude := receipt.TxType == evmtypes.ShellEVMTxType || isReceiptFromAnteError(sdkctx, receipt)
	if api.isPanicCache != nil {
		api.isPanicCache.Add(hash, exclude)
	}
	return exclude, nil
}

func (api *DebugAPI) TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_traceBlockByNumber", api.connectionType, startTime, returnErr, recover())
	}()

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	latest := api.ctxProvider(LatestCtxHeight).BlockHeight()
	if api.maxBlockLookback >= 0 && number.Int64() < latest-api.maxBlockLookback {
		return nil, fmt.Errorf("block number %d is beyond max lookback of %d", number.Int64(), api.maxBlockLookback)
	}

	if cached, ok := api.tryBlockTraceCacheByNumber(ctx, number, config); ok {
		return cached, nil
	}

	if api.shouldUseProfiledBlockTrace(config) {
		result, returnErr = api.profiledTraceBlockByNumber(ctx, number, config)
	} else {
		result, returnErr = api.tracersAPI.TraceBlockByNumber(ctx, number, config)
	}
	return
}

func (api *DebugAPI) TraceBlockByHash(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_traceBlockByHash", api.connectionType, startTime, returnErr, recover())
	}()

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	if cached, ok := api.tryBlockTraceCacheByHash(ctx, hash, config); ok {
		return cached, nil
	}

	if api.shouldUseProfiledBlockTrace(config) {
		result, returnErr = api.profiledTraceBlockByHash(ctx, hash, config)
	} else {
		result, returnErr = api.tracersAPI.TraceBlockByHash(ctx, hash, config)
	}
	return
}

func (api *DebugAPI) TraceCall(ctx context.Context, args export.TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, config *tracers.TraceCallConfig) (result interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_traceCall", api.connectionType, startTime, returnErr, recover())
	}()

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	result, returnErr = api.tracersAPI.TraceCall(ctx, args, blockNrOrHash, config)
	return
}

func (api *DebugAPI) GetRawHeader(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (_ hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_getRawHeader", api.connectionType, startTime, returnErr, recover())
	}()
	return nil, &ErrEVMNotSupported{Msg: "debug_getRawHeader is not supported on Sei EVM RPC"}
}

func (api *DebugAPI) GetRawBlock(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (_ hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_getRawBlock", api.connectionType, startTime, returnErr, recover())
	}()
	return nil, &ErrEVMNotSupported{Msg: "debug_getRawBlock is not supported on Sei EVM RPC"}
}

func (api *DebugAPI) GetRawReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (_ []hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_getRawReceipts", api.connectionType, startTime, returnErr, recover())
	}()
	return nil, &ErrEVMNotSupported{Msg: "debug_getRawReceipts is not supported on Sei EVM RPC"}
}

func (api *DebugAPI) GetRawTransaction(ctx context.Context, hash common.Hash) (_ hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_getRawTransaction", api.connectionType, startTime, returnErr, recover())
	}()
	return nil, &ErrEVMNotSupported{Msg: "debug_getRawTransaction is not supported on Sei EVM RPC"}
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
	tracingBackend := *api.backend
	tracingBackend.ctxProvider = func(height int64) sdk.Context {
		return api.ctxProvider(height).WithIsTracing(true)
	}
	ctx = WithReceiptTraces(ctx, receiptTraces)
	_, tx, blockHash, blockNumber, index, err := tracingBackend.GetTransaction(ctx, hash)
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
	block, _, err := tracingBackend.BlockByHash(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	stateDB, _, err := tracingBackend.ReplayTransactionTillIndex(ctx, block, int(index)) //nolint:gosec
	if err != nil {
		return nil, err
	}
	response := StateAccessResponse{
		AppState:        state.GetDBImpl(stateDB).Ctx().StoreTracer().DerivePrestateToJson(),
		TendermintState: tendermintTraces.MustMarshalToJson(),
		Receipt:         receiptTraces.MustMarshalToJson(),
	}
	return response, nil
}
