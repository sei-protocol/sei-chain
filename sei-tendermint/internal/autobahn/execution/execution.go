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
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "autobahn", "execution")

// Config configures an Executor.
type Config struct {
	// GenDoc is the genesis a fresh app is initialized from.
	GenDoc *types.GenesisDoc
	// App is the application blocks are executed on.
	App *abevm.Proxy
	// Signer parses block transactions and retains admitted senders until execution.
	Signer *abevm.Signer
	// AppHashBuilder receives each block's hash before the block runs.
	AppHashBuilder builder.AppHashBuilder
	// BlockStore holds the blocks to execute.
	BlockStore *blockstore.Store
}

// finalizeAheadBlocks is how many blocks Run may have in FinalizeBlock at once.
const finalizeAheadBlocks = 3

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
			Height: utils.Clamp[atypes.RoadHeight](cfg.App.LastBlockHeight()),
		})),
	}
}

// ExecutedBlocks publishes the most recently committed blocks.
func (e *Executor) ExecutedBlocks() utils.AtomicRecv[atypes.ExecutedBlocks] {
	return e.executed.Subscribe()
}

// Run initializes the app, then executes the durable blocks after its tip in
// road order until ctx is canceled or a block fails. Up to finalizeAheadBlocks
// blocks are in FinalizeBlock at once, so the app can prepare a block while the
// previous one executes.
func (e *Executor) Run(ctx context.Context) error {
	next, err := e.initApp(ctx)
	if err != nil {
		return err
	}
	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		inFlight := utils.NewSemaphore(finalizeAheadBlocks)
		results := make(chan scope.JoinHandle[atypes.ExecutedBlock], finalizeAheadBlocks)
		s.Spawn(func() error {
			for n := next; ; n++ {
				release, err := inFlight.Acquire(ctx)
				if err != nil {
					return err
				}
				block, err := e.cfg.BlockStore.WaitForBlock(ctx, n)
				if err != nil {
					return fmt.Errorf("WaitForBlock(%v): %w", n, err)
				}
				b := block.SuperBlock()
				// Reported here, in road order: the builder requires each height to follow the last.
				if err := e.cfg.AppHashBuilder.ReportBlockHash(ctx, uint64(b.Height), [32]byte(b.Hash)); err != nil {
					return fmt.Errorf("AppHashBuilder.ReportBlockHash(%v): %w", b.Height, err)
				}
				result := s.Spawn1(func() (atypes.ExecutedBlock, error) {
					defer release()
					executed, err := e.executeBlock(ctx, b)
					if err != nil {
						return atypes.ExecutedBlock{}, fmt.Errorf("executeBlock(%v): %w", n, err)
					}
					return executed, nil
				})
				if err := utils.Send(ctx, results, result); err != nil {
					return err
				}
			}
		})
		for {
			gigametrics.SetPhase(gigametrics.PhaseConsensus)
			result, err := utils.Recv(ctx, results)
			if err != nil {
				return err
			}
			gigametrics.SetPhase(gigametrics.PhaseExecution)
			executed, err := result.Join(ctx)
			if err != nil {
				return err
			}
			e.publishExecuted(executed)
		}
	})
	logger.Info("Execution stopped", "last_executed", e.executed.Load().Latest().Height)
	return err
}

// initApp brings the app to a state execution can extend: InitChain on a fresh
// app, or the committed tip's header on restart. It returns the first road to
// execute.
func (e *Executor) initApp(ctx context.Context) (atypes.RoadHeight, error) {
	app := e.cfg.App
	info := app.Info()
	last, ok := utils.SafeCast[atypes.RoadHeight](info.LastBlockHeight)
	if !ok {
		return 0, fmt.Errorf("invalid info.LastBlockHeight = %v", info.LastBlockHeight)
	}
	if last == 0 {
		// Fresh start: CometBFT handshaker is skipped in giga mode (see
		// node.go: shouldHandshake = !stateSync && !gigaEnabled), so we
		// call InitChain ourselves to seed the store before the first block. A
		// resumed app reports its durable height and refuses initialization.
		next, ok := utils.SafeCast[atypes.RoadHeight](e.cfg.GenDoc.InitialHeight)
		if !ok || next == 0 {
			return 0, fmt.Errorf("invalid GenDoc.InitialHeight = %v", e.cfg.GenDoc.InitialHeight)
		}
		gasLimit, ok := utils.SafeCast[uint64](e.cfg.GenDoc.ConsensusParams.Block.MaxGas)
		if !ok || gasLimit == 0 {
			return 0, fmt.Errorf("invalid genesis block gas limit: %d", e.cfg.GenDoc.ConsensusParams.Block.MaxGas)
		}
		if err := app.InitChain(abevm.InitConfig{InitialHeight: next, GasLimit: gasLimit}); err != nil {
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

// executeBlock finalizes b and returns its execution result. FinalizeBlock may
// still be running for earlier blocks; the app finalizes them in road order.
func (e *Executor) executeBlock(ctx context.Context, b *atypes.SuperBlock) (atypes.ExecutedBlock, error) {
	app := e.cfg.App
	txs, err := e.cfg.Signer.ParseTxs(b.Txs)
	if err != nil {
		return atypes.ExecutedBlock{}, fmt.Errorf("decode block transactions: %w", err)
	}

	e.cfg.Signer.ForgetTxs(txs)
	resp, err := app.FinalizeBlock(ctx, abevm.FinalizeBlockReq{
		Txs: txs,
		// The road block hash identifies the block before execution and is fed as the block hash to EVM contracts.
		Hash:   common.Hash(b.Hash),
		Height: b.Height,
		Time:   b.Timestamp,
	})
	if err != nil {
		return atypes.ExecutedBlock{}, fmt.Errorf("app.FinalizeBlock(): %w", err)
	}
	return atypes.ExecutedBlock{Height: b.Height, GasUsed: resp.GasUsed}, nil
}

// publishExecuted records b in the executed-block window and the gas metric.
// Blocks must be published in road order.
func (e *Executor) publishExecuted(b atypes.ExecutedBlock) {
	gigametrics.SetPhase(gigametrics.PhaseStorage)
	gigametrics.SetStoragePhase(gigametrics.StoragePhaseBookkeeping)
	metrics.GasUsed.Add(utils.Clamp[int64](b.GasUsed))
	e.executed.Store(e.executed.Load().Push(b))
	gigametrics.EndStoragePhase()
}
