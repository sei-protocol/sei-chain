// Package execution runs the application over the blocks the BlockStore has made durable.
package execution

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	gigametrics "github.com/sei-protocol/sei-chain/giga/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Config configures an Executor.
type Config struct {
	// GenDoc is the genesis a fresh app is initialized from.
	GenDoc *types.GenesisDoc
	// App is the application blocks are executed on.
	App *proxy.EVMProxy
	// AppHashBuilder receives each block's hash before the block runs.
	AppHashBuilder builder.AppHashBuilder
	// BlockStore holds the blocks to execute.
	BlockStore *blockstore.Store
}

// finalizeAheadBlocks is how many blocks Run may be inside FinalizeBlock for at once.
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
			Number: utils.Clamp[atypes.RoadIndex](cfg.App.LastBlockHeight()),
		})),
	}
}

// ExecutedBlocks publishes the most recently committed blocks.
func (e *Executor) ExecutedBlocks() utils.AtomicRecv[atypes.ExecutedBlocks] {
	return e.executed.Subscribe()
}

// pendingExec is one in-flight executeBlock, whose outcome is reported on done.
type pendingExec struct {
	height atypes.RoadIndex
	done   chan execResult
}

// execResult is the gas a committed block used, or the error that stopped it.
type execResult struct {
	gasUsed int64
	err     error
}

// Run initializes the app, then executes the durable blocks after its tip in
// road order until ctx is canceled or a block fails. Up to finalizeAheadBlocks
// calls to FinalizeBlock are in flight at once; the app decodes a later block
// before the earlier one's turn to execute.
func (e *Executor) Run(ctx context.Context) error {
	next, err := e.initApp(ctx)
	if err != nil {
		return err
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		inFlight := utils.NewSemaphore(finalizeAheadBlocks)
		results := make(chan pendingExec, finalizeAheadBlocks)
		s.Spawn(func() error {
			for n := next; ; n++ {
				release, err := inFlight.Acquire(ctx)
				if err != nil {
					return err
				}
				block, err := e.cfg.BlockStore.WaitForBlock(ctx, n)
				if err != nil {
					release()
					return fmt.Errorf("WaitForBlock(%v): %w", n, err)
				}
				b := block.SuperBlock()
				req := e.makeFinalizeRequest(b)
				done := make(chan execResult, 1)
				s.Spawn(func() error {
					defer release()
					gasUsed, err := e.executeBlock(ctx, b, req)
					done <- execResult{gasUsed: gasUsed, err: err}
					return nil
				})
				if err := utils.Send(ctx, results, pendingExec{height: b.Height, done: done}); err != nil {
					return err
				}
			}
		})
		for {
			gigametrics.SetPhase(gigametrics.PhaseConsensus)
			p, err := utils.Recv(ctx, results)
			if err != nil {
				return err
			}
			gigametrics.SetPhase(gigametrics.PhaseExecution)
			result, err := utils.Recv(ctx, p.done)
			if err != nil {
				return err
			}
			if result.err != nil {
				return fmt.Errorf("executeBlock(%v): %w", p.height, result.err)
			}
			e.publishExecuted(p.height, result.gasUsed)
		}
	})
}

// publishExecuted records a committed block in the executed-block window and the gas metric.
// It must be called in road order: the window indexes blocks by their offset from the oldest.
func (e *Executor) publishExecuted(height atypes.RoadIndex, gasUsed int64) {
	gigametrics.SetStoragePhase(gigametrics.StoragePhaseBookkeeping)
	metrics.GasUsed.Add(gasUsed)
	e.executed.Store(e.executed.Load().Push(atypes.ExecutedBlock{
		Number:  height,
		GasUsed: utils.Clamp[uint64](gasUsed),
	}))
	gigametrics.EndStoragePhase()
}

