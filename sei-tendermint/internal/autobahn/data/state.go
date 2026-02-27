package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

const blocksCacheSize = 4000

// ErrNotFound is returned when the resource is not found.
var ErrNotFound = errors.New("not found")

// ErrPruned is returned when the resource has been is pruned.
var ErrPruned = errors.New("pruned")

// Config is the config for the data State.
type Config struct {
	// Committee.
	Committee *types.Committee
	// PruneAfter is the duration after which the state prunes executed blocks.
	PruneAfter utils.Option[time.Duration]
}

// StateAPI is the interface of the State for consuming global blocks
// and reporting AppHashes.
type StateAPI interface {
	prometheus.Collector
	GlobalBlock(ctx context.Context, n types.GlobalBlockNumber) (*types.GlobalBlock, error)
	PushAppHash(n types.GlobalBlockNumber, hash types.AppHash) error
}

var _ StateAPI = (*State)(nil)

// BlockStore represents a persistent storage for global blocks.
type BlockStore interface {
	WriteGlobalBlockToWAL(block *types.GlobalBlock) error
}

type appProposalWithTimestamp struct {
	proposal  *types.AppProposal
	timestamp time.Time
}

type inner struct {
	qcs          map[types.GlobalBlockNumber]*types.FullCommitQC      // [first,nextQC)
	blocks       map[types.GlobalBlockNumber]*types.Block             // [first,nextBlock) + subset of [nextBlock,nextQC)
	appProposals map[types.GlobalBlockNumber]appProposalWithTimestamp // [first,nextAppProposal)

	// first <= nextAppProposal <= nextBlock <= nextQC
	first           types.GlobalBlockNumber
	nextAppProposal types.GlobalBlockNumber
	nextBlock       types.GlobalBlockNumber
	nextQC          types.GlobalBlockNumber
}

func newInner() *inner {
	return &inner{
		qcs:             map[types.GlobalBlockNumber]*types.FullCommitQC{},
		blocks:          map[types.GlobalBlockNumber]*types.Block{},
		appProposals:    map[types.GlobalBlockNumber]appProposalWithTimestamp{},
		first:           0,
		nextAppProposal: 0,
		nextBlock:       0,
		nextQC:          0,
	}
}

func (i *inner) updateNextBlock(m *dataMetrics) {
	t := time.Now()
	for {
		b, ok := i.blocks[i.nextBlock]
		if !ok {
			return
		}
		i.nextBlock += 1
		latency := t.Sub(b.Payload().CreatedAt()).Seconds()
		m.Blocks.Receive.ObserveWithWeight(latency, 1)
		m.Txs.Receive.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
	}
}

// State of the chain.
// Contains blocks in global order and proofs of their finality.
type State struct {
	cfg        *Config
	metrics    *dataMetrics
	inner      utils.Watch[*inner]
	blockStore utils.Option[BlockStore]
}

// NewState constructs a new data State.
func NewState(cfg *Config, blockStore utils.Option[BlockStore]) *State {
	return &State{
		cfg:        cfg,
		metrics:    newDataMetrics(),
		inner:      utils.NewWatch(newInner()),
		blockStore: blockStore,
	}
}

// Committee returns the committee.
func (s *State) Committee() *types.Committee { return s.cfg.Committee }

