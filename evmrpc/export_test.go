package evmrpc

import (
	"context"
	"sync"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
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
