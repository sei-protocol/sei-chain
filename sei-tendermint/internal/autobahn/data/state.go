package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data/metrics"
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
	GlobalBlock(ctx context.Context, n types.GlobalBlockNumber) (*types.GlobalBlock, error)
	// PushAppHash blocks until block n and its QC are durably persisted,
	// ensuring AppVotes are only issued for data that survives a crash.
	PushAppHash(ctx context.Context, n types.GlobalBlockNumber, hash types.AppHash) error
}

var _ StateAPI = (*State)(nil)

// blockEntry is a (number, block) pair collected in runPersist batches.
type blockEntry struct {
	n   types.GlobalBlockNumber
	blk *types.Block
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

	// blockHashes is a hash → height index for GlobalBlockByHash. Maintained
	// in lockstep with blocks via insertBlock / pruneFirst, covering the same
	// retain window.
	//
	// TODO: replace with blockDB.ReadBlockByHash once BlockDB exposes the
	// primary GlobalBlockNumber alongside the block on a secondary-key lookup.
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
func (i *inner) insertQC(registry *epoch.Registry, qc *types.FullCommitQC) error {
	e, ok := registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
	if !ok {
		return fmt.Errorf("unknown epoch_index %d", qc.QC().Proposal().EpochIndex())
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
	if qc == nil {
		// Evicted after execution; nothing to insert.
		return nil
	}
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

func (i *inner) updateNextBlock(m *metrics.Metrics) {
	t := time.Now()
	for {
		b, ok := i.blocks[i.nextBlock]
		if !ok {
			return
		}
		i.nextBlock += 1
		latency := t.Sub(b.Payload().CreatedAt()).Seconds()
		m.Blocks.Receive.Observe(latency)
		m.Txs.Receive.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
	}
}

func (i *inner) pruneFirst(now time.Time, m *metrics.Metrics) {
	b := i.blocks[i.first]
	latency := now.Sub(b.Payload().CreatedAt()).Seconds()
	m.Blocks.Prune.Observe(latency)
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
	metrics *metrics.Metrics
	inner   utils.Watch[*inner]
	blockDB types.BlockDB
	// Captured in NewState (before any goroutines start) and consumed once at
	// the top of Run to seed runPersist. Capturing here avoids a race where a
	// concurrent PushQC (called via an inbound connection after the transport
	// starts but before Run's scope begins) could advance inner.nextQC/nextBlock
	// past unpersisted data, causing runPersist to skip writing those entries.
	blockDBPersistedQC    types.GlobalBlockNumber
	blockDBPersistedBlock types.GlobalBlockNumber
}

// NewState constructs a data State, replaying persisted state from blockDB.
// Use memblock.NewBlockDB() for an in-memory store (testing / no persistent dir).
// The caller owns blockDB and must close it after State.Run returns (nodeImpl
// owns this in production); State never closes it.
// TODO(gprusak): add support for starting execution from non-zero commit QC.
func NewState(cfg *Config, blockDB types.BlockDB) (*State, error) {
	s := &State{
		cfg:     cfg,
		metrics: metrics.Get(),
		inner:   utils.NewWatch(newInner(cfg.Registry.FirstBlock())),
		blockDB: blockDB,
	}
	if err := s.loadFromBlockDB(blockDB); err != nil {
		return nil, fmt.Errorf("loadFromBlockDB: %w", err)
	}
	for inner := range s.inner.Lock() {
		s.blockDBPersistedQC = inner.nextQC
		s.blockDBPersistedBlock = inner.nextBlock
	}
	return s, nil
}

// loadFromBlockDB replays QCs and blocks from blockDB into s.inner.
// Called from NewState before any goroutines are spawned; the lock is acquired
// only to satisfy the Watch API.
func (s *State) loadFromBlockDB(blockDB types.BlockDB) error {
	for in := range s.inner.Lock() {
		// Restore QCs from BlockDB. On the first QC, skipTo its GlobalRange.First
		// to advance past any pruned prefix (a straddling QC starts before the
		// prune boundary). Subsequent QCs must be consecutive — insertQC errors
		// on any gap.
		var err error
		err = func() error {
			it, err := blockDB.QCs(false)
			if err != nil {
				return fmt.Errorf("open QC iterator: %w", err)
			}
			defer func() { _ = it.Close() }()
			for {
				ok, err := it.Next()
				if err != nil || !ok {
					return err
				}
				qc, err := it.QC()
				if err != nil {
					return err
				}
				if in.first == in.nextQC {
					gr := qc.QC().GlobalRange()
					if gr.First < in.nextQC {
						return fmt.Errorf("QC in BlockDB predates committee genesis %d: got %d", in.nextQC, gr.First)
					}
					in.skipTo(gr.First)
				}
				if err := in.insertQC(s.cfg.Registry, qc); err != nil {
					return fmt.Errorf("load QC from BlockDB: %w", err)
				}
			}
		}()
		if err != nil {
			return err
		}

		// Restore blocks from BlockDB. The first retained block drives inner.first —
		// a straddling QC may start before the prune boundary, so QC data alone
		// cannot determine where blocks begin. insertBlock's n >= nextQC guard
		// enforces the no-block-without-QC invariant.
		err = func() error {
			it2, err := blockDB.Blocks(false)
			if err != nil {
				return fmt.Errorf("open block iterator: %w", err)
			}
			defer func() { _ = it2.Close() }()
			var nextExpect types.GlobalBlockNumber
			for {
				ok, err := it2.Next()
				if err != nil || !ok {
					return err
				}
				n := it2.Number()
				if n >= in.nextQC {
					return fmt.Errorf("block %d in BlockDB has no QC coverage (nextQC=%d)", n, in.nextQC)
				}
				// No blocks loaded yet (insertBlock does not advance nextBlock
				// until after this loop). Mirrors the QC pass's first==nextQC check.
				if len(in.blocks) == 0 {
					if n < in.first {
						return fmt.Errorf("block %d in BlockDB predates first QC start %d", n, in.first)
					}
					// Drop QC entries below the first retained block (straddling-QC
					// case: QC started before the prune boundary so the QC pass
					// populated in.qcs for [qcFirst, n); those entries are unreachable
					// via pruneFirst which starts at first=n).
					for k := in.first; k < n; k++ {
						delete(in.qcs, k)
					}
					in.first = n
					in.nextBlock = n
					in.nextAppProposal = n
					nextExpect = n
				}
				// updateNextBlock only advances nextBlock through contiguous present
				// entries, so runPersist always writes [persistedBlock, nextBlock)
				// fully populated. A gap here means BlockDB corruption.
				if n != nextExpect {
					return fmt.Errorf("block gap in BlockDB: expected %d, got %d", nextExpect, n)
				}
				nextExpect++
				blk, err := it2.Block()
				if err != nil {
					return err
				}
				qc := in.qcs[n]
				e, ok := s.cfg.Registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
				if !ok {
					return fmt.Errorf("unknown epoch_index %d", qc.QC().Proposal().EpochIndex())
				}
				committee := e.Committee()
				if err := blk.Verify(committee); err != nil {
					return fmt.Errorf("verify block %d from BlockDB: %w", n, err)
				}
				if err := in.insertBlock(committee, n, blk); err != nil {
					return fmt.Errorf("insert block %d from BlockDB: %w", n, err)
				}
			}
		}()
		if err != nil {
			return err
		}

		// Advance nextBlock through contiguous loaded blocks. Don't use
		// updateNextBlock: stale timestamps would skew metrics.
		for ; in.blocks[in.nextBlock] != nil; in.nextBlock++ {
		}
		// Data loaded from BlockDB was already durably persisted.
		in.nextBlockToPersist = in.nextBlock
	}
	return nil
}

// Registry returns the epoch registry.
func (s *State) Registry() *epoch.Registry { return s.cfg.Registry }

// PushQC pushes FullCommitQC and a subset of blocks that were finalized by it.
// Pushing the qc and blocks is atomic, so that no unnecessary GetBlock RPCs are issued.
// Even if the qc was already pushed earlier, the blocks are pushed anyway.
func (s *State) PushQC(ctx context.Context, qc *types.FullCommitQC, blocks []*types.Block) error {
	// Wait until QC is needed.
	ep, ok := s.cfg.Registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
	if !ok {
		return fmt.Errorf("unknown epoch_index %d", qc.QC().Proposal().EpochIndex())
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
			storedEp, ok := s.cfg.Registry.EpochByIndex(storedQC.QC().Proposal().EpochIndex())
			if !ok {
				return fmt.Errorf("unknown epoch_index %d", storedQC.QC().Proposal().EpochIndex())
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
		if qc, ok := inner.qcs[n]; ok {
			return qc, nil
		}
		// Evicted from memory after persist; fall through to BlockDB.
	}
	return s.qcFromDB(n)
}

// PushBlock pushes block to the state.
// The QC for n must already be present (guaranteed by PushQC ordering), unless
// it was already executed and evicted from memory by runPersist — in that case
// the block is dropped silently.
func (s *State) PushBlock(ctx context.Context, n types.GlobalBlockNumber, block *types.Block) error {
	var epochIdx types.EpochIndex
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextQC }); err != nil {
			return err
		}
		if n < inner.first {
			// Block arrived after pruning; drop silently so the sender keeps delivering future blocks.
			return nil
		}
		qc := inner.qcs[n]
		if qc == nil {
			// QC was evicted after the height was executed (n < nextAppProposal).
			// The block is no longer needed in memory.
			return nil
		}
		epochIdx = qc.QC().Proposal().EpochIndex()
	}
	ep, ok := s.cfg.Registry.EpochByIndex(epochIdx)
	if !ok {
		return fmt.Errorf("unknown epoch_index %d", epochIdx)
	}
	// Verify outside the lock against the known epoch.
	if err := block.Verify(ep.Committee()); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if n < inner.first {
			return nil
		}
		if inner.qcs[n] == nil {
			return nil
		}
		if err := inner.insertBlock(ep.Committee(), n, block); err != nil {
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

// GlobalBlockByHash returns the finalized GlobalBlock whose stored header
// hashes to the given value, or None if no such block is currently in the
// retained range. Non-blocking. Falls back to BlockDB when the entry was
// evicted from memory after persist.
func (s *State) GlobalBlockByHash(hash types.BlockHeaderHash) (utils.Option[*types.GlobalBlock], error) {
	for inner := range s.inner.Lock() {
		n, ok := inner.blockHashes[hash]
		if !ok {
			break // may still be in BlockDB after eviction
		}
		b, hasB := inner.blocks[n]
		qc, hasQC := inner.qcs[n]
		if hasB && hasQC {
			return utils.Some(assembleGlobalBlock(n, b, qc)), nil
		}
		break // hash known but entries evicted; load from BlockDB
	}
	return s.globalBlockByHashFromDB(hash)
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
		if b, ok := inner.blocks[n]; ok {
			return b, nil
		}
		// Evicted from memory after persist; fall through to BlockDB.
	}
	return s.blockFromDB(n)
}

// TryBlock returns the block with the given global number.
// Returns ErrPruned if the block has already been pruned.
// Returns ErrNotFound if the block is not available yet.
func (s *State) TryBlock(n types.GlobalBlockNumber) (*types.Block, error) {
	for inner := range s.inner.Lock() {
		if n < inner.first {
			return nil, ErrPruned
		}
		if b, ok := inner.blocks[n]; ok {
			return b, nil
		}
		if n >= inner.nextBlock {
			return nil, ErrNotFound
		}
		// Evicted from memory after persist; fall through to BlockDB.
	}
	b, err := s.blockFromDB(n)
	if err != nil {
		if errors.Is(err, ErrPruned) {
			// Async BlockDB prune may have reclaimed it; surface as not found
			// for the non-blocking Try* contract when callers race with GC.
			return nil, ErrNotFound
		}
		return nil, err
	}
	return b, nil
}

// assembleGlobalBlock builds a GlobalBlock from a block and its covering QC.
func assembleGlobalBlock(n types.GlobalBlockNumber, b *types.Block, fqc *types.FullCommitQC) *types.GlobalBlock {
	qc := fqc.QC()
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
// Falls back to BlockDB when the entry was evicted from memory after persist.
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
		b, hasB := inner.blocks[n]
		qc, hasQC := inner.qcs[n]
		if hasB && hasQC {
			return assembleGlobalBlock(n, b, qc), nil
		}
		// Evicted from memory after persist; fall through to BlockDB.
	}
	return s.globalBlockFromDB(n)
}

func (s *State) blockFromDB(n types.GlobalBlockNumber) (*types.Block, error) {
	opt, err := s.blockDB.ReadBlockByNumber(n)
	if err != nil {
		return nil, fmt.Errorf("blockDB.ReadBlockByNumber(%d): %w", n, err)
	}
	b, ok := opt.Get()
	if !ok {
		return nil, ErrPruned
	}
	return b, nil
}

func (s *State) qcFromDB(n types.GlobalBlockNumber) (*types.FullCommitQC, error) {
	opt, err := s.blockDB.ReadQCByBlockNumber(n)
	if err != nil {
		return nil, fmt.Errorf("blockDB.ReadQCByBlockNumber(%d): %w", n, err)
	}
	qc, ok := opt.Get()
	if !ok {
		return nil, ErrPruned
	}
	return qc, nil
}

func (s *State) globalBlockFromDB(n types.GlobalBlockNumber) (*types.GlobalBlock, error) {
	b, err := s.blockFromDB(n)
	if err != nil {
		return nil, err
	}
	qc, err := s.qcFromDB(n)
	if err != nil {
		return nil, err
	}
	return assembleGlobalBlock(n, b, qc), nil
}

func (s *State) globalBlockByHashFromDB(hash types.BlockHeaderHash) (utils.Option[*types.GlobalBlock], error) {
	opt, err := s.blockDB.ReadBlockByHash(hash)
	if err != nil {
		return utils.None[*types.GlobalBlock](), fmt.Errorf("blockDB.ReadBlockByHash: %w", err)
	}
	bn, ok := opt.Get()
	if !ok {
		return utils.None[*types.GlobalBlock](), nil
	}
	qc, err := s.qcFromDB(bn.Number)
	if err != nil {
		if errors.Is(err, ErrPruned) {
			return utils.None[*types.GlobalBlock](), nil
		}
		return utils.None[*types.GlobalBlock](), err
	}
	return utils.Some(assembleGlobalBlock(bn.Number, bn.Block, qc)), nil
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
			s.metrics.Blocks.Execute.Observe(latency)
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
		// Keep at least one entry so BlockDB is never empty on restart.
		for inner.first+1 < firstToKeep {
			inner.pruneFirst(pruningTime, s.metrics)
		}
		ctrl.Updated()
		truncateTo = utils.Some(inner.first)
	}
	// Prune BlockDB outside the lock to avoid holding it during disk I/O.
	// PruneBefore advances an in-memory watermark; GC is asynchronous and not
	// persisted. On restart before GC reclaims entries, NewState may see
	// below-watermark blocks/QCs and set inner.first lower than the pre-crash
	// watermark. This is safe (resurrected data is QC-covered committed data)
	// and self-heals on the next runPruning cycle. Durable prune bounds come
	// from the BlockDB retention TTL, not from PruneBefore itself.
	if n, ok := truncateTo.Get(); ok {
		return s.blockDB.PruneBefore(n)
	}
	return nil
}