// PushQC pushes FullCommitQC and a subset of blocks that were finalized by it.
// Pushing the qc and blocks is atomic, so that no unnecessary GetBlock RPCs are issued.
// Even if the qc was already pushed earlier, the blocks are pushed anyway.
func (s *State) PushQC(ctx context.Context, qc *types.FullCommitQC, blocks []*types.Block) error {
	// Wait until QC is needed.
	gr := qc.QC().GlobalRange()
	needQC, err := func() (bool, error) {
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return gr.First <= inner.nextQC && gr.First < inner.nextAppProposal+blocksCacheSize
			}); err != nil {
				return false, err
			}
			return inner.nextQC == gr.First, nil
		}
		panic("unreachable")
	}()
	if err != nil {
		return err
	}
	// Verify data.
	if needQC {
		if err := qc.Verify(s.cfg.Committee); err != nil {
			return fmt.Errorf("qc.Verify(): %w", err)
		}
	}
	byHash := map[types.BlockHeaderHash]*types.Block{}
	for _, b := range blocks {
		byHash[b.Header().Hash()] = b
		if err := b.Verify(s.cfg.Committee); err != nil {
			return fmt.Errorf("b.Verify(): %w", err)
		}
	}
	// Atomically insert QC and blocks.
	for inner, ctrl := range s.inner.Lock() {
		if needQC {
			for inner.nextQC < gr.Next {
				inner.qcs[inner.nextQC] = qc
				inner.nextQC += 1
			}
			ctrl.Updated()
		}
		if len(byHash) == 0 {
			break
		}
		// Match blocks against stored (already verified) QC headers.
		// Cap at inner.nextQC: we have no verified QC beyond that point.
		for n := max(inner.nextBlock, gr.First); n < min(gr.Next, inner.nextQC); n += 1 {
			if _, ok := inner.blocks[n]; ok {
				continue
			}
			storedQC := inner.qcs[n]
			storedGR := storedQC.QC().GlobalRange()
			if b, ok := byHash[storedQC.Headers()[n-storedGR.First].Hash()]; ok {
				inner.blocks[n] = b
			}
		}
		ctrl.Updated()
		inner.updateNextBlock(s.metrics)
	}
	return nil
}

// QC returns the FullCommitQC proving finality of the block n.
func (s *State) QC(ctx context.Context, n types.GlobalBlockNumber) (*types.FullCommitQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextQC
		}); err != nil {
			return nil, err
		}
		if n < inner.first {
			return nil, ErrPruned
		}
		return inner.qcs[n], nil
	}
	panic("unreachable")
}

// PushBlock pushes block to the state.
// Waits until the block header is available.
func (s *State) PushBlock(ctx context.Context, n types.GlobalBlockNumber, block *types.Block) error {
	if err := block.Verify(s.cfg.Committee); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextQC }); err != nil {
			return err
		}
		// Early exit if we already have the block.
		if n < inner.nextBlock {
			return nil
		}
		if _, ok := inner.blocks[n]; ok {
			return nil
		}
		qc := inner.qcs[n]
		want := qc.Headers()[n-qc.QC().GlobalRange().First].Hash()
		if got := block.Header().Hash(); want != got {
			return fmt.Errorf("block header hash mismatch: want %v, got %v", want, got)
		}
		inner.blocks[n] = block
		inner.updateNextBlock(s.metrics)
		ctrl.Updated()
	}
	return nil
}

// NextBlock returns the index of the next block to be pushed.
func (s *State) NextBlock() types.GlobalBlockNumber {
	for inner := range s.inner.Lock() {
		return inner.nextBlock
	}
	panic("unreachable")
}

// Block returns the block with the given global number.
// This function is used for syncing - GlobalBlock can be derived from Block and FullCommitQC,
// which have to be fetched upfront anyway.
func (s *State) Block(ctx context.Context, n types.GlobalBlockNumber) (*types.Block, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextBlock
		}); err != nil {
			return nil, err
		}
		if n < inner.first {
			return nil, ErrPruned
		}
		return inner.blocks[n], nil
	}
	panic("unreachable")
}

// TryBlock returns the block with the given global number.
// Returns ErrPruned if the block has already been pruned.
// Returns ErrNotFound if the block is not available yet.
func (s *State) TryBlock(n types.GlobalBlockNumber) (*types.Block, error) {
	for inner := range s.inner.Lock() {
		if n < inner.first {
			return nil, ErrPruned
		}
		b, ok := inner.blocks[n]
		if !ok {
			return nil, ErrNotFound
		}
		return b, nil
	}
	panic("unreachable")
}

// GlobalBlock returns the block with the given global number.
// Returns ErrPruned if the block has already been pruned.
func (s *State) GlobalBlock(ctx context.Context, n types.GlobalBlockNumber) (*types.GlobalBlock, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextBlock
		}); err != nil {
			return nil, err
		}
		if n < inner.first {
			return nil, ErrPruned
		}
		return &types.GlobalBlock{
			GlobalNumber:  n,
			Header:        inner.blocks[n].Header(),
			Payload:       inner.blocks[n].Payload(),
			FinalAppState: inner.qcs[n].QC().Proposal().App(),
		}, nil
	}
	panic("unreachable")
}

