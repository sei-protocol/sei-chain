package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
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
	// Registry is the authoritative source of committee and stake information.
	Registry *epoch.Registry
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
// The possible WAL states on startup and how they are handled:
//
//	Case  Blocks    QCs       Scenario                          Action
//	1     empty     empty     Fresh start                       no-op
//	2     [a,b]     empty     QCs lost (corruption)             error (statesync needed)
//	3     empty     [X,Y)     Blocks lost (crash)               Prefix: fast-forward blocks.next to X
//	4     [a,b]     [X,Y)     Normal (a=X, b<Y)                 no-op
//	5     [a,b]     [X,Y)     Prune crash: blocks ahead (a>X)   Prefix: truncate QCs to a
//	6     [a,b]     [X,Y)     Prune crash: QCs ahead (a<X)      Prefix: truncate blocks to X
//	7     [a,b]     [X,Y)     Persist crash: blocks past (b>=Y) Tail: truncate blocks to Y
//	8     [a,b]     [X,Y)     QCs ahead (normal, b<Y)           Tail: no-op (blocks catch up)
func (dw *DataWAL) reconcile(firstBlock types.GlobalBlockNumber) error {
	fb := firstBlock
	// Fix tail: remove blocks past QC range.
	qcNext := dw.CommitQCs.Next()
	if qcNext == fb && dw.Blocks.Next() > fb {
		// Blocks exist but QCs WAL is empty — data is corrupted.
		return fmt.Errorf("corrupted WAL: blocks exist but QCs WAL is empty; statesync required to recover")
	}
	if err := dw.Blocks.TruncateAfter(qcNext); err != nil {
		return fmt.Errorf("truncate blocks tail: %w", err)
	}
	// Fix prefix: align both WALs to the later start.
	blocksFirst := dw.Blocks.LoadedFirst()
	qcsFirst := dw.CommitQCs.LoadedFirst()
	reconciled := max(blocksFirst, qcsFirst)
	if reconciled > fb {
		if err := dw.TruncateBefore(reconciled); err != nil {
			return err
		}
	}
	return nil
}

