package app

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/app/benchmark"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	evmcfg "github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// InitBenchmark initializes the benchmark system with the configured scenarios.
// This is called during app initialization when the benchmark build tag is enabled.
func (app *App) InitBenchmark(ctx context.Context, chainID string, evmChainID int64, logger log.Logger) {
	// Defensive check: prevent benchmarking on live chains
	if evmcfg.IsLiveEVMChainID(evmChainID) {
		panic("benchmark not allowed on live chains")
	}

	logger.Info("Initializing benchmark mode", "chainID", chainID, "evmChainID", evmChainID)

	manager, err := benchmark.NewManager(ctx, app.encodingConfig.TxConfig, chainID, evmChainID, logger)
	if err != nil {
		panic("failed to initialize benchmark manager: " + err.Error())
	}

	app.benchmarkManager = manager
	logger.Info("Benchmark system initialized")
}

// PrepareProposalBenchmarkHandler generates benchmark transactions during PrepareProposal.
func (app *App) PrepareProposalBenchmarkHandler(_ sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	if app.benchmarkManager == nil {
		return &abci.ResponsePrepareProposal{TxRecords: []*abci.TxRecord{}}, nil
	}

	select {
	case proposal, ok := <-app.benchmarkManager.ProposalChannel():
		if proposal == nil || !ok {
			return &abci.ResponsePrepareProposal{TxRecords: []*abci.TxRecord{}}, nil
		}
		app.benchmarkManager.Logger.Increment(int64(len(proposal.TxRecords)), req.Time, req.Height)
		return proposal, nil
	default:
		return &abci.ResponsePrepareProposal{TxRecords: []*abci.TxRecord{}}, nil
	}
}

// ProcessBenchmarkReceipts extracts receipts from the block and forwards them to
// the benchmark system for deployment tracking during the setup phase.
func (app *App) ProcessBenchmarkReceipts(ctx sdk.Context) {
	if app.benchmarkManager == nil || !app.benchmarkManager.IsSetupPhase() {
		return
	}

	pendingHashes := app.benchmarkManager.GetPendingDeployHashes()
	if len(pendingHashes) == 0 {
		return
	}

	ctx.Logger().Info("benchmark: Looking for deployment receipts",
		"pendingCount", len(pendingHashes),
		"height", ctx.BlockHeight())

	receipts := make(map[common.Hash]*evmtypes.Receipt)
	for _, txHash := range pendingHashes {
		receipt, err := app.EvmKeeper.GetReceipt(ctx, txHash)
		if err != nil {
			ctx.Logger().Info("benchmark: Receipt not found for deployment tx",
				"txHash", txHash.Hex(),
				"error", err.Error())
			continue
		}
		ctx.Logger().Info("benchmark: Found deployment receipt",
			"txHash", txHash.Hex(),
			"status", receipt.Status,
			"contractAddress", receipt.ContractAddress,
			"gasUsed", receipt.GasUsed)
		receipts[txHash] = receipt
	}

	if len(receipts) > 0 {
		app.benchmarkManager.ProcessReceipts(receipts)
	}
}

// BenchmarkLogger returns the benchmark logger for recording timing metrics.
// Returns nil if benchmark mode is not enabled.
func (app *App) BenchmarkLogger() *benchmark.Logger {
	if app.benchmarkManager == nil {
		return nil
	}
	return app.benchmarkManager.Logger
}

// RecordBenchmarkCommitTime records the commit duration for benchmark metrics.
func (app *App) RecordBenchmarkCommitTime(duration time.Duration) {
	if logger := app.BenchmarkLogger(); logger != nil {
		logger.RecordCommitTime(duration)
	}
}

// StartBenchmarkBlockProcessing marks the start of block processing for timing.
func (app *App) StartBenchmarkBlockProcessing() {
	if logger := app.BenchmarkLogger(); logger != nil {
		logger.StartBlockProcessing()
	}
}

// EndBenchmarkBlockProcessing marks the end of block processing for timing.
func (app *App) EndBenchmarkBlockProcessing() {
	if logger := app.BenchmarkLogger(); logger != nil {
		logger.EndBlockProcessing()
	}
}
