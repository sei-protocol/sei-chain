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

// Config is the config for the data State.
type Config struct {
	// Registry is the authoritative source of committee and stake information.
	Registry *epoch.Registry
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
	n     types.GlobalBlockNumber
	block *types.Block
}

// appHashTip is the block and covering FullCommitQC for the latest PushAppHash
// height. Kept so nextToExecute works after evictBelowBound drops that height
// from the maps.
type appHashTip struct {
	block *types.Block
	qc    *types.FullCommitQC
}

type inner struct {
	// Map key ranges (low end = first):
	//   qcs:          [first, nextQC)
	//   blocks:       [first, nextBlock) + gap-fills in [nextBlock, nextQC)
	//   appProposals: [first, nextAppProposal)
	//   blockHashes:  mirrors blocks (insertBlock / evictBelowBound)
	//
	// Durable copies below first live in BlockDB. AppProposals are not
	// persisted; they are rebuilt via PushAppHash / re-execution after restart.
	qcs          map[types.GlobalBlockNumber]*types.FullCommitQC
	blocks       map[types.GlobalBlockNumber]*types.Block
	appProposals map[types.GlobalBlockNumber]*types.AppProposal
	blockHashes  map[types.BlockHeaderHash]types.GlobalBlockNumber

	// lastAppHash pins nextAppProposal-1 for nextToExecute. None until the
	// first PushAppHash in this process.
	lastAppHash utils.Option[appHashTip]

	// first is the exclusive low end of retained in-memory state: maps keep
	// [first, next*). Set by newInner / skipTo; advanced by evictBelowBound when
	// a CommitQC.App certifies a higher floor (min(nextAppProposal, App+1)).
	//
	// first <= nextAppProposal <= nextBlockToPersist <= nextBlock <= nextQC
	//
	// AppProposals require persistence (nextAppProposal <= nextBlockToPersist).
	// BlockDB prune status lives only in the store watermark (see PruneBefore).
	first              types.GlobalBlockNumber
	nextAppProposal    types.GlobalBlockNumber
	nextBlockToPersist types.GlobalBlockNumber
	nextBlock          types.GlobalBlockNumber
	nextQC             types.GlobalBlockNumber
}