// makeFinalizeRequest builds the FinalizeBlock request for a superblock, except for
// the header's ProposerAddress, which depends on the committed state.
func (e *Executor) makeFinalizeRequest(b *atypes.SuperBlock) *proxy.RequestFinalizeBlock {
	return &proxy.RequestFinalizeBlock{
		Txs: b.Txs,
		// The road block hash identifies the block before execution.
		// It is used to identify block processed optimistically
		// and is fed as block hash to EVM contracts.
		Hash: b.Hash[:],
		Header: &proxy.Header{
			ChainID: e.cfg.GenDoc.ChainID,
			Height:  int64(b.Height), // nolint:gosec // different representations of the same value
			Time:    b.Timestamp,
		},
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
		// call InitChain ourselves. It sets up the app's deliverState
		// against which the first FinalizeBlock below runs.
		//
		// Re-entering on restart (crashed after InitChain, before the first
		// block became durable) is safe — nothing was committed, so it
		// behaves as a fresh init. An app whose storage already holds
		// blocks must report them here and refuse InitChain, since blocks
		// may be durable before Commit acknowledged them.
		if _, err := app.InitChain(&proxy.RequestInitChain{
			Time:          e.cfg.GenDoc.GenesisTime,
			ChainID:       e.cfg.GenDoc.ChainID,
			InitialHeight: e.cfg.GenDoc.InitialHeight,
			MaxGas:        e.cfg.GenDoc.ConsensusParams.Block.MaxGas,
			AppStateBytes: e.cfg.GenDoc.AppState,
		}); err != nil {
			return 0, fmt.Errorf("App.InitChain(): %w", err)
		}
		next, ok := utils.SafeCast[atypes.RoadIndex](e.cfg.GenDoc.InitialHeight)
		if !ok {
			return 0, fmt.Errorf("invalid GenDoc.InitialHeight = %v", e.cfg.GenDoc.InitialHeight)
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
	app.InitLastHeader(&proxy.Header{
		ChainID: e.cfg.GenDoc.ChainID,
		Height:  int64(last), // nolint:gosec // different representations of the same value
		Time:    block.Header().Timestamp(),
		// TODO: for consistency we should also set proposerAddress here,
		// but this is a placeholder solution so maybe we don't care.
	})
	return last + 1, nil
}

// executeBlock reports b's hash, finalizes it, and commits it, and returns the gas it used.
// FinalizeBlock may already be running for later blocks; the app executes them in height order.
func (e *Executor) executeBlock(ctx context.Context, b *atypes.SuperBlock, req *proxy.RequestFinalizeBlock) (int64, error) {
	if err := e.cfg.AppHashBuilder.ReportBlockHash(ctx, uint64(b.Height), [32]byte(b.Hash)); err != nil {
		return 0, fmt.Errorf("AppHashBuilder.ReportBlockHash(%v): %w", b.Height, err)
	}
	app := e.cfg.App
	// WARNING: the reward distribution has corner cases where it forgets the proposer,
	// because reward is distributed with a delay. This is not our problem here though.
	if vals := app.GetValidators(); len(vals) > 0 {
		// Deterministically select a proposer from the app's validator committee.
		// We need it so that app does not emit error logs.
		proposer := slices.MinFunc(vals, func(a, b proxy.ValidatorUpdate) int { return a.PubKey.Compare(b.PubKey) })
		req.Header.ProposerAddress = proposer.PubKey.ED25519().Address()
	}
	resp, err := app.FinalizeBlock(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("app.FinalizeBlock(): %w", err)
	}
	gigametrics.SetPhase(gigametrics.PhaseStorage)
	gasUsed := finalizeBlockGasUsed(resp)

	gigametrics.SetStoragePhase(gigametrics.StoragePhaseAppCommit)
	if _, err := app.Commit(ctx); err != nil {
		return 0, fmt.Errorf("app.Commit(): %w", err)
	}
	return gasUsed, nil
}

// finalizeBlockGasUsed returns the gas used, as reported by FinalizeBlock.
func finalizeBlockGasUsed(resp *proxy.ResponseFinalizeBlock) int64 {
	var total int64
	for _, result := range resp.TxResults {
		if result != nil {
			total += max(0, result.GasUsed)
		}
	}
	return total
}
