package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers"
	_ "github.com/ethereum/go-ethereum/eth/tracers/js" // run init()s to register JS tracers
	traceLogger "github.com/ethereum/go-ethereum/eth/tracers/logger"
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
	maxStructLogBytes  int // per-call cap on retained default struct-logger output; 0 = unlimited
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

func (api *DebugAPI) guardHistoricalDebugTraceByTxHash(ctx context.Context, endpoint string, hash common.Hash) error {
	if api.keeper == nil {
		return nil
	}
	receipt, err := api.keeper.GetReceipt(api.ctxProvider(LatestCtxHeight), hash)
	if err != nil || receipt == nil {
		return nil
	}
	return api.guardHistoricalDebugTraceHeight(ctx, endpoint, int64(receipt.BlockNumber)) //nolint:gosec
}

func (api *DebugAPI) guardHistoricalDebugTraceByNumber(ctx context.Context, endpoint string, number rpc.BlockNumber) error {
	height, err := api.resolveDebugTraceBlockNumber(ctx, number)
	if err != nil {
		return err
	}
	return api.guardHistoricalDebugTraceHeight(ctx, endpoint, height)
}

func (api *DebugAPI) guardHistoricalDebugTraceByHash(ctx context.Context, endpoint string, hash common.Hash) error {
	if api.backend == nil || api.tmClient == nil {
		return nil
	}
	block, err := blockByHashRespectingWatermarks(ctx, api.tmClient, api.backend.watermarks, hash.Bytes(), 1)
	if err != nil || block == nil || block.Block == nil {
		return nil
	}
	return api.guardHistoricalDebugTraceHeight(ctx, endpoint, block.Block.Height)
}

func (api *DebugAPI) guardHistoricalDebugTraceByNumberOrHash(ctx context.Context, endpoint string, blockNrOrHash rpc.BlockNumberOrHash) error {
	if number, ok := blockNrOrHash.Number(); ok {
		return api.guardHistoricalDebugTraceByNumber(ctx, endpoint, number)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		return api.guardHistoricalDebugTraceByHash(ctx, endpoint, hash)
	}
	return api.guardHistoricalDebugTraceHeight(ctx, endpoint, api.ctxProvider(LatestCtxHeight).BlockHeight())
}

func (api *DebugAPI) resolveDebugTraceBlockNumber(ctx context.Context, number rpc.BlockNumber) (int64, error) {
	switch number {
	case rpc.SafeBlockNumber, rpc.FinalizedBlockNumber, rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		return api.ctxProvider(LatestCtxHeight).BlockHeight(), nil
	case rpc.EarliestBlockNumber:
		if api.tmClient == nil {
			return 0, errors.New("tendermint client is not configured")
		}
		genesisRes, err := api.tmClient.Genesis(ctx)
		if err != nil {
			return 0, err
		}
		return genesisRes.Genesis.InitialHeight, nil
	default:
		return number.Int64(), nil
	}
}

func (api *DebugAPI) guardHistoricalDebugTraceHeight(ctx context.Context, endpoint string, blockHeight int64) error {
	latest := api.ctxProvider(LatestCtxHeight).BlockHeight()
	if !isHistoricalDebugTraceBlock(blockHeight, latest, api.maxBlockLookback) {
		return nil
	}
	recordHistoricalDebugTraceAttempt(ctx, endpoint, string(api.connectionType))
	return fmt.Errorf("block number %d is beyond max lookback of %d", blockHeight, api.maxBlockLookback)
}

func isHistoricalDebugTraceBlock(blockHeight, latestHeight, maxBlockLookback int64) bool {
	if maxBlockLookback < 0 || blockHeight < 0 || latestHeight < blockHeight {
		return false
	}
	return blockHeight < latestHeight-maxBlockLookback
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
		maxStructLogBytes:  clampUint64ToInt(debugCfg.MaxTraceStructLogBytes),
		profiledBlockTrace: debugCfg.EnableParallelizedBlockTrace,
	}
}

// clampUint64ToInt converts an operator-configured uint64 to int, saturating at
// math.MaxInt instead of wrapping to a negative value. A negative maxStructLogBytes
// would be treated as "disabled" by clampDefaultStructLogLimit, silently defeating
// the cap — the opposite of an operator setting a very large limit.
func clampUint64ToInt(v uint64) int {
	if v > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(v)
}