// runPersist is a background goroutine that persists blocks and QCs to BlockDB.
// It waits for in-memory data to advance past the persistence cursors, then
// writes QCs (first, per the BlockDB contract) and blocks, then flushes once
// per batch. nextBlockToPersist advances to min(persistedQC, persistedBlock)
// to unblock PushAppHash only when both are durable.
// Errors propagate vertically (kill the component).
func (s *State) runPersist(ctx context.Context, persistedQC, persistedBlock types.GlobalBlockNumber) error {
	evictedQC := persistedQC
	for {
		// Wait for unpersisted data and snapshot what needs writing.
		type batch struct {
			qcs      []*types.FullCommitQC
			blocks   []blockEntry
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
				b.blocks = append(b.blocks, blockEntry{n: n, blk: inner.blocks[n]})
			}
		}
		// Write QCs first (BlockDB contract: QC must precede covered blocks).
		for _, qc := range b.qcs {
			gr := qc.QC().GlobalRange()
			if err := s.blockDB.WriteQC(gr.First, gr.Next, qc); err != nil {
				return fmt.Errorf("write QC [%d,%d): %w", gr.First, gr.Next, err)
			}
		}
		for _, lb := range b.blocks {
			if err := s.blockDB.WriteBlock(lb.n, lb.blk); err != nil {
				return fmt.Errorf("write block %d: %w", lb.n, err)
			}
		}
		// Flush once per batch before advancing nextBlockToPersist, so that
		// PushAppHash only unblocks after data is crash-durable.
		if err := s.blockDB.Flush(); err != nil {
			return fmt.Errorf("flush BlockDB: %w", err)
		}
		persistedQC = b.qcEnd
		persistedBlock = b.blockEnd
		// Advance nextBlockToPersist to where both QCs and blocks are durable.
		// Evict blocks and QCs below nextAppProposal — those have been executed
		// and are now fully owned by BlockDB.
		newToPersist := min(persistedQC, persistedBlock)
		for inner, ctrl := range s.inner.Lock() {
			if newToPersist > inner.nextBlockToPersist {
				inner.nextBlockToPersist = newToPersist
				ctrl.Updated()
			}
			// Keep qcs/blocks[nextAppProposal-1]: nextToExecute reads it to
			// compute the next lane block after the last executed global block.
			evictBelow := evictedQC
			if inner.nextAppProposal > inner.first {
				evictBelow = inner.nextAppProposal - 1
			}
			for ; evictedQC < evictBelow; evictedQC++ {
				if b, ok := inner.blocks[evictedQC]; ok {
					delete(inner.blockHashes, b.Header().Hash())
					delete(inner.blocks, evictedQC)
				}
				delete(inner.qcs, evictedQC)
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
		// Prune BlockDB outside the lock to avoid holding it during disk I/O.
		if n, ok := truncateTo.Get(); ok {
			if err := s.blockDB.PruneBefore(n); err != nil {
				return fmt.Errorf("prune BlockDB before %d: %w", n, err)
			}
		}
		// Wait until the next pruning time.
		if err := utils.SleepUntil(ctx, pruningTime); err != nil {
			return err
		}
	}
}

// Run starts the background goroutines (persistence and pruning).
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("runPersist", func() error {
			return s.runPersist(ctx, s.blockDBPersistedQC, s.blockDBPersistedBlock)
		})
		if pruneAfter, ok := s.cfg.PruneAfter.Get(); ok {
			scope.SpawnNamed("runPruning", func() error {
				return s.runPruning(ctx, pruneAfter)
			})
		}
		return nil
	})
}