// PushAppHash marks blocks up to n as executed. Hash is the execution result.
func (s *State) PushAppHash(n types.GlobalBlockNumber, hash types.AppHash) error {
	for inner, ctrl := range s.inner.Lock() {
		if n < inner.nextAppProposal {
			return fmt.Errorf("received app proposal out of order: got %v, want >= %v", n, inner.nextAppProposal)
		}
		if n >= inner.nextBlock {
			return fmt.Errorf("block %v is not available yet", n)
		}
		proposal := types.NewAppProposal(
			n,
			inner.qcs[n].QC().Proposal().Index(),
			hash,
		)
		t := time.Now()
		apt := appProposalWithTimestamp{
			proposal:  proposal,
			timestamp: t,
		}
		for inner.nextAppProposal <= n {
			b := inner.blocks[inner.nextAppProposal]
			latency := t.Sub(b.Payload().CreatedAt()).Seconds()
			s.metrics.Blocks.Execute.ObserveWithWeight(latency, 1)
			s.metrics.Txs.Execute.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
			inner.appProposals[inner.nextAppProposal] = apt
			inner.nextAppProposal += 1
		}
		ctrl.Updated()
	}
	return nil
}

// AppProposal returns the lowest AppProposal containing the block n.
// WARNING: currently we do not enforce all blocks to have AppProposal, therefore
// an AppProposal for a later block might be returned instead.
func (s *State) AppProposal(ctx context.Context, n types.GlobalBlockNumber) (*types.AppProposal, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextAppProposal }); err != nil {
			return nil, err
		}
		if n < inner.first {
			return nil, ErrPruned
		}
		return inner.appProposals[n].proposal, nil
	}
	panic("unreachable")
}

func (s *State) runPruning(ctx context.Context, after time.Duration) error {
	pruningTime := time.Now()
	for {
		for inner, ctrl := range s.inner.Lock() {
			// Prune entries at pruningTime.
			for inner.first < inner.nextAppProposal && pruningTime.Sub(inner.appProposals[inner.first].timestamp) >= after {
				b := inner.blocks[inner.first]
				latency := pruningTime.Sub(b.Payload().CreatedAt()).Seconds()
				s.metrics.Blocks.Prune.ObserveWithWeight(latency, 1)
				s.metrics.Txs.Prune.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
				delete(inner.appProposals, inner.first)
				delete(inner.blocks, inner.first)
				delete(inner.qcs, inner.first)
				inner.first += 1
				ctrl.Updated()
			}
			// Compute the next pruning time.
			if err := ctrl.WaitUntil(ctx, func() bool { return inner.first < inner.nextAppProposal }); err != nil {
				return err
			}
			pruningTime = inner.appProposals[inner.first].timestamp.Add(after)
		}
		// Wait until the next pruning time.
		if err := utils.SleepUntil(ctx, pruningTime); err != nil {
			return err
		}
	}
}

// Run runs the background tasks of the data State.
// TODO(gprusak): add support for starting execution from non-zero commit QC.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		if pruneAfter, ok := s.cfg.PruneAfter.Get(); ok {
			scope.SpawnNamed("runPruning", func() error {
				return s.runPruning(ctx, pruneAfter)
			})
		}
		if blockStore, ok := s.blockStore.Get(); ok {
			// TODO(gprusak): writing to blockStore is best effort now,
			// since the blocks may get pruned before they are written.
			scope.SpawnNamed("blockStore", func() error {
				for idx := types.GlobalBlockNumber(0); ; idx += 1 {
					b, err := s.GlobalBlock(ctx, idx)
					if err != nil {
						return fmt.Errorf("s.Blocks(): %w", err)
					}
					if err := blockStore.WriteGlobalBlockToWAL(b); err != nil {
						return fmt.Errorf("s.blockStore.WriteGlobalBlock(): %w", err)
					}
				}
			})
		}
		return nil
	})
}
