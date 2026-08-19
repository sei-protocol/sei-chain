package evmrpc

import (
	"context"
	"math/big"
	"sync"

	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/tracersutils"
	cosmoclient "github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// RangeQueryWindowBlocksForTest exposes rangeQueryWindowBlocks so integration
// tests can assert tryFilterLogsRange's window boundaries without hardcoding
// the constant.
const RangeQueryWindowBlocksForTest = rangeQueryWindowBlocks

// FilterConfigTest exposes filter limits for integration tests in evmrpc_test.
type FilterConfigTest struct {
	MaxLog      int64
	MaxLogBytes int64
	MaxBlock    int64
}

func NewFilterConfigForTest(cfg FilterConfigTest) *FilterConfig {
	filterCfg := &FilterConfig{
		maxLog:      cfg.MaxLog,
		maxLogBytes: cfg.MaxLogBytes,
		maxBlock:    cfg.MaxBlock,
	}
	if filterCfg.maxBlock <= 0 {
		filterCfg.maxBlock = DefaultMaxBlockRange
	}
	if filterCfg.maxLog <= 0 {
		filterCfg.maxLog = DefaultMaxLogLimit
	}
	if filterCfg.maxLogBytes <= 0 {
		filterCfg.maxLogBytes = receipt.DefaultMaxLogBytes
	}
	return filterCfg
}

// LogFetcherTestDeps wires a LogFetcher for range-path integration tests.
type LogFetcherTestDeps struct {
	TmClient                 cosmoclient.LocalClient
	K                        *keeper.Keeper
	TxConfigProvider         func(int64) cosmoclient.TxConfig
	CtxProvider              func(int64) sdk.Context
	FilterConfig             *FilterConfig
	IncludeSyntheticReceipts bool
	DbReadSemaphore          chan struct{}
	GlobalBlockCache         BlockCache
	CacheCreationMutex       *sync.Mutex
	GlobalLogSlicePool       *LogSlicePool
	Watermarks               *WatermarkManager
}

func NewLogFetcherForTest(deps LogFetcherTestDeps) *LogFetcher {
	if deps.DbReadSemaphore == nil {
		deps.DbReadSemaphore = make(chan struct{}, 1)
	}
	if deps.GlobalBlockCache == nil {
		deps.GlobalBlockCache = NewBlockCache(8)
	}
	if deps.CacheCreationMutex == nil {
		deps.CacheCreationMutex = &sync.Mutex{}
	}
	if deps.GlobalLogSlicePool == nil {
		deps.GlobalLogSlicePool = NewLogSlicePool()
	}
	return &LogFetcher{
		tmClient:                 deps.TmClient,
		k:                        deps.K,
		txConfigProvider:         deps.TxConfigProvider,
		ctxProvider:              deps.CtxProvider,
		filterConfig:             deps.FilterConfig,
		includeSyntheticReceipts: deps.IncludeSyntheticReceipts,
		dbReadSemaphore:          deps.DbReadSemaphore,
		globalBlockCache:         deps.GlobalBlockCache,
		cacheCreationMutex:       deps.CacheCreationMutex,
		globalLogSlicePool:       deps.GlobalLogSlicePool,
		watermarks:               deps.Watermarks,
	}
}

// GetLogsByFiltersWithBackoffForTest exposes the polling-path overflow-backoff
// wrapper for integration tests.
func (f *LogFetcher) GetLogsByFiltersWithBackoffForTest(
	ctx context.Context,
	crit filters.FilterCriteria,
	lastToHeight int64,
) ([]*ethtypes.Log, int64, error) {
	return f.getLogsByFiltersWithBackoff(ctx, crit, lastToHeight)
}

// TryFilterLogsRangeForTest exposes the litt range-query path for integration tests.
func (f *LogFetcher) TryFilterLogsRangeForTest(
	ctx context.Context,
	fromBlock, toBlock uint64,
	crit filters.FilterCriteria,
	limit int64,
) ([]*ethtypes.Log, error) {
	return f.tryFilterLogsRange(ctx, fromBlock, toBlock, crit, limit)
}

// MatchesCriteriaForTest re-exports log criteria matching for fake receipt stores in tests.
func MatchesCriteriaForTest(log *ethtypes.Log, crit filters.FilterCriteria) bool {
	return MatchesCriteria(log, crit)
}

func ProfiledTraceBlockParallelForTest(
	api *DebugAPI,
	ctx context.Context,
	block *ethtypes.Block,
	metadata []tracersutils.TraceBlockMetadata,
	config *tracers.TraceConfig,
	statedb vm.StateDB,
	signer ethtypes.Signer,
	blockHash gethcommon.Hash,
	results []*tracers.TxTraceResult,
	threads int,
) ([]*tracers.TxTraceResult, error) {
	return api.profiledTraceBlockParallel(ctx, block, metadata, config, statedb, signer, blockHash, results, threads)
}

func NewTraceBackendForTest(keeper *keeper.Keeper, ctxProvider func(int64) sdk.Context) *Backend {
	return &Backend{
		keeper:      keeper,
		ctxProvider: ctxProvider,
	}
}

func NewDebugAPIForTest(backend *Backend) *DebugAPI {
	return &DebugAPI{backend: backend}
}

// GasPriceHelperForTest exposes gasPriceHelper for integration tests in evmrpc_test.
func (i *InfoAPI) GasPriceHelperForTest(ctx context.Context, baseFee *big.Int, totalGasUsedPrevBlock uint64, medianRewardPrevBlock *big.Int) (*hexutil.Big, error) {
	return i.gasPriceHelper(ctx, baseFee, totalGasUsedPrevBlock, medianRewardPrevBlock)
}

// CalculateGasUsedRatioForTest exposes calculateGasUsedRatio for integration tests in evmrpc_test.
func (i *InfoAPI) CalculateGasUsedRatioForTest(ctx context.Context, blockHeight int64) (float64, error) {
	return i.calculateGasUsedRatio(ctx, blockHeight)
}
