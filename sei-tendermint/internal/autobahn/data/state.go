package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
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
	// PushAppHash blocks until block n and its QC are durably persisted,
	// ensuring AppVotes are only issued for data that survives a crash.
	PushAppHash(ctx context.Context, n types.GlobalBlockNumber, hash types.AppHash) error
}

var _ StateAPI = (*State)(nil)

// DataWAL groups the WALs used by State for crash recovery.
// Both fields are always non-nil; when stateDir is None they are no-op persisters.
// This is temporary and will go away once we switch to a proper storage solution.
type DataWAL struct {
	Blocks    *persist.GlobalBlockPersister
	CommitQCs *persist.FullCommitQCPersister
}

// Close shuts down both WALs.
func (dw *DataWAL) Close() error {
	return errors.Join(dw.Blocks.Close(), dw.CommitQCs.Close())
}

// TruncateBefore removes entries fully before n from both WALs in parallel.
// A crash between the two calls just leaves stale entries in one WAL,
// which are harmless on reload.
func (dw *DataWAL) TruncateBefore(n types.GlobalBlockNumber) error {
	return scope.Parallel(func(ps scope.ParallelScope) error {
		ps.Spawn(func() error {
			if err := dw.Blocks.TruncateBefore(n); err != nil {
				return fmt.Errorf("truncate global block WAL: %w", err)
			}
			return nil
		})
		ps.Spawn(func() error {
			if err := dw.CommitQCs.TruncateBefore(n); err != nil {
				return fmt.Errorf("truncate full commitqc WAL: %w", err)
			}
			return nil
		})
		return nil
	})
}

// reconcile fixes cursor inconsistencies between the two WALs that can
// result from crashes during parallel persistence or pruning.
//
// Prefix: advances both WALs to the max of their starting points.
// blocksFirst > qcsFirst can only happen if a prune completed on blocks
// but crashed before completing on QCs (blocks never naturally start
// ahead of QCs since QCs arrive first). In that case, truncating QCs
// to match completes the interrupted prune.
//
// Tail: if blocks WAL extends past QCs WAL, the excess blocks were
// persisted without their corresponding QCs (crash during parallel
// persistence in runPersist). These blocks are untrustworthy — a
// different QC could arrive for those positions — so they are removed.
func (dw *DataWAL) reconcile(committee *types.Committee) error {
	fb := committee.FirstBlock()
	// Fix prefix.
	blocksFirst := dw.Blocks.LoadedFirst()
	qcsFirst := dw.CommitQCs.LoadedFirst()
	reconciled := max(blocksFirst, qcsFirst)
	if reconciled > fb {
		if err := dw.TruncateBefore(reconciled); err != nil {
			return err
		}
	}
	// Fix tail: remove blocks past QCs range. TruncateAfter handles the
	// case where qcEnd-1 is before the first block (removes all).
	qcNext := dw.CommitQCs.Next()
	if dw.Blocks.Next() > qcNext && qcNext > 0 {
		if err := dw.Blocks.TruncateAfter(qcNext - 1); err != nil {
			return fmt.Errorf("truncate blocks tail: %w", err)
		}
	}
	return nil
}

// NewDataWAL constructs both global-block and global-commitqc WALs.
// When stateDir is None, the returned persisters are no-ops.
func NewDataWAL(stateDir utils.Option[string], committee *types.Committee) (*DataWAL, error) {
	blocks, err := persist.NewGlobalBlockPersister(stateDir, committee)
	if err != nil {
		return nil, fmt.Errorf("global block WAL: %w", err)
	}
	commitQCs, err := persist.NewFullCommitQCPersister(stateDir, committee)
	if err != nil {
		_ = blocks.Close()
		return nil, fmt.Errorf("full commitqc WAL: %w", err)
	}
	dw := &DataWAL{
		Blocks:    blocks,
		CommitQCs: commitQCs,
	}
	// Reconcile cursor inconsistency: a crash between the two parallel
	// TruncateBefore calls can leave one WAL truncated while the other
	// still has stale entries. Advance both to the max starting point.
	if err := dw.reconcile(committee); err != nil {
		_ = dw.Close()
		return nil, fmt.Errorf("reconcile WALs: %w", err)
	}
	return dw, nil
}

type appProposalWithTimestamp struct {
	proposal  *types.AppProposal
	timestamp time.Time
}