func newInner(firstBlock types.GlobalBlockNumber) *inner {
	return &inner{
		qcs:                map[types.GlobalBlockNumber]*types.FullCommitQC{},
		blocks:             map[types.GlobalBlockNumber]*types.Block{},
		appProposals:       map[types.GlobalBlockNumber]*types.AppProposal{},
		blockHashes:        map[types.BlockHeaderHash]types.GlobalBlockNumber{},
		first:              firstBlock,
		nextAppProposal:    firstBlock,
		nextBlockToPersist: firstBlock,
		nextBlock:          firstBlock,
		nextQC:             firstBlock,
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
	i.lastAppHash = utils.None[appHashTip]()
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
// the block signature before calling (unlike insertQC, which verifies).
//
// insertBlock does NOT advance nextBlock — callers should call
// updateNextBlock after inserting one or more blocks. This separation
// allows batch insertion (e.g. PushQC inserts multiple blocks, then
// advances nextBlock once).
func (i *inner) insertBlock(n types.GlobalBlockNumber, block *types.Block) error {
	// Contiguous prefix is done or evicted; only [nextBlock, nextQC) inserts.
	// After success, blocks/blockHashes gain n; qcs[n] is already set.
	if n < i.nextBlock || n >= i.nextQC {
		return nil
	}
	if _, ok := i.blocks[n]; ok {
		return nil // already have it (gap fill)
	}
	// n is in [nextBlock, nextQC); QCs are contiguous and first <=
	// nextAppProposal <= nextBlock, so qcs[n] is always present.
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

// State of the chain.
// Contains blocks in global order and proofs of their finality.
type State struct {
	cfg     *Config
	metrics *metrics.Metrics
	inner   utils.Watch[*inner]
	blockDB types.BlockDB
}

// NewState constructs a data State, replaying persisted state from blockDB.
// Use memblock.NewBlockDB() for an in-memory store (testing / no persistent dir).
// The caller owns blockDB and must close it after State.Run returns (nodeImpl
// owns this in production); State never closes it.
// Recovery from a non-zero CommitQC tip is handled by loadFromBlockDB (skipTo).
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
	return s, nil
}

// loadFromBlockDB replays QCs and blocks from blockDB into s.inner.
// Called from NewState before any goroutines are spawned; the lock is acquired
// only to satisfy the Watch API.
//
// The recovery floor is derived from BlockDB: empty store keeps
// registry.FirstBlock(); otherwise cursors skipTo the first retained QC start.
// Inconsistencies (gaps, block without QC, first-block/QC mismatch, etc.) are
// returned as errors rather than normalized — BlockDB is expected to present a
// consistent iterator view (see littblock watermark / stranding rules).
func (s *State) loadFromBlockDB(blockDB types.BlockDB) error {
	for in := range s.inner.Lock() {
		// Restore QCs from BlockDB. On the first QC, skipTo its GlobalRange.First
		// to advance past any pruned prefix. Subsequent QCs must be consecutive —
		// insertQC errors on any gap.
		err := func() error {
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
				if len(in.qcs) == 0 {
					gr := qc.QC().GlobalRange()
					if gr.First < in.nextQC {
						return fmt.Errorf("QC in BlockDB predates committee genesis %d: got %d", in.nextQC, gr.First)
					}
					if gr.First > in.nextQC {
						in.skipTo(gr.First)
					}
				}
				if err := in.insertQC(s.cfg.Registry, qc); err != nil {
					return fmt.Errorf("load QC from BlockDB: %w", err)
				}
			}
		}()
		if err != nil {
			return err
		}

		// Restore blocks from BlockDB. First block must align with first QC start
		// (set by the QC pass); a mismatch is corruption / incomplete store.
		// After the QC pass with no AppProposals, nextAppProposal is the recovery
		// floor (registry.FirstBlock() or first retained QC start).
		err = func() error {
			it, err := blockDB.Blocks(false)
			if err != nil {
				return fmt.Errorf("open block iterator: %w", err)
			}
			defer func() { _ = it.Close() }()
			nextExpect := in.nextAppProposal
			for {
				ok, err := it.Next()
				if err != nil || !ok {
					return err
				}
				n := it.Number()
				if n >= in.nextQC {
					return fmt.Errorf("block %d in BlockDB has no QC coverage (nextQC=%d)", n, in.nextQC)
				}
				// updateNextBlock only advances nextBlock through contiguous present
				// entries, so runPersist always writes [persistedBlock, nextBlock)
				// fully populated. A gap here means BlockDB corruption.
				if n != nextExpect {
					return fmt.Errorf("%w: expected %d, got %d", types.ErrBlockGap, nextExpect, n)
				}
				nextExpect++
				blk, err := it.Block()
				if err != nil {
					return err
				}
				qc := in.qcs[n]
				e, ok := s.cfg.Registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
				if !ok {
					return fmt.Errorf("unknown epoch_index %d", qc.QC().Proposal().EpochIndex())
				}
				if err := blk.Verify(e.Committee()); err != nil {
					return fmt.Errorf("verify block %d from BlockDB: %w", n, err)
				}
				if err := in.insertBlock(n, blk); err != nil {
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

// insertBlocksByHash matches byHash against stored (already verified) QC
// headers over gr ∩ [nextBlock, nextQC) and inserts hits. Advances nextBlock
// when the contiguous prefix grows. Caller must hold inner's lock.
func (s *State) insertBlocksByHash(inner *inner, gr types.GlobalRange, byHash map[types.BlockHeaderHash]*types.Block) error {
	for n := max(inner.nextBlock, gr.First); n < min(gr.Next, inner.nextQC); n++ {
		storedQC := inner.qcs[n]
		storedGR := storedQC.QC().GlobalRange()
		if b, ok := byHash[storedQC.Headers()[n-storedGR.First].Hash()]; ok {
			if err := inner.insertBlock(n, b); err != nil {
				return err
			}
		}
	}
	inner.updateNextBlock(s.metrics)
	return nil
}

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
			evictBelowBound(inner)
			break
		}
		if err := s.insertBlocksByHash(inner, gr, byHash); err != nil {
			return err
		}
		ctrl.Updated()
		// CommitQC.App may advance the certified floor; drop covered RAM.
		evictBelowBound(inner)
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
		// [first, nextQC) is retained in RAM; below that use BlockDB.
		if n < inner.first {
			break
		}
		return inner.qcs[n], nil
	}
	return s.qcFromDB(n)
}

// PushBlock pushes block to the state.
// The QC for n must already be present (guaranteed by PushQC ordering), unless
// the height is already in the contiguous block prefix (n < nextBlock) — in
// that case the block is dropped silently (already stored or executed/evicted).
func (s *State) PushBlock(ctx context.Context, n types.GlobalBlockNumber, block *types.Block) error {
	var epochIdx types.EpochIndex
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextQC }); err != nil {
			return err
		}
		// Already past the contiguous prefix: insertBlock would no-op, and
		// heights below nextAppProposal may no longer have qcs[n] in RAM.
		if n < inner.nextBlock {
			return nil
		}
		// n in [nextBlock, nextQC): QC is contiguous in that range.
		epochIdx = inner.qcs[n].QC().Proposal().EpochIndex()
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
		if err := inner.insertBlock(n, block); err != nil {
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
// hashes to the given value, or None if no such block is currently retained.
// Non-blocking. Serves from RAM when the hash is indexed and
// n >= nextAppProposal (unexecuted contiguous prefix and gap-fills ahead of
// nextBlock). Executed heights fall through to BlockDB. Gap-fills are not
// written to BlockDB until nextBlock catches up, so they must be served from
// RAM here — unlike Block/TryBlock, which hide gaps by design.
func (s *State) GlobalBlockByHash(hash types.BlockHeaderHash) (utils.Option[*types.GlobalBlock], error) {
	for inner := range s.inner.Lock() {
		n, ok := inner.blockHashes[hash]
		if !ok {
			break
		}
		if n < inner.nextAppProposal {
			break
		}
		return utils.Some(assembleGlobalBlock(n, inner.blocks[n], inner.qcs[n])), nil
	}
	return s.globalBlockByHashFromDB(hash)
}

// Block returns the block with the given global number.
// Waits until the contiguous prefix reaches n (n < nextBlock), then returns
// it from memory or BlockDB. Does not expose gaps ahead of nextBlock.
// This function is used for syncing - GlobalBlock can be derived from Block and FullCommitQC,
// which have to be fetched upfront anyway.
func (s *State) Block(ctx context.Context, n types.GlobalBlockNumber) (*types.Block, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextBlock
		}); err != nil {
			return nil, err
		}
		// [first, nextBlock) is retained in RAM; below that use BlockDB.
		if n < inner.first {
			break
		}
		return inner.blocks[n], nil
	}
	return s.blockFromDB(n)
}