// clampDefaultStructLogLimit caps the default struct logger's retained output at
// api.maxStructLogBytes. No-op for custom tracers, a disabled cap (0), or a
// smaller caller-supplied Limit.
func (api *DebugAPI) clampDefaultStructLogLimit(config *tracers.TraceConfig) {
	if config == nil || config.Tracer != nil || api.maxStructLogBytes <= 0 {
		return
	}
	if config.Config == nil {
		config.Config = &traceLogger.Config{}
	}
	if config.Limit <= 0 || config.Limit > api.maxStructLogBytes {
		config.Limit = api.maxStructLogBytes
	}
}

func (api *DebugAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (result interface{}, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "debug_traceTransaction", api.connectionType, startTime, returnErr, recover())
	}()

	if returnErr = api.guardHistoricalDebugTraceByTxHash(ctx, "debug_traceTransaction", hash); returnErr != nil {
		return nil, returnErr
	}

	if cached, ok := api.tryTraceCache(hash, config); ok {
		return cached, nil
	}

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	if config == nil {
		config = &tracers.TraceConfig{}
	}
	api.clampDefaultStructLogLimit(config)
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

// isPanicOrSyntheticTx returns true if the tx isn't traceable. Legacy sei
// block and receipt *ExcludeTraceFail endpoints use it to filter out txs
// whose trace would be empty or meaningless.
//
// Per evmrpc/README.md ("Tracing Failure Management Endpoints"), the target
// is txs "included in blocks but not executed" — pre-state-check failures
// (nonce mismatch, insufficient funds, etc.) and chain-generated synthetic
// txs. Reverts, OOG, and other in-VM failures all ran and produced traces;
// they stay in.
//
// Discriminator: receipts are written in two paths. WriteReceipt
// (x/evm/keeper/receipt.go) covers executed txs and sets EffectiveGasPrice
// from msg.GasPrice (>0 on chains with positive min gas price) and GasUsed
// > 0 (intrinsic gas at minimum). The ante-deferred stub path
// (x/evm/keeper/abci.go) writes EffectiveGasPrice=0 and GasUsed=0 for any
// nonce-bumping ante failure — regardless of which check failed (insufficient
// funds, fee, mempool admission, etc.). Both fields zero is the signal that
// the tx never reached the VM.
//
// (This does NOT use filterTransactions's isReceiptFromAnteError. That
// helper's post-v5.8.0 branch is intentionally narrow — PR #2343's
// TestAnteFailureOthers explicitly requires insufficient-funds receipts to
// be *included* in regular eth_getBlockBy* responses. *ExcludeTraceFail has
// the opposite semantic per the README, so it needs its own check.)
//
//   - GetReceipt error                          → no receipt yet           → exclude
//   - TxType == ShellEVMTxType (math.MaxUint32) → chain-generated synthetic → exclude
//   - EffectiveGasPrice==0 && GasUsed==0        → ante-deferred stub        → exclude
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

	exclude := isReceiptUntraceable(receipt)
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

	if returnErr = api.guardHistoricalDebugTraceByNumber(ctx, "debug_traceBlockByNumber", number); returnErr != nil {
		return nil, returnErr
	}

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	if cached, ok := api.tryBlockTraceCacheByNumber(ctx, number, config); ok {
		return cached, nil
	}

	if config == nil {
		config = &tracers.TraceConfig{}
	}
	api.clampDefaultStructLogLimit(config)
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

	if returnErr = api.guardHistoricalDebugTraceByHash(ctx, "debug_traceBlockByHash", hash); returnErr != nil {
		return nil, returnErr
	}

	if cached, ok := api.tryBlockTraceCacheByHash(ctx, hash, config); ok {
		return cached, nil
	}

	if config == nil {
		config = &tracers.TraceConfig{}
	}
	api.clampDefaultStructLogLimit(config)
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

	if returnErr = api.guardHistoricalDebugTraceByNumberOrHash(ctx, "debug_traceCall", blockNrOrHash); returnErr != nil {
		return nil, returnErr
	}

	if config == nil {
		config = &tracers.TraceCallConfig{}
	}
	api.clampDefaultStructLogLimit(&config.TraceConfig)
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
	if returnErr = api.guardHistoricalDebugTraceByTxHash(ctx, "debug_traceStateAccess", hash); returnErr != nil {
		return nil, returnErr
	}

	ctx, done, err := api.prepareTraceContext(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

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
	// Bail before the potentially expensive prestate/trace serialization if the
	// trace deadline has already elapsed during replay.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response := StateAccessResponse{
		AppState:        state.GetDBImpl(stateDB).Ctx().StoreTracer().DerivePrestateToJson(),
		TendermintState: tendermintTraces.MustMarshalToJson(),
		Receipt:         receiptTraces.MustMarshalToJson(),
	}
	return response, nil
}
