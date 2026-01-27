package app

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmcfg "github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/sei-protocol/sei-load/config"
	"github.com/sei-protocol/sei-load/generator"
	"github.com/sei-protocol/sei-load/generator/scenarios"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
)

type benchmarkLogger struct {
	mx             sync.Mutex
	txCount        int64         // Total transactions processed
	blockCount     int64         // Number of times Increment was called (number of blocks)
	latestHeight   int64         // Highest height seen in the window
	maxBlockTime   time.Duration // Maximum time difference between consecutive blocks
	totalBlockTime time.Duration // Sum of all block time differences in the window
	blockTimeCount int64         // Number of block time differences calculated
	prevBlockTime  time.Time     // Previous block time for calculating differences
	lastFlushTime  time.Time     // When we last flushed (for TPS calculation)
	// Commit time tracking
	maxCommitTime   time.Duration // Maximum commit time in the window
	totalCommitTime time.Duration // Sum of all commit times in the window
	commitCount     int64         // Number of commits in the window
	// Block processing time tracking (ProcessProposal start to FinalizeBlock end)
	blockProcessStartTime time.Time     // Start time of current block processing
	maxBlockProcessTime   time.Duration // Maximum block processing time in the window
	totalBlockProcessTime time.Duration // Sum of all block processing times in the window
	blockProcessCount     int64         // Number of block processing times recorded
	logger                log.Logger
}

func (l *benchmarkLogger) Increment(count int64, blocktime time.Time, height int64) {
	l.mx.Lock()
	defer l.mx.Unlock()

	// Initialize lastFlushTime on first increment (when blocks actually start processing)
	if l.lastFlushTime.IsZero() {
		l.lastFlushTime = time.Now()
	}

	l.txCount += count
	l.blockCount++
	if height > l.latestHeight {
		l.latestHeight = height
	}

	// Calculate time difference between consecutive blocks
	if !l.prevBlockTime.IsZero() {
		blockTimeDiff := blocktime.Sub(l.prevBlockTime)
		if blockTimeDiff > l.maxBlockTime {
			l.maxBlockTime = blockTimeDiff
		}
		l.totalBlockTime += blockTimeDiff
		l.blockTimeCount++
	}
	l.prevBlockTime = blocktime
}

// RecordCommitTime records the duration of a commit operation
func (l *benchmarkLogger) RecordCommitTime(duration time.Duration) {
	l.mx.Lock()
	defer l.mx.Unlock()

	if duration > l.maxCommitTime {
		l.maxCommitTime = duration
	}
	l.totalCommitTime += duration
	l.commitCount++
}

// StartBlockProcessing marks the start of block processing (at ProcessProposal)
func (l *benchmarkLogger) StartBlockProcessing() {
	l.mx.Lock()
	defer l.mx.Unlock()
	l.blockProcessStartTime = time.Now()
}

// EndBlockProcessing marks the end of block processing (at FinalizeBlock end) and records the duration
func (l *benchmarkLogger) EndBlockProcessing() {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.blockProcessStartTime.IsZero() {
		return
	}

	duration := time.Since(l.blockProcessStartTime)
	if duration > l.maxBlockProcessTime {
		l.maxBlockProcessTime = duration
	}
	l.totalBlockProcessTime += duration
	l.blockProcessCount++
	l.blockProcessStartTime = time.Time{} // Reset for next block
}

// calculateTPS computes transactions per second based on transaction count and duration
func calculateTPS(txCount int64, duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(txCount) / duration.Seconds()
}

// calculateAvgBlockTime computes the average block time from total block time and count
func calculateAvgBlockTime(totalBlockTime time.Duration, blockTimeCount int64) int64 {
	if blockTimeCount <= 0 {
		return 0
	}
	avgBlockTime := totalBlockTime / time.Duration(blockTimeCount)
	return avgBlockTime.Milliseconds()
}

// flushStats holds the statistics for a flush window
type flushStats struct {
	txCount           int64
	blockCount        int64
	latestHeight      int64
	maxBlockTimeMs    int64
	avgBlockTimeMs    int64
	maxCommitTimeMs   int64
	avgCommitTimeMs   int64
	maxBlockProcessMs int64
	avgBlockProcessMs int64
	tps               float64
}

// getAndResetStats atomically reads current stats and resets counters for next window
func (l *benchmarkLogger) getAndResetStats(now time.Time) (flushStats, time.Time) {
	l.mx.Lock()
	defer l.mx.Unlock()

	stats := flushStats{
		txCount:           l.txCount,
		blockCount:        l.blockCount,
		latestHeight:      l.latestHeight,
		maxBlockTimeMs:    l.maxBlockTime.Milliseconds(),
		maxCommitTimeMs:   l.maxCommitTime.Milliseconds(),
		maxBlockProcessMs: l.maxBlockProcessTime.Milliseconds(),
	}

	prevTime := l.lastFlushTime
	totalBlockTime := l.totalBlockTime
	blockTimeCount := l.blockTimeCount
	totalCommitTime := l.totalCommitTime
	commitCount := l.commitCount
	totalBlockProcessTime := l.totalBlockProcessTime
	blockProcessCount := l.blockProcessCount

	// Reset counters for next window (but keep prevBlockTime and blockProcessStartTime for continuity)
	l.txCount = 0
	l.blockCount = 0
	l.latestHeight = 0
	l.maxBlockTime = 0
	l.totalBlockTime = 0
	l.blockTimeCount = 0
	l.maxCommitTime = 0
	l.totalCommitTime = 0
	l.commitCount = 0
	l.maxBlockProcessTime = 0
	l.totalBlockProcessTime = 0
	l.blockProcessCount = 0
	l.lastFlushTime = now

	// Calculate TPS
	duration := now.Sub(prevTime)
	if duration > 0 && !prevTime.IsZero() {
		stats.tps = calculateTPS(stats.txCount, duration)
	}

	// Calculate average block time
	stats.avgBlockTimeMs = calculateAvgBlockTime(totalBlockTime, blockTimeCount)

	// Calculate average commit time
	if commitCount > 0 {
		stats.avgCommitTimeMs = (totalCommitTime / time.Duration(commitCount)).Milliseconds()
	}

	// Calculate average block processing time
	if blockProcessCount > 0 {
		stats.avgBlockProcessMs = (totalBlockProcessTime / time.Duration(blockProcessCount)).Milliseconds()
	}

	return stats, prevTime
}