// TryBlock returns the block with the given global number.
// Returns ErrNotFound if n is not yet in the contiguous prefix (n >= nextBlock),
// including gap-fills stored above nextBlock — same no-gap contract as Block.
// Returns ErrPruned if BlockDB no longer has an evicted height.
// Evicted-but-still-durable heights (n < nextBlock) load from BlockDB.
func (s *State) TryBlock(n types.GlobalBlockNumber) (*types.Block, error) {
	for inner := range s.inner.Lock() {
		if n >= inner.nextBlock {
			return nil, types.ErrNotFound
		}
		if n < inner.first {
			break
		}
		return inner.blocks[n], nil
	}
	return s.blockFromDB(n)
}

// assembleGlobalBlock builds a GlobalBlock from a block and its covering QC.
// In-memory callers must have verified n is in [inner.first, inner.nextBlock)
// (or a sub-window); map lookups outside that range nil-deref. BlockDB
// fallbacks pass values already loaded from the store.
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
// Waits until the contiguous prefix reaches n (same no-gap contract as Block).
// Returns ErrPruned if the block has already been pruned from BlockDB.
// Falls back to BlockDB when the entry was evicted from memory after persist.
func (s *State) GlobalBlock(ctx context.Context, n types.GlobalBlockNumber) (*types.GlobalBlock, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return n < inner.nextBlock
		}); err != nil {
			return nil, err
		}
		if n < inner.first {
			break
		}
		return assembleGlobalBlock(n, inner.blocks[n], inner.qcs[n]), nil
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
		// Caller only falls through for heights below nextBlock (already seen).
		// None here means the store no longer has them (pruned/reclaimed).
		return nil, types.ErrPruned
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
		return nil, types.ErrPruned
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
		if errors.Is(err, types.ErrPruned) || errors.Is(err, types.ErrNotFound) {
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
		// TODO(gprusak): this will be problematic on restart,
		// nextAppProposal should be initiated wrt current application height,
		// so that we don't iterate over all blocks in storage on startup.
		for inner.nextAppProposal <= n {
			b := inner.blocks[inner.nextAppProposal]
			latency := t.Sub(b.Payload().CreatedAt()).Seconds()
			s.metrics.Blocks.Execute.Observe(latency)
			s.metrics.Txs.Execute.ObserveWithWeight(latency, uint64(len(b.Payload().Txs())))
			inner.appProposals[inner.nextAppProposal] = proposal
			inner.nextAppProposal += 1
		}
		// Pin tip before eviction may drop maps[n].
		inner.lastAppHash = utils.Some(appHashTip{
			block: inner.blocks[n],
			qc:    inner.qcs[n],
		})
		evictBelowBound(inner)
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
			return nil, types.ErrPruned
		}
		ap := inner.appProposals[n]
		if ap == nil {
			return nil, types.ErrPruned
		}
		return ap, nil
	}
	panic("unreachable")
}