type inner struct {
	qcs          map[types.GlobalBlockNumber]*types.FullCommitQC      // [first,nextQC)
	blocks       map[types.GlobalBlockNumber]*types.Block             // [first,nextBlock) + subset of [nextBlock,nextQC)
	appProposals map[types.GlobalBlockNumber]appProposalWithTimestamp // [first,nextAppProposal)

	// first <= nextAppProposal <= nextBlockToPersist <= nextBlock <= nextQC
	//
	// This invariant guarantees no race between pruning and persisting:
	// blocks are not eligible for pruning until they have an AppProposal
	// (first <= nextAppProposal), which requires persistence
	// (nextAppProposal <= nextBlockToPersist).
	first              types.GlobalBlockNumber
	nextAppProposal    types.GlobalBlockNumber
	nextBlockToPersist types.GlobalBlockNumber
	nextBlock          types.GlobalBlockNumber
	nextQC             types.GlobalBlockNumber
}

func newInner(committee *types.Committee) *inner {
	first := committee.FirstBlock()
	return &inner{
		qcs:                map[types.GlobalBlockNumber]*types.FullCommitQC{},
		blocks:             map[types.GlobalBlockNumber]*types.Block{},
		appProposals:       map[types.GlobalBlockNumber]appProposalWithTimestamp{},
		first:              first,
		nextAppProposal:    first,
		nextBlockToPersist: first,
		nextBlock:          first,
		nextQC:             first,
	}
}

// skipTo advances all cursors to n, discarding everything before it.
// Used on recovery when the first loaded QC starts past committee.FirstBlock()
// (i.e. data before n was pruned in a previous run).
func (i *inner) skipTo(n types.GlobalBlockNumber) {
	i.first = n
	i.nextAppProposal = n
	i.nextBlockToPersist = n
	i.nextBlock = n
	i.nextQC = n
}

// insertQC verifies and inserts a FullCommitQC into the inner state.
func (i *inner) insertQC(committee *types.Committee, qc *types.FullCommitQC) error {
	if err := qc.Verify(committee); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	gr := qc.QC().GlobalRange(committee)
	if gr.First != i.nextQC {
		return fmt.Errorf("QC gap: expected first=%d, got %d", i.nextQC, gr.First)
	}
	for i.nextQC < gr.Next {
		i.qcs[i.nextQC] = qc
		i.nextQC += 1
	}
	return nil
}

