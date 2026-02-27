// Package benchmark provides transaction generation capabilities for benchmarking.
//
// The benchmark system operates in two phases:
//
//  1. Setup Phase: Deploys any contracts required by the configured scenarios.
//     During this phase, deployment transactions are generated and submitted.
//     After each block, receipts are checked to extract deployed contract addresses.
//
//  2. Load Phase: Once all contracts are deployed, the system transitions to
//     generating load transactions according to the configured scenario weights.
//
// Usage:
//
//	cfg, _ := benchmark.LoadConfig(configPath, evmChainID, seiChainID)
//	gen, _ := benchmark.NewGenerator(cfg, txConfig, logger)
//	benchLogger := benchmark.NewLogger(logger)
//	proposalCh := gen.StartProposalChannel(ctx, benchLogger)
//
// The generator can be configured via JSON config files that follow the sei-load
// LoadConfig format. See benchmark/scenarios/ for example configurations.
package benchmark

import (
	"context"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	evmcfg "github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// Manager coordinates benchmark generation and logging.
type Manager struct {
	Generator  *Generator
	Logger     *Logger
	proposalCh <-chan *abci.ResponsePrepareProposal
}

// NewManager creates a new benchmark manager from configuration.
func NewManager(ctx context.Context, txConfig client.TxConfig, chainID string, evmChainID int64, logger log.Logger) (*Manager, error) {
	// Defensive check: prevent benchmarking on live chains
	if evmcfg.IsLiveEVMChainID(evmChainID) {
		panic("benchmark not allowed on live chains")
	}

	// Load config from environment variable or use default
	configPath := os.Getenv("BENCHMARK_CONFIG")

	cfg, err := LoadConfig(configPath, evmChainID, chainID)
	if err != nil {
		return nil, err
	}

	gen, err := NewGenerator(cfg, txConfig, logger)
	if err != nil {
		return nil, err
	}

	benchLogger := NewLogger(logger)
	go benchLogger.Start(ctx)

	proposalCh := gen.StartProposalChannel(ctx, benchLogger)

	logger.Info("Benchmark manager initialized",
		"configPath", configPath,
		"scenarios", len(cfg.Scenarios),
	)

	return &Manager{
		Generator:  gen,
		Logger:     benchLogger,
		proposalCh: proposalCh,
	}, nil
}

// ProposalChannel returns the channel of prepared proposals.
func (m *Manager) ProposalChannel() <-chan *abci.ResponsePrepareProposal {
	return m.proposalCh
}

// ProcessReceipts forwards receipts to the generator for deployment tracking.
func (m *Manager) ProcessReceipts(receipts map[common.Hash]*evmtypes.Receipt) {
	m.Generator.ProcessReceipts(receipts)
}

// IsSetupPhase returns true if the benchmark is still in the setup phase.
func (m *Manager) IsSetupPhase() bool {
	return m.Generator.IsSetupPhase()
}

// GetPendingDeployHashes returns the hashes of pending deployment transactions.
func (m *Manager) GetPendingDeployHashes() []common.Hash {
	return m.Generator.GetPendingDeployHashes()
}