func (i *inner) nextToExecute(lane types.LaneID) types.BlockNumber {
	// TODO(gprusak): decide whether 0 is a good result in this case in general.
	tip, ok := i.lastAppHash.Get()
	if !ok {
		return 0
	}
	r := tip.qc.QC().LaneRange(lane)
	// TODO: this header can be actually extracted from FullCommitQC, so consider moving all this logic there.
	h := tip.block.Header()
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

// PruneBefore asks BlockDB to drop data before retainFrom. This is independent
// of in-memory retention: RAM is cleared only by evictBelowBound (AppQC floor),
// and AppProposals are not persisted. BlockDB enforces its own never-empty
// retention and refuses reads below its watermark.
func (s *State) PruneBefore(retainFrom types.GlobalBlockNumber) error {
	return s.blockDB.PruneBefore(retainFrom)
}

// runPersist is a background goroutine that persists blocks and QCs to BlockDB.
// It waits for in-memory data to advance past the persistence cursors, then
// writes QCs (first, per the BlockDB contract) and blocks, then flushes once
// per batch. nextBlockToPersist advances to min(nextToPersistQC, nextToPersistBlock)
// to unblock PushAppHash only when both are durable.
// Errors propagate vertically (kill the component).
//
// Persistence cursors (nextToPersistQC / nextToPersistBlock) seed from
// BlockDB.Status() when present so PushQC-before-Run heights are not skipped.
// When a Status tip is absent, seed from post-load nextBlockToPersist (recovery
// floor), never bare registry.FirstBlock() — a QC-only store can skipTo past
// genesis while NextBlock is still missing.
//
// In-memory eviction is driven by PushQC / PushAppHash (evictBelowBound), not here.
func (s *State) runPersist(ctx context.Context) error {
	tips := s.blockDB.Status()
	var nextToPersistQC, nextToPersistBlock types.GlobalBlockNumber
	for inner := range s.inner.Lock() {
		// After loadFromBlockDB, nextBlockToPersist is the durable recovery tip.
		nextToPersistQC = inner.nextBlockToPersist
		nextToPersistBlock = inner.nextBlockToPersist
	}
	if n, ok := tips.NextQC.Get(); ok {
		nextToPersistQC = n
	}
	if n, ok := tips.NextBlock.Get(); ok {
		nextToPersistBlock = n
	}
	for {
		type batch struct {
			qcs      []*types.FullCommitQC
			blocks   []blockEntry
			qcEnd    types.GlobalBlockNumber
			blockEnd types.GlobalBlockNumber
		}
		var b batch
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return nextToPersistQC < inner.nextQC || nextToPersistBlock < inner.nextBlock
			}); err != nil {
				return err
			}
			b.qcEnd = inner.nextQC
			b.blockEnd = inner.nextBlock
			// Collect deduplicated QCs for [nextToPersistQC, nextQC).
			seen := map[types.GlobalBlockNumber]bool{}
			for n := nextToPersistQC; n < inner.nextQC; n++ {
				qc := inner.qcs[n]
				qcFirst := qc.QC().GlobalRange().First
				if !seen[qcFirst] {
					seen[qcFirst] = true
					b.qcs = append(b.qcs, qc)
				}
			}
			// Collect blocks for [nextToPersistBlock, nextBlock).
			for n := nextToPersistBlock; n < inner.nextBlock; n++ {
				b.blocks = append(b.blocks, blockEntry{n: n, block: inner.blocks[n]})
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
			if err := s.blockDB.WriteBlock(lb.n, lb.block); err != nil {
				return fmt.Errorf("write block %d: %w", lb.n, err)
			}
		}
		// Flush once per batch before advancing nextBlockToPersist, so that
		// PushAppHash only unblocks after data is crash-durable.
		if err := s.blockDB.Flush(); err != nil {
			return fmt.Errorf("flush BlockDB: %w", err)
		}
		nextToPersistQC = b.qcEnd
		nextToPersistBlock = b.blockEnd
		newToPersist := min(nextToPersistQC, nextToPersistBlock)
		for inner, ctrl := range s.inner.Lock() {
			if newToPersist > inner.nextBlockToPersist {
				inner.nextBlockToPersist = newToPersist
				ctrl.Updated()
			}
		}
	}
}

