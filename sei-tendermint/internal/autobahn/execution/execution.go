// Package execution runs the application over the blocks the BlockStore has made durable.
package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	gigametrics "github.com/sei-protocol/sei-chain/giga/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Config configures an Executor.
type Config struct {
	// ParseWorkers controls transaction decoding; zero uses GOMAXPROCS.
	ParseWorkers int
	// GenDoc is the genesis a fresh app is initialized from.
	GenDoc *types.GenesisDoc
	// App is the application blocks are executed on.
	App *proxy.EVMProxy
	// AppHashBuilder receives each block's hash before the block runs.
	AppHashBuilder builder.AppHashBuilder
	// BlockStore holds the blocks to execute.
	BlockStore *blockstore.Store
}

// Executor executes durable blocks on the app, one at a time in road order.
type Executor struct {
	cfg      *Config
	executed utils.AtomicSend[atypes.ExecutedBlocks]
}

// New returns an Executor for cfg.
func New(cfg *Config) *Executor {
	return &Executor{
		cfg: cfg,
		executed: utils.NewAtomicSend(atypes.NewExecutedBlocks(atypes.ExecutedBlock{
			Number: utils.Clamp[atypes.RoadIndex](cfg.App.LastBlockHeight()),
		})),
	}
}

// ExecutedBlocks publishes the most recently committed blocks.
func (e *Executor) ExecutedBlocks() utils.AtomicRecv[atypes.ExecutedBlocks] {
	return e.executed.Subscribe()
}

// Run initializes the app, then executes the durable blocks after its tip in
// road order until ctx is canceled or a block fails.
func (e *Executor) Run(ctx context.Context) error {
	next, err := e.initApp(ctx)
	if err != nil {
		return err
	}
	for n := next; ; n++ {
		gigametrics.SetPhase(gigametrics.PhaseConsensus)
		block, err := e.cfg.BlockStore.WaitForBlock(ctx, n)
		if err != nil {
			return fmt.Errorf("WaitForBlock(%v): %w", n, err)
		}
		gigametrics.SetPhase(gigametrics.PhaseExecution)
		if err := e.executeBlock(ctx, block.SuperBlock()); err != nil {
			return fmt.Errorf("executeBlock(%v): %w", n, err)
		}
	}
}

// initApp brings the app to a state execution can extend: InitChain on a fresh
// app, or the committed tip's header on restart. It returns the first road to
// execute.
func (e *Executor) initApp(ctx context.Context) (atypes.RoadIndex, error) {
	app := e.cfg.App
	info := app.Info()
	last, ok := utils.SafeCast[atypes.RoadIndex](info.LastBlockHeight)
	if !ok {
		return 0, fmt.Errorf("invalid info.LastBlockHeight = %v", info.LastBlockHeight)
	}
	if last == 0 {
		// Fresh start: CometBFT handshaker is skipped in giga mode (see
		// node.go: shouldHandshake = !stateSync && !gigaEnabled), so we
		// InitChain seeds the store before the first block. A resumed app reports
		// its durable height instead and refuses initialization.
		next, ok := utils.SafeCast[atypes.RoadIndex](e.cfg.GenDoc.InitialHeight)
		if !ok || next == 0 {
			return 0, fmt.Errorf("invalid GenDoc.InitialHeight = %v", e.cfg.GenDoc.InitialHeight)
		}
		gasLimit, ok := utils.SafeCast[uint64](e.cfg.GenDoc.ConsensusParams.Block.MaxGas)
		if !ok || gasLimit == 0 {
			return 0, fmt.Errorf("invalid genesis block gas limit: %d", e.cfg.GenDoc.ConsensusParams.Block.MaxGas)
		}
		if err := app.InitChain(proxy.InitConfig{InitialHeight: next, GasLimit: gasLimit}); err != nil {
			return 0, fmt.Errorf("App.InitChain(): %w", err)
		}
		return next, nil
	}
	// Blocks execute only once durable, so the app tip is in the BlockStore
	// unless retention has passed it.
	block, err := e.cfg.BlockStore.WaitForBlock(ctx, last)
	if err != nil {
		if errors.Is(err, atypes.ErrPruned) {
			return 0, fmt.Errorf("app tip %d is below the BlockStore retention floor, so its header cannot be loaded: %w", last, err)
		}
		return 0, fmt.Errorf("WaitForBlock(%v): %w", last, err)
	}
	app.RestoreBlockTime(block.Header().Timestamp())
	return last + 1, nil
}

// executeBlock reports b's hash, finalizes b, and publishes its execution result.
func (e *Executor) executeBlock(ctx context.Context, b *atypes.SuperBlock) error {
	if err := e.cfg.AppHashBuilder.ReportBlockHash(ctx, uint64(b.Height), [32]byte(b.Hash)); err != nil {
		return fmt.Errorf("AppHashBuilder.ReportBlockHash(%v): %w", b.Height, err)
	}
	app := e.cfg.App
	txs, err := app.ParseTxs(app.EvmChainID(), b.Txs, e.cfg.ParseWorkers)
	if err != nil {
		return fmt.Errorf("decode block transactions: %w", err)
	}

	resp, err := app.FinalizeBlock(ctx, proxy.FinalizeBlockReq{
		Txs: txs,
		// The road block hash identifies the block before execution and is fed as the block hash to EVM contracts.
		Hash:   common.Hash(b.Hash),
		Height: b.Height,
		Time:   b.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("app.FinalizeBlock(): %w", err)
	}
	gigametrics.SetPhase(gigametrics.PhaseStorage)
	gasUsed := resp.GasUsed

	gigametrics.SetStoragePhase(gigametrics.StoragePhaseBookkeeping)
	metrics.GasUsed.Add(utils.Clamp[int64](gasUsed))
	e.executed.Store(e.executed.Load().Push(atypes.ExecutedBlock{
		Number:  b.Height,
		GasUsed: gasUsed,
	}))
	gigametrics.EndStoragePhase()
	return nil
}
