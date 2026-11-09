// Package execution runs the application over the blocks the BlockStore has made durable.
package execution

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	gigametrics "github.com/sei-protocol/sei-chain/giga/metrics"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
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
	App *proxy.Proxy
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

// pendingBlock is a durable block with its FinalizeBlock request, whose preparation reports on
// prepared once it is done.
type pendingBlock struct {
	block    *atypes.SuperBlock
	req      *abci.RequestFinalizeBlock
	prepared chan error
}

// Run initializes the app, then executes the durable blocks after its tip in
// road order until ctx is canceled or a block fails. Each block is prepared on
// its own goroutine, up to atypes.PrepareAheadBlocks ahead of the one executing.
func (e *Executor) Run(ctx context.Context) error {
	next, err := e.initApp(ctx)
	if err != nil {
		return err
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		blocks := make(chan pendingBlock, atypes.PrepareAheadBlocks)
		s.Spawn(func() error {
			for n := next; ; n++ {
				block, err := e.cfg.BlockStore.WaitForBlock(ctx, n)
				if err != nil {
					return fmt.Errorf("WaitForBlock(%v): %w", n, err)
				}
				b := block.SuperBlock()
				p := pendingBlock{block: b, req: e.finalizeRequest(b), prepared: make(chan error, 1)}
				s.Spawn(func() error {
					p.prepared <- e.cfg.App.PrepareBlock(ctx, p.req)
					return nil
				})
				if err := utils.Send(ctx, blocks, p); err != nil {
					return err
				}
			}
		})
		for {
			gigametrics.SetPhase(gigametrics.PhaseConsensus)
			p, err := utils.Recv(ctx, blocks)
			if err != nil {
				return err
			}
			prepareErr, err := utils.Recv(ctx, p.prepared)
			if err != nil {
				return err
			}
			if prepareErr != nil {
				return fmt.Errorf("app.PrepareBlock(%v): %w", p.block.Height, prepareErr)
			}
			gigametrics.SetPhase(gigametrics.PhaseExecution)
			if err := e.executeBlock(ctx, p.block, p.req); err != nil {
				return fmt.Errorf("executeBlock(%v): %w", p.block.Height, err)
			}
		}
	})
}

// finalizeRequest builds the FinalizeBlock request for a superblock, except for
// the header's ProposerAddress, which depends on the committed state.
func (e *Executor) finalizeRequest(b *atypes.SuperBlock) *abci.RequestFinalizeBlock {
	return &abci.RequestFinalizeBlock{
		Txs: b.Txs,
		// Empty DecidedLastCommit does not indicate missing votes.
		DecidedLastCommit: abci.CommitInfo{},
		// The road block hash identifies the block before execution.
		// It is used to identify block processed optimistically
		// and is fed as block hash to EVM contracts.
		Hash: b.Hash[:],
		Header: (&types.Header{
			ChainID: e.cfg.GenDoc.ChainID,
			Height:  int64(b.Height), // nolint:gosec // different representations of the same value
			Time:    b.Timestamp,
		}).ToProto(),
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
		if _, err := app.InitChain(e.cfg.GenDoc.ToRequestInitChain()); err != nil {
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
	app.InitLastHeader((&types.Header{
		ChainID: e.cfg.GenDoc.ChainID,
		Height:  int64(last), // nolint:gosec // different representations of the same value
		Time:    block.Header().Timestamp(),
		// TODO: for consistency we should also set proposerAddress here,
		// but this is a placeholder solution so maybe we don't care.
	}).ToProto())
	return last + 1, nil
}

// executeBlock reports b's hash to the app hash builder, then finalizes and
// commits b through its prepared request.
func (e *Executor) executeBlock(ctx context.Context, b *atypes.SuperBlock, req *abci.RequestFinalizeBlock) error {
	if err := e.cfg.AppHashBuilder.ReportBlockHash(ctx, uint64(b.Height), [32]byte(b.Hash)); err != nil {
		return fmt.Errorf("AppHashBuilder.ReportBlockHash(%v): %w", b.Height, err)
	}
	app := e.cfg.App
	// Read from the app's state on the execute loop, after the previous block's Commit.
	// WARNING: the reward distribution has corner cases where it forgets the proposer,
	// because reward is distributed with a delay. This is not our problem here though.
	if vals := app.GetValidators(); len(vals) > 0 {
		// Deterministically select a proposer from the app's validator committee.
		// We need it so that app does not emit error logs.
		proposer := slices.MinFunc(vals, func(a, b abci.ValidatorUpdate) int { return a.PubKey.Compare(b.PubKey) })
		key, err := crypto.PubKeyFromProto(proposer.PubKey)
		if err != nil {
			return fmt.Errorf("crypto.PubKeyFromProto(): %w", err)
		}
		req.Header.ProposerAddress = key.Address()
	}
	resp, err := app.FinalizeBlock(ctx, req)
	if err != nil {
		return fmt.Errorf("app.FinalizeBlock(): %w", err)
	}
	gigametrics.SetPhase(gigametrics.PhaseStorage)
	gasUsed := finalizeBlockGasUsed(resp)

	gigametrics.SetStoragePhase(gigametrics.StoragePhaseAppCommit)
	if _, err := app.Commit(ctx); err != nil {
		return fmt.Errorf("app.Commit(): %w", err)
	}
	gigametrics.SetStoragePhase(gigametrics.StoragePhaseBookkeeping)
	metrics.GasUsed.Add(gasUsed)
	e.executed.Store(e.executed.Load().Push(atypes.ExecutedBlock{
		Number:  b.Height,
		GasUsed: utils.Clamp[uint64](gasUsed),
	}))
	gigametrics.EndStoragePhase()
	return nil
}

// finalizeBlockGasUsed returns the gas used, as reported by FinalizeBlock.
func finalizeBlockGasUsed(resp *abci.ResponseFinalizeBlock) int64 {
	var total int64
	for _, result := range resp.TxResults {
		if result != nil {
			total += max(0, result.GasUsed)
		}
	}
	return total
}