// certifiedAppFloor is the exclusive eviction floor from the tip CommitQC's
// embedded App, if any: App.GlobalNumber()+1. Only the tip QC is consulted
// (no backward walk). Absent App → no certified floor yet.
func (i *inner) certifiedAppFloor() utils.Option[types.GlobalBlockNumber] {
	if i.nextQC == 0 {
		return utils.None[types.GlobalBlockNumber]()
	}
	qc, ok := i.qcs[i.nextQC-1]
	if !ok {
		return utils.None[types.GlobalBlockNumber]()
	}
	app, ok := qc.QC().Proposal().App().Get()
	if !ok {
		return utils.None[types.GlobalBlockNumber]()
	}
	return utils.Some(app.GlobalNumber() + 1)
}

// evictBelowBound advances first to min(nextAppProposal, certifiedAppFloor) when
// a CommitQC.App exists, and drops cached blocks/QCs/AppProposals with n < first.
// No-op when there is no certified App or the bound would not advance first.
// Caller must hold inner's lock. Invoked from PushQC / PushAppHash.
func evictBelowBound(inner *inner) {
	floor, ok := inner.certifiedAppFloor().Get()
	if !ok {
		return
	}
	bound := min(inner.nextAppProposal, floor)
	if bound <= inner.first {
		return
	}
	inner.first = bound
	for n, b := range inner.blocks {
		if n < bound {
			delete(inner.blockHashes, b.Header().Hash())
			delete(inner.blocks, n)
		}
	}
	for n := range inner.qcs {
		if n < bound {
			delete(inner.qcs, n)
		}
	}
	for n := range inner.appProposals {
		if n < bound {
			delete(inner.appProposals, n)
		}
	}
}

// Run starts the background persistence goroutine.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("runPersist", func() error {
			return s.runPersist(ctx)
		})
		return nil
	})
}