// insertBlock inserts a pre-verified block into the inner state.
// Requires a QC to already be present for block n. Callers must verify
// the block signature before calling.
func (i *inner) insertBlock(committee *types.Committee, n types.GlobalBlockNumber, block *types.Block) error {
	if n < i.first || n >= i.nextQC {
		return nil // outside QC range
	}
	if _, ok := i.blocks[n]; ok {
		return nil // already have it
	}
	qc := i.qcs[n]
	storedGR := qc.QC().GlobalRange(committee)
	want := qc.Headers()[n-storedGR.First].Hash()
	if got := block.Header().Hash(); want != got {
		return fmt.Errorf("block %d header hash mismatch: want %v, got %v", n, want, got)
	}
	i.blocks[n] = block
	return nil
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

func (i *inner) pruneFirst(now time.Time, m *dataMetrics) {
	b := i.blocks[i.first]
	latency := now.Sub(b.Payload().CreatedAt()).Seconds()
	m.Blocks.Prune.ObserveWithWeight(latency, 1)
	m.Txs.Prune.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
	delete(i.appProposals, i.first)
	delete(i.blocks, i.first)
	delete(i.qcs, i.first)
	i.first += 1
}

// State of the chain.
// Contains blocks in global order and proofs of their finality.
type State struct {
	cfg     *Config
	metrics *dataMetrics
	inner   utils.Watch[*inner]
	dataWAL *DataWAL
}

// NewState constructs a new data State.
// dataWAL persists blocks and QCs to WALs for crash recovery and provides
// preloaded data from the previous run. Use NewDataWAL to construct it.
func NewState(cfg *Config, dataWAL *DataWAL) (*State, error) {
	inner := newInner(cfg.Committee)
	// If WAL data starts past committee.FirstBlock() (due to pruning in a
	// previous run), fast-forward all cursors to where data actually starts.
	qcFirst := dataWAL.CommitQCs.LoadedFirst()
	if qcFirst > cfg.Committee.FirstBlock() {
		inner.skipTo(qcFirst)
	}
	// Restore QCs first (they set nextQC), then blocks.
	// All loaded data is verified as if it came from the network.
	for _, qc := range dataWAL.CommitQCs.ConsumeLoaded() {
		if err := inner.insertQC(cfg.Committee, qc); err != nil {
			return nil, fmt.Errorf("load QC from WAL: %w", err)
		}
	}
	for _, lb := range dataWAL.Blocks.ConsumeLoaded() {
		if err := lb.Block.Verify(cfg.Committee); err != nil {
			return nil, fmt.Errorf("load block %d from WAL: %w", lb.Number, err)
		}
		if err := inner.insertBlock(cfg.Committee, lb.Number, lb.Block); err != nil {
			return nil, fmt.Errorf("load block %d from WAL: %w", lb.Number, err)
		}
	}
	// Advance nextBlock through contiguous blocks. Don't use
	// updateNextBlock: stale timestamps would skew metrics.
	for ; inner.blocks[inner.nextBlock] != nil; inner.nextBlock++ {
	}
	// Data loaded from WALs was already persisted in the previous run.
	inner.nextBlockToPersist = inner.nextBlock
	// WAL cursor consistency was resolved by DataWAL.reconcile at construction.
	// Verify the blocks persister cursor is not behind inner.nextBlock.
	if dataWAL.Blocks.Next() < inner.nextBlock {
		return nil, fmt.Errorf("blocks WAL cursor %d behind inner.nextBlock %d after reconciliation",
			dataWAL.Blocks.Next(), inner.nextBlock)
	}
	return &State{
		cfg:     cfg,
		metrics: newDataMetrics(),
		inner:   utils.NewWatch(inner),
		dataWAL: dataWAL,
	}, nil
}

// Committee returns the committee.
func (s *State) Committee() *types.Committee { return s.cfg.Committee }

// PushQC pushes FullCommitQC and a subset of blocks that were finalized by it.
// Pushing the qc and blocks is atomic, so that no unnecessary GetBlock RPCs are issued.
// Even if the qc was already pushed earlier, the blocks are pushed anyway.
func (s *State) PushQC(ctx context.Context, qc *types.FullCommitQC, blocks []*types.Block) error {
	// Wait until QC is needed.
	gr := qc.QC().GlobalRange(s.cfg.Committee)
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
		for n := max(inner.nextBlock, gr.First); n < min(gr.Next, inner.nextQC); n += 1 {
			storedQC := inner.qcs[n]
			storedGR := storedQC.QC().GlobalRange(s.cfg.Committee)
			if b, ok := byHash[storedQC.Headers()[n-storedGR.First].Hash()]; ok {
				if err := inner.insertBlock(s.cfg.Committee, n, b); err != nil {
					return err
				}
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
	// Verify outside the lock to avoid holding it during expensive crypto.
	if err := block.Verify(s.cfg.Committee); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextQC }); err != nil {
			return err
		}
		if err := inner.insertBlock(s.cfg.Committee, n, block); err != nil {
			return err
		}
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
		b := inner.blocks[n]
		qc := inner.qcs[n].QC()
		return &types.GlobalBlock{
			GlobalNumber:  n,
			Timestamp:     qc.Proposal().BlockTimestamp(s.Committee(), n).OrPanic("global block not in QC"),
			Header:        b.Header(),
			Payload:       b.Payload(),
			FinalAppState: qc.Proposal().App(),
		}, nil
	}
	panic("unreachable")
}