// NewDataWAL constructs both global-block and global-commitqc WALs.
// When stateDir is None, the returned persisters are no-ops.
func NewDataWAL(stateDir utils.Option[string], firstBlock types.GlobalBlockNumber) (*DataWAL, error) {
	blocks, err := persist.NewGlobalBlockPersister(stateDir, firstBlock)
	if err != nil {
		return nil, fmt.Errorf("global block WAL: %w", err)
	}
	commitQCs, err := persist.NewFullCommitQCPersister(stateDir, firstBlock)
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
	if err := dw.reconcile(firstBlock); err != nil {
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
	qcs    map[types.GlobalBlockNumber]*types.FullCommitQC // [first,nextQC)
	blocks map[types.GlobalBlockNumber]*types.Block        // [first,nextBlock) + subset of [nextBlock,nextQC)
	// appProposal[n] contains appProposal block >=n.
	appProposals map[types.GlobalBlockNumber]appProposalWithTimestamp // [first,nextAppProposal)

	// blockHashes is a hash → height index mirroring blocks. Maintained
	// in lockstep with blocks via insertBlock / pruneFirst, so it covers
	// exactly the same retain window without a separate prune cursor or
	// startup warmup. Powers BlockByHash.
	//
	// TODO(autobahn): remove once a writer is wired into block execution
	// that populates sei-db/ledger_db/block.BlockDB. BlockDB has a built-in
	// hash index that survives process restart and lives outside Autobahn's
	// RetainHeight pruning, making this in-memory index obsolete.
	blockHashes map[types.BlockHeaderHash]types.GlobalBlockNumber

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

func newInner(firstBlock types.GlobalBlockNumber) *inner {
	first := firstBlock
	return &inner{
		qcs:                map[types.GlobalBlockNumber]*types.FullCommitQC{},
		blocks:             map[types.GlobalBlockNumber]*types.Block{},
		appProposals:       map[types.GlobalBlockNumber]appProposalWithTimestamp{},
		blockHashes:        map[types.BlockHeaderHash]types.GlobalBlockNumber{},
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
// Accepts QCs whose range starts at or before nextQC (partially pruned
// prefix is silently skipped). Rejects gaps where gr.First > nextQC.
func (i *inner) insertQC(ep *epoch.Registry, qc *types.FullCommitQC) error {
	e, err := ep.EpochForProposal(qc.QC().Proposal())
	if err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	if err := qc.Verify(e); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	gr := qc.QC().GlobalRange()
	if gr.Next <= i.nextQC {
		return nil // fully behind, skip
	}
	if gr.First > i.nextQC {
		return fmt.Errorf("QC gap: expected first<=%d, got %d", i.nextQC, gr.First)
	}
	for i.nextQC < gr.Next {
		i.qcs[i.nextQC] = qc
		i.nextQC++
	}
	return nil
}

// insertBlock inserts a pre-verified block into the inner state.
// Requires a QC to already be present for block n. Callers must verify
// the block signature before calling.
//
// insertBlock does NOT advance nextBlock — callers should call
// updateNextBlock after inserting one or more blocks. This separation
// allows batch insertion (e.g. PushQC inserts multiple blocks, then
// advances nextBlock once).
func (i *inner) insertBlock(committee *types.Committee, n types.GlobalBlockNumber, block *types.Block) error {
	if n < i.first || n >= i.nextQC {
		return nil // outside QC range
	}
	if _, ok := i.blocks[n]; ok {
		return nil // already have it
	}
	qc := i.qcs[n]
	storedGR := qc.QC().GlobalRange()
	want := qc.Headers()[n-storedGR.First].Hash()
	got := block.Header().Hash()
	if want != got {
		return fmt.Errorf("block %d header hash mismatch: want %v, got %v", n, want, got)
	}
	i.blocks[n] = block
	i.blockHashes[got] = n
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
	delete(i.blockHashes, b.Header().Hash())
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
	inner := newInner(cfg.Registry.FirstBlock())
	// Fast-forward cursors to where data starts. Use blocks as golden:
	// per-block pruning may split a QC range, so blocks determine where
	// useful data starts. QCs before that are kept for verification but
	// don't set inner.first.
	blocksFirst := dataWAL.Blocks.LoadedFirst()
	qcFirst := dataWAL.CommitQCs.LoadedFirst()
	dataFirst := max(blocksFirst, qcFirst)
	if dataFirst > cfg.Registry.FirstBlock() {
		inner.skipTo(dataFirst)
	}
	// Restore QCs. insertQC handles partially pruned QCs (range starts
	// before inner.first) by skipping the pruned prefix.
	for _, qc := range dataWAL.CommitQCs.ConsumeLoaded() {
		if err := inner.insertQC(cfg.Registry, qc); err != nil {
			return nil, fmt.Errorf("load QC from WAL: %w", err)
		}
	}
	// Restore blocks. Verify contiguity as defense in depth.
	expectedBlock := inner.first
	for _, lb := range dataWAL.Blocks.ConsumeLoaded() {
		if lb.Number < inner.first || lb.Number >= inner.nextQC {
			continue // outside QC range (stale from reconcile)
		}
		if lb.Number != expectedBlock {
			return nil, fmt.Errorf("block gap in WAL: expected %d, got %d", expectedBlock, lb.Number)
		}
		expectedBlock = lb.Number + 1
		qc := inner.qcs[lb.Number]
		e, err := cfg.Registry.EpochForProposal(qc.QC().Proposal())
		if err != nil {
			return nil, fmt.Errorf("unknown epoch: %w", err)
		}
		committee := e.Committee()
		if err := lb.Block.Verify(committee); err != nil {
			return nil, fmt.Errorf("load block %d from WAL: %w", lb.Number, err)
		}
		if err := inner.insertBlock(committee, lb.Number, lb.Block); err != nil {
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

// Registry returns the epoch registry.
func (s *State) Registry() *epoch.Registry { return s.cfg.Registry }

// PushQC pushes FullCommitQC and a subset of blocks that were finalized by it.
// Pushing the qc and blocks is atomic, so that no unnecessary GetBlock RPCs are issued.
// Even if the qc was already pushed earlier, the blocks are pushed anyway.
func (s *State) PushQC(ctx context.Context, qc *types.FullCommitQC, blocks []*types.Block) error {
	// Wait until QC is needed.
	ep, err := s.cfg.Registry.EpochForProposal(qc.QC().Proposal())
	if err != nil {
		return fmt.Errorf("unknown epoch: %w", err)
	}
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
		if err := qc.Verify(ep); err != nil {
			return fmt.Errorf("qc.Verify(): %w", err)
		}
	}
	byHash := map[types.BlockHeaderHash]*types.Block{}
	committee := ep.Committee()
	for _, b := range blocks {
		byHash[b.Header().Hash()] = b
		if err := b.Verify(committee); err != nil {
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
			storedGR := storedQC.QC().GlobalRange()
			storedEp, err := s.cfg.Registry.EpochForProposal(storedQC.QC().Proposal())
			if err != nil {
				return fmt.Errorf("unknown epoch: %w", err)
			}
			storedCommittee := storedEp.Committee()
			if b, ok := byHash[storedQC.Headers()[n-storedGR.First].Hash()]; ok {
				if err := inner.insertBlock(storedCommittee, n, b); err != nil {
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
	// Verify outside the lock against all window committees. A block may satisfy
	// multiple committees during an epoch transition; we collect them all so the
	// in-lock re-verify can be skipped when the authoritative epoch is already covered.
	preEps, err := s.cfg.Registry.VerifyInWindow(block.Verify)
	if err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextQC }); err != nil {
			return err
		}
		qc := inner.qcs[n]
		if qc == nil {
			// Block arrived after pruning; drop silently so the sender keeps delivering future blocks.
			return nil
		}
		blockEp, err := s.cfg.Registry.EpochForProposal(qc.QC().Proposal())
		if err != nil {
			return fmt.Errorf("unknown epoch: %w", err)
		}
		if !epochInSet(blockEp, preEps) {
			return fmt.Errorf("block.Verify(): signed by epoch %d, not in window", blockEp.EpochIndex())
		}
		if err := inner.insertBlock(blockEp.Committee(), n, block); err != nil {
			return err
		}
		inner.updateNextBlock(s.metrics)
		ctrl.Updated()
	}
	return nil
}

func epochInSet(ep *types.Epoch, set []*types.Epoch) bool {
	for _, e := range set {
		if e.EpochIndex() == ep.EpochIndex() {
			return true
		}
	}
	return false
}

// NextBlock returns the index of the next block to be pushed.
func (s *State) NextBlock() types.GlobalBlockNumber {
	for inner := range s.inner.Lock() {
		return inner.nextBlock
	}
	panic("unreachable")
}

// GlobalBlockByHash returns the finalized GlobalBlock whose stored header
// hashes to the given value, or None if no such block is currently in the
// retained range. The lookup-and-construct happens under a single lock so
// the returned block matches the looked-up hash atomically — pruning can't
// change which height a hash maps to between the index check and the block
// construction. Tracks the same retain window as Block / GlobalBlock since
// the hash index is maintained in lockstep by insertBlock / pruneFirst.
//
// Returns an error in the signature for forward-compat with the eventual
// switch to sei-db/ledger_db/block.BlockDB.GetBlockByHash. Today's
// in-memory implementation never errors.
//
// TODO(autobahn): when BlockDB is wired, take a ctx parameter and narrow
// the error contract — db-internal errors should surface by shutting down
// the persistence background task (matching how persistence handles errors
// today), so the query path's error stays bounded to context.Canceled.
func (s *State) GlobalBlockByHash(hash types.BlockHeaderHash) (utils.Option[*types.GlobalBlock], error) {
	for inner := range s.inner.Lock() {
		n, ok := inner.blockHashes[hash]
		if !ok {
			return utils.None[*types.GlobalBlock](), nil
		}
		return utils.Some(inner.globalBlockAt(n)), nil
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

// globalBlockAt assembles the GlobalBlock at height n from inner state.
// Caller must have verified n is in [inner.first, inner.nextBlock); n
// outside that range nil-derefs on inner.blocks[n] / inner.qcs[n].
func (i *inner) globalBlockAt(n types.GlobalBlockNumber) *types.GlobalBlock {
	b := i.blocks[n]
	qc := i.qcs[n].QC()
	return &types.GlobalBlock{
		GlobalNumber:  n,
		Timestamp:     qc.Proposal().BlockTimestamp(n).OrPanic("global block not in QC"),
		Header:        b.Header(),
		Payload:       b.Payload(),
		FinalAppState: qc.Proposal().App(),
	}
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
		return inner.globalBlockAt(n), nil
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
			inner.qcs[n].QC().Proposal().EpochIndex(),
		)
		t := time.Now()
		apt := appProposalWithTimestamp{
			proposal:  proposal,
			timestamp: t,
		}
		// TODO(gprusak): this will be problematic on restart,
		// nextAppProposal should be initiated wrt current application height,
		// so that we don't iterate over all blocks in storage on startup.
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

func (i *inner) nextToExecute(lane types.LaneID) types.BlockNumber {
	// TODO(gprusak): decide whether 0 is a good result in this case in general.
	if i.first == i.nextAppProposal {
		return 0
	}
	n := i.nextAppProposal - 1
	r := i.qcs[n].QC().LaneRange(lane)
	// TODO: this header can be actually extracted from FullCommitQC, so consider moving all this logic there.
	h := i.blocks[n].Header()
	x := lane.Compare(h.Lane())
	// NOTE: here we assume the specific ordering of lane blocks in the CommitQC:
	// TODO(gprusak): move this logic closer to CommitQC
	switch {
	case x < 0:
		return r.Next()
	case x > 0:
		return r.First()
	default:
		return h.BlockNumber() + 1
	}
}

// Waits until lane block n is executed, returns the next block of this lane to be executed (>n)
func (s *State) WaitUntilExecuted(ctx context.Context, lane types.LaneID, n types.BlockNumber) (types.BlockNumber, error) {
	for inner, ctrl := range s.inner.Lock() {
		for {
			if next := inner.nextToExecute(lane); n < next {
				return next, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return 0, err
			}
		}
	}
	panic("unreachable")
}

// PruneBefore removes blocks, QCs, and AppProposals before retainFrom.
// Blocks at retainFrom and above are kept. Per-block pruning may split
// a QC range; this is handled on recovery (NewState skips partial QC prefixes).
func (s *State) PruneBefore(retainFrom types.GlobalBlockNumber) error {
	pruningTime := time.Now()
	truncateTo := utils.None[types.GlobalBlockNumber]()
	for inner, ctrl := range s.inner.Lock() {
		// Can only prune executed blocks (those with AppProposals).
		firstToKeep := min(retainFrom, inner.nextAppProposal)
		if firstToKeep <= inner.first {
			return nil
		}
		// Keep at least one entry so WALs are never empty on restart.
		for inner.first+1 < firstToKeep {
			inner.pruneFirst(pruningTime, s.metrics)
		}
		ctrl.Updated()
		truncateTo = utils.Some(inner.first)
	}
	// Truncate WALs outside the lock to avoid holding it during disk I/O.
	if n, ok := truncateTo.Get(); ok {
		return s.dataWAL.TruncateBefore(n)
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
				first := qc.QC().GlobalRange().First
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
		truncateTo := utils.None[types.GlobalBlockNumber]()
		for inner, ctrl := range s.inner.Lock() {
			// Prune blocks old enough. Keep at least one entry.
			// Per-block pruning may split QC ranges; handled on recovery.
			// TODO: a proper fix would not prune until AppQC exists.
			pruned := false
			for inner.first+1 < inner.nextAppProposal && pruningTime.Sub(inner.appProposals[inner.first].timestamp) >= after {
				inner.pruneFirst(pruningTime, s.metrics)
				pruned = true
			}
			if pruned {
				ctrl.Updated()
				truncateTo = utils.Some(inner.first)
			}
			// Wait for at least 2 entries before retrying. Without +1,
			// the loop would spin when only one entry remains (kept by
			// the +1 guard above).
			if err := ctrl.WaitUntil(ctx, func() bool { return inner.first+1 < inner.nextAppProposal }); err != nil {
				return err
			}
			pruningTime = inner.appProposals[inner.first].timestamp.Add(after)
		}
		// Truncate WALs outside the lock to avoid holding it during disk I/O.
		if n, ok := truncateTo.Get(); ok {
			if err := s.dataWAL.TruncateBefore(n); err != nil {
				return err
			}
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