func (l *benchmarkLogger) FlushLog() {
	now := time.Now()
	stats, _ := l.getAndResetStats(now)

	l.logger.Info("benchmark",
		"txs", stats.txCount,
		"blocks", stats.blockCount,
		"height", stats.latestHeight,
		"blockTimeMax", stats.maxBlockTimeMs,
		"blockTimeAvg", stats.avgBlockTimeMs,
		"commitTimeMax", stats.maxCommitTimeMs,
		"commitTimeAvg", stats.avgCommitTimeMs,
		"blockProcessMax", stats.maxBlockProcessMs,
		"blockProcessAvg", stats.avgBlockProcessMs,
		"tps", stats.tps,
	)
}

func (l *benchmarkLogger) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.FlushLog()
		}
	}
}

func NewGeneratorCh(ctx context.Context, txConfig client.TxConfig, chainID string, evmChainID int64, logger log.Logger) <-chan *abci.ResponsePrepareProposal {
	// Read number of transactions per batch from environment variable, default to 1000
	txsPerBatch := 1000
	if envVal := os.Getenv("BENCHMARK_TXS_PER_BATCH"); envVal != "" {
		if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
			txsPerBatch = parsed
		}
	}
	logger.Info("benchmark generator config", "txsPerBatch", txsPerBatch)

	gen, err := generator.NewConfigBasedGenerator(&config.LoadConfig{
		ChainID:    evmChainID,
		SeiChainID: chainID,
		Accounts:   &config.AccountConfig{Accounts: 5000},
		Scenarios: []config.Scenario{{
			Name:   scenarios.EVMTransfer,
			Weight: 1,
		}},
	})
	if err != nil {
		panic("failed to initialize generator: " + err.Error())
	}
	ch := make(chan *abci.ResponsePrepareProposal, 100)
	go func() {
		defer close(ch)
		var height int64
		for {
			// bail on ctx err
			if ctx.Err() != nil {
				return
			}
			// generate txs
			loadTxs := gen.GenerateN(txsPerBatch)
			if len(loadTxs) == 0 {
				continue
			}

			// Convert LoadTx to Cosmos SDK transaction bytes
			txRecords := make([]*abci.TxRecord, 0, len(loadTxs))
			for _, loadTx := range loadTxs {
				if loadTx.EthTx == nil {
					continue
				}

				// Convert Ethereum transaction to Cosmos SDK format
				txData, err := ethtx.NewTxDataFromTx(loadTx.EthTx)
				if err != nil {
					logger.Error("failed to convert eth tx to tx data", "error", err)
					panic(err)
				}

				msg, err := evmtypes.NewMsgEVMTransaction(txData)
				if err != nil {
					logger.Error("failed to create msg evm transaction", "error", err)
					panic(err)
				}

				gasUsedEstimate := loadTx.EthTx.Gas() // Use gas limit from transaction

				txBuilder := txConfig.NewTxBuilder()
				if err = txBuilder.SetMsgs(msg); err != nil {
					logger.Error("failed to set msgs", "error", err)
					panic(err)
				}
				txBuilder.SetGasEstimate(gasUsedEstimate)

				txbz, encodeErr := txConfig.TxEncoder()(txBuilder.GetTx())
				if encodeErr != nil {
					logger.Error("failed to encode tx", "error", encodeErr)
					panic(encodeErr)
				}

				txRecords = append(txRecords, &abci.TxRecord{
					Action: abci.TxRecord_UNMODIFIED,
					Tx:     txbz,
				})
			}

			if len(txRecords) == 0 {
				continue
			}

			proposal := &abci.ResponsePrepareProposal{
				TxRecords: txRecords,
			}

			height++
			select {
			case ch <- proposal:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// InitGenerator initializes the benchmark generator with default config
func (app *App) InitGenerator(ctx context.Context, chainID string, evmChainID int64, logger log.Logger) {
	// defensive logic just to prevent this from initializing
	if evmcfg.IsLiveEVMChainID(evmChainID) {
		panic("benchmark not allowed on live chains")
	}
	logger.Info("Initializing benchmark mode generator", "mode", "benchmark")
	app.benchmarkLogger = &benchmarkLogger{
		logger: logger,
	}
	go app.benchmarkLogger.Start(ctx)
	app.benchmarkProposalCh = NewGeneratorCh(ctx, app.encodingConfig.TxConfig, chainID, evmChainID, logger)
	logger.Info("Benchmark generator initialized and started", "config", "default EVM Transfers")
}

func (app *App) PrepareProposalGeneratorHandler(_ sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	select {
	case proposal, ok := <-app.benchmarkProposalCh:
		if proposal == nil || !ok {
			return &abci.ResponsePrepareProposal{
				TxRecords: []*abci.TxRecord{},
			}, nil
		}
		app.benchmarkLogger.Increment(int64(len(proposal.TxRecords)), req.Time, req.Height)
		return proposal, nil
	default:
		return &abci.ResponsePrepareProposal{
			TxRecords: []*abci.TxRecord{},
		}, nil
	}
}