// PushAppHash marks blocks up to n as executed. Hash is the execution result.
// Waits for the block to be durably persisted before proceeding.
func (s *State) PushAppHash(ctx context.Context, n types.GlobalBlockNumber, hash types.AppHash) error {
	for inner, ctrl := range s.inner.Lock() {
		if n < inner.nextAppProposal {
			return fmt.Errorf("received app proposal out of order: got %v, want >= %v", n, inner.nextAppProposal)
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextBlockToPersist
		}); err != nil {
			return err
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

// findPruneBoundary returns the highest block number we can prune to
// while keeping entire QC ranges intact. canPrune is called with the end
// of each candidate range; return false to stop. The last QC range
// (qcEnd >= nextAppProposal) is always kept so WALs are never empty.
func (i *inner) findPruneBoundary(committee *types.Committee, canPrune func(qcEnd types.GlobalBlockNumber) bool) types.GlobalBlockNumber {
	target := i.first
	for target < i.nextAppProposal {
		qcEnd := i.qcs[target].QC().GlobalRange(committee).Next
		if qcEnd >= i.nextAppProposal {
			break // last QC range — keep it
		}
		if !canPrune(qcEnd) {
			break
		}
		target = qcEnd
	}
	return target
}

func (s *State) PruneBefore(n types.GlobalBlockNumber) error {
	pruningTime := time.Now()
	for inner, ctrl := range s.inner.Lock() {
		target := inner.findPruneBoundary(s.cfg.Committee, func(qcEnd types.GlobalBlockNumber) bool {
			return qcEnd <= n
		})
		oldFirst := inner.first
		for inner.first < target {
			inner.pruneFirst(pruningTime, s.metrics)
		}
		if inner.first > oldFirst {
			ctrl.Updated()
			if err := s.dataWAL.TruncateBefore(inner.first); err != nil {
				return err
			}
		}
	}
	return nil
}

// runPersist is a background goroutine that persists blocks and QCs to WALs.
// It waits for in-memory data to advance past the persistence cursors, then
// writes QCs and blocks in parallel. QCs are persisted up to nextQC (eagerly),
// blocks up to nextBlock. nextBlockToPersist advances to min(persistedQC, persistedBlock)
// to unblock PushAppHash only when both are durable.
// Errors propagate vertically (kill the component).
func (s *State) runPersist(ctx context.Context) error {
	// Initialize from nextBlockToPersist, not nextQC/nextBlock. PushQC may
	// have been called before Run() (race between p2p startup and Run),
	// advancing nextQC/nextBlock beyond what's been persisted. Starting
	// from nextBlockToPersist ensures we don't skip unpersisted data.
	var persistedQC, persistedBlock types.GlobalBlockNumber
	for inner := range s.inner.Lock() {
		persistedQC = inner.nextBlockToPersist
		persistedBlock = inner.nextBlockToPersist
	}
	for {
		// Wait for unpersisted data and snapshot what needs writing.
		type batch struct {
			qcs      []*types.FullCommitQC
			blocks   []persist.LoadedGlobalBlock
			qcEnd    types.GlobalBlockNumber
			blockEnd types.GlobalBlockNumber
		}
		var b batch
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return persistedQC < inner.nextQC || persistedBlock < inner.nextBlock
			}); err != nil {
				return err
			}
			b.qcEnd = inner.nextQC
			b.blockEnd = inner.nextBlock
			// Collect deduplicated QCs for [persistedQC, nextQC).
			seen := map[types.GlobalBlockNumber]bool{}
			for n := persistedQC; n < inner.nextQC; n++ {
				qc := inner.qcs[n]
				first := qc.QC().GlobalRange(s.cfg.Committee).First
				if !seen[first] {
					seen[first] = true
					b.qcs = append(b.qcs, qc)
				}
			}
			// Collect blocks for [persistedBlock, nextBlock).
			for n := persistedBlock; n < inner.nextBlock; n++ {
				b.blocks = append(b.blocks, persist.LoadedGlobalBlock{
					Number: n,
					Block:  inner.blocks[n],
				})
			}
		}
		// Persist QCs and blocks in parallel.
		if err := scope.Parallel(func(ps scope.ParallelScope) error {
			ps.Spawn(func() error {
				for _, qc := range b.qcs {
					if err := s.dataWAL.CommitQCs.PersistQC(qc); err != nil {
						return fmt.Errorf("persist full commitqc: %w", err)
					}
				}
				return nil
			})
			ps.Spawn(func() error {
				for _, lb := range b.blocks {
					if err := s.dataWAL.Blocks.PersistBlock(lb.Number, lb.Block); err != nil {
						return fmt.Errorf("persist global block %d: %w", lb.Number, err)
					}
				}
				return nil
			})
			return nil
		}); err != nil {
			return err
		}
		persistedQC = b.qcEnd
		persistedBlock = b.blockEnd
		// Advance nextBlockToPersist to where both QCs and blocks are durable.
		newToPersist := min(persistedQC, persistedBlock)
		for inner, ctrl := range s.inner.Lock() {
			if newToPersist > inner.nextBlockToPersist {
				inner.nextBlockToPersist = newToPersist
				ctrl.Updated()
			}
		}
	}
}

func (s *State) runPruning(ctx context.Context, after time.Duration) error {
	pruningTime := time.Now()
	for {
		for inner, ctrl := range s.inner.Lock() {
			oldFirst := inner.first
			// Always keep at least one QC range so that (1) WALs are
			// never empty (inner.first is recoverable on restart) and
			// (2) at least one block remains for AppProposal voting.
			// TODO: a proper fix would not prune until AppQC exists.
			target := inner.findPruneBoundary(s.cfg.Committee, func(qcEnd types.GlobalBlockNumber) bool {
				return pruningTime.Sub(inner.appProposals[qcEnd-1].timestamp) >= after
			})
			for inner.first < target {
				inner.pruneFirst(pruningTime, s.metrics)
			}
			if inner.first > oldFirst {
				ctrl.Updated()
				if err := s.dataWAL.TruncateBefore(inner.first); err != nil {
					return err
				}
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
		scope.SpawnNamed("runPersist", func() error {
			return s.runPersist(ctx)
		})
		if pruneAfter, ok := s.cfg.PruneAfter.Get(); ok {
			scope.SpawnNamed("runPruning", func() error {
				return s.runPruning(ctx, pruneAfter)
			})
		}
		return nil
	})
}
