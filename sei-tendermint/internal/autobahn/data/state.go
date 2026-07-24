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
)

const blocksCacheSize = 4000

// Config is the config for the data State.
type Config struct {
	// Registry is the authoritative source of committee and stake information.
	Registry *epoch.Registry
	// LastExecutedBlock is the last app-committed global height
	// (app.LastBlockHeight). Used only to map → CommitQC road for
	// SetupInitialDuo. 0 means fresh / unknown.
	//
	// TODO(autobahn): This is read from the Cosmos app state DB today. Autobahn
	// should not depend on the app DB — move executed height into Giga storage.
	LastExecutedBlock types.GlobalBlockNumber
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

	// Store under Watch.Lock; readers use State.epochDuo.
	epochDuo utils.AtomicSend[types.EpochDuo]

	// first is the exclusive low end of retained in-memory state: maps keep
	// [first, next*). Set by newInner / skipTo; advanced by evictBelowBound to
	// min(nextAppProposal, App.GlobalNumber()+1) when a CommitQC.App exists.
	// nextToExecute reads the next (or tip) QC from maps — it does not need
	// nextAppProposal-1 retained after eviction.
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
}

// insertQC verifies and inserts a FullCommitQC into the inner state.
// Accepts QCs whose range starts at or before nextQC (partially pruned
// prefix is silently skipped). Rejects gaps where gr.First > nextQC.
func (i *inner) insertQC(qc *types.FullCommitQC, ep *types.Epoch) error {
	if err := qc.Verify(ep); err != nil {
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

// insertBlock inserts a block into the inner state.
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
//
// Invariant: a CommitQC's embedded AppProposal (when present) always refers to
// a global number from a *past* CommitQC — strictly below that tip QC's
// GlobalRange.First (enforced in Proposal.Verify). Together with BlockDB's
// never-empty retention and eviction at min(nextAppProposal, App+1), in-memory
// maps therefore always retain at least the certified tip QC after a
// CommitQC.App appears. nextToExecute uses qc[nextAppProposal] (or the tip QC
// when fully caught up), so it does not require retaining nextAppProposal-1.
type State struct {
	cfg      *Config
	metrics  *metrics.Metrics
	inner    utils.Watch[*inner]
	epochDuo utils.AtomicRecv[types.EpochDuo] // Load-only view of inner.epochDuo; EpochDuo() reads it
	blockDB  types.BlockDB
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
	// Seed epochs before replay (data owns SetupInitialDuo).
	// TODO(autobahn): persist execution tip in Giga storage (stop using app DB).
	commitQCs, err := commitQCSpan(blockDB)
	if err != nil {
		return nil, fmt.Errorf("scan CommitQC span: %w", err)
	}
	nextRoad, err := s.nextRoadToExecute(blockDB)
	if err != nil {
		return nil, fmt.Errorf("resolve next road to execute: %w", err)
	}
	cfg.Registry.SetupInitialDuo(nextRoad, commitQCs)

	if err := s.loadFromBlockDB(blockDB); err != nil {
		return nil, fmt.Errorf("loadFromBlockDB: %w", err)
	}

	// Center epochDuo on CommitQC tipcut (Index+1).
	for in := range s.inner.Lock() {
		initRoad := types.RoadIndex(0)
		if in.nextQC > in.first {
			if lastQC := in.qcs[in.nextQC-1]; lastQC != nil {
				initRoad = lastQC.QC().Proposal().Index() + 1
			}
		}
		initDuo, err := cfg.Registry.DuoAt(initRoad)
		if err != nil {
			return nil, fmt.Errorf("init epochDuo: %w", err)
		}
		in.epochDuo = utils.NewAtomicSend(initDuo)
		s.epochDuo = in.epochDuo.Subscribe()
	}
	return s, nil
}

// commitQCSpan returns the half-open [First, Next) of retained CommitQC roads,
// or None when the store holds no QCs. Used to seed SetupInitialDuo.
func commitQCSpan(blockDB types.BlockDB) (utils.Option[types.RoadRange], error) {
	first, ok, err := boundaryQCRoad(blockDB, false)
	if err != nil || !ok {
		return utils.None[types.RoadRange](), err
	}
	last, ok, err := boundaryQCRoad(blockDB, true)
	if err != nil {
		return utils.None[types.RoadRange](), err
	}
	if !ok {
		return utils.None[types.RoadRange](), fmt.Errorf("CommitQC span: first present but last missing")
	}
	return utils.Some(types.RoadRange{First: first, Next: last + 1}), nil
}

// boundaryQCRoad returns the proposal road of the first (reverse=false) or last
// (reverse=true) retained CommitQC, or ok=false when the store holds no QCs.
func boundaryQCRoad(blockDB types.BlockDB, reverse bool) (types.RoadIndex, bool, error) {
	it, err := blockDB.QCs(reverse)
	if err != nil {
		return 0, false, fmt.Errorf("open QC iterator: %w", err)
	}
	defer func() { _ = it.Close() }()
	ok, err := it.Next()
	if err != nil || !ok {
		return 0, false, err
	}
	qc, err := it.QC()
	if err != nil {
		return 0, false, err
	}
	return qc.QC().Proposal().Index(), true, nil
}

// nextRoadToExecute is the half-open execution tipcut. Covering QC road R:
// mid-range → R; IsLastBlock → R+1. LastExecutedBlock 0 → None. Positive height
// with missing/pruned covering QC → error.
func (s *State) nextRoadToExecute(blockDB types.BlockDB) (utils.Option[types.RoadIndex], error) {
	n := s.cfg.LastExecutedBlock
	if n == 0 {
		return utils.None[types.RoadIndex](), nil
	}
	opt, err := blockDB.ReadQCByBlockNumber(n)
	if err != nil {
		if errors.Is(err, types.ErrPruned) || errors.Is(err, types.ErrNotFound) {
			return utils.None[types.RoadIndex](), fmt.Errorf("covering QC for executed block %d missing or pruned", n)
		}
		return utils.None[types.RoadIndex](), fmt.Errorf("read QC for executed block %d: %w", n, err)
	}
	qc, ok := opt.Get()
	if !ok {
		return utils.None[types.RoadIndex](), fmt.Errorf("covering QC for executed block %d missing or pruned", n)
	}
	road := qc.QC().Proposal().Index()
	if qc.QC().GlobalRange().IsLastBlock(n) {
		return utils.Some(road + 1), nil
	}
	return utils.Some(road), nil
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
//
// Epochs must already be seeded (SetupInitialDuo) so EpochAt resolves QC committees.
// Live PushQC/PushBlock use epochDuo, not EpochAt.
//
// TODO: Cap how much of BlockDB we replay into RAM (similar to PushQC's
// blocksCacheSize gate). Deferred to a follow-up PR — today we load the full
// retained store.
//
// TODO: Push gap / first-block / QC-coverage consistency checks down into the
// BlockDB implementation so loadFromBlockDB can assume a consistent view.
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
				ep, err := s.cfg.Registry.EpochAt(qc.QC().Proposal().Index())
				if err != nil {
					return fmt.Errorf("load QC from BlockDB: epoch lookup: %w", err)
				}
				if err := in.insertQC(qc, ep); err != nil {
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
				e, err := s.cfg.Registry.EpochAt(qc.QC().Proposal().Index())
				if err != nil {
					return fmt.Errorf("load block %d from BlockDB: epoch lookup: %w", n, err)
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

// EpochDuo is a point-in-time snapshot; re-call after a boundary advance.
func (s *State) EpochDuo() types.EpochDuo { return s.epochDuo.Load() }

// CommitTipCut is the road after the last applied CommitQC (Index+1), or 0 if
// none. Restart anchor for p2p.checkRestartTips (consensus tip vs data).
func (s *State) CommitTipCut() types.RoadIndex {
	for inner := range s.inner.Lock() {
		if inner.nextQC == 0 || inner.nextQC <= inner.first {
			return 0
		}
		qc := inner.qcs[inner.nextQC-1]
		if qc == nil {
			return 0
		}
		return qc.QC().Proposal().Index() + 1
	}
	panic("unreachable")
}

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

// PushQC atomically admits qc and optional finalized blocks.
// Tip-order WaitUntil runs before EpochForRoad so a first QC of the next epoch
// waits for the boundary slide (rather than ErrRoadAfterWindow). Epoch via
// epochDuo only (not Registry); before-window hard-fails.
func (s *State) PushQC(ctx context.Context, qc *types.FullCommitQC, blocks []*types.Block) error {
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
	ep, err := s.epochDuo.Load().EpochForRoad(qc.QC().Proposal().Index())
	if err != nil {
		return err
	}
	// Verify data.
	if needQC {
		if err := qc.Verify(ep); err != nil {
			return fmt.Errorf("qc.Verify(): %w", err)
		}
	}
	// Blocks share the QC's epoch (unlike PushBlock, which uses the stored QC).
	byHash := map[types.BlockHeaderHash]*types.Block{}
	committee := ep.Committee()
	for _, b := range blocks {
		byHash[b.Header().Hash()] = b
		if err := b.Verify(committee); err != nil {
			return fmt.Errorf("b.Verify(): %w", err)
		}
	}
	// Closing Current: WaitForDuo(tipcut) before mutating nextQC.
	idx := qc.QC().Proposal().Index()
	var nextDuo *types.EpochDuo
	duo := s.epochDuo.Load()
	if needQC && idx+1 == duo.Current.RoadRange().Next {
		nt, err := s.cfg.Registry.WaitForDuo(ctx, idx+1)
		if err != nil {
			return err
		}
		nextDuo = &nt
	}
	for inner, ctrl := range s.inner.Lock() {
		if needQC {
			// Only the first inserter may advance epochDuo.
			applied := inner.nextQC == gr.First
			for inner.nextQC < gr.Next {
				inner.qcs[inner.nextQC] = qc
				inner.nextQC += 1
			}
			if applied && nextDuo != nil {
				inner.epochDuo.Store(*nextDuo)
			}
			ctrl.Updated()
		}
		if len(byHash) > 0 {
			if err := s.insertBlocksByHash(inner, gr, byHash); err != nil {
				return err
			}
			ctrl.Updated()
		}
		// Only a newly accepted QC can advance CommitQC.App / the eviction floor.
		if needQC {
			evictBelowBound(inner)
		}
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

// PushBlock requires a covering QC (or n < nextBlock → silent drop).
// Same epochDuo before-window hard-fail as PushQC.
func (s *State) PushBlock(ctx context.Context, n types.GlobalBlockNumber, block *types.Block) error {
	var ep *types.Epoch
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return n < inner.nextQC }); err != nil {
			return err
		}
		// Already in/below the contiguous prefix: insertBlock would no-op
		// (stored, or evicted below first with nextBlock advanced past n).
		if n < inner.nextBlock {
			return nil
		}
		// n in [nextBlock, nextQC): QC is contiguous in that range.
		var err error
		ep, err = s.epochDuo.Load().EpochForRoad(inner.qcs[n].QC().Proposal().Index())
		if err != nil {
			return fmt.Errorf("epoch not in window: %w", err)
		}
	}
	// Verify outside the lock against the known epoch.
	if err := block.Verify(ep.Committee()); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		// insertBlock may no-op if n fell into the contiguous prefix (or was
		// gap-filled) while we verified outside the lock; Updated is still
		// signaled so waiters re-check.
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
// Non-blocking. Serves from RAM whenever the hash is still indexed (contiguous
// prefix, gap-fills, and executed heights not yet dropped by evictBelowBound).
// Falls back to BlockDB only after eviction removes the hash — matching
// Block/TryBlock/QC, which also prefer maps before the store. Gap-fills are
// not written to BlockDB until nextBlock catches up, so they must be served
// from RAM here; Block/TryBlock continue to hide gaps by number.
func (s *State) GlobalBlockByHash(hash types.BlockHeaderHash) (utils.Option[*types.GlobalBlock], error) {
	for inner := range s.inner.Lock() {
		n, ok := inner.blockHashes[hash]
		if !ok {
			break
		}
		// blockHashes stays in lockstep with blocks; a hit means both block and
		// covering QC are still cached (including n < nextAppProposal when
		// AppQC eviction has not advanced first past n yet).
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
// Callers must supply non-nil b and fqc for height n. In-memory paths look up
// maps only for heights still indexed there (including gap-fills); BlockDB
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
		ap, ok := inner.appProposals[n]
		if !ok {
			return nil, types.ErrPruned
		}
		return ap, nil
	}
	panic("unreachable")
}

func (i *inner) nextToExecute(lane types.LaneID) types.BlockNumber {
	// TODO(gprusak): decide whether 0 is a good result in this case in general.
	// Empty maps (first == nextQC) only on fresh start / after skipTo with no QC.
	if i.first == i.nextQC {
		return 0
	}
	// Fully executed through the certified tip: next lane block is past the tip QC.
	if i.nextAppProposal == i.nextQC {
		return i.qcs[i.nextAppProposal-1].QC().LaneRange(lane).Next()
	}
	// nextAppProposal < nextQC: derive from the next global block to execute
	// (header from FullCommitQC — works even if blocks[n] was never gap-filled).
	n := i.nextAppProposal
	fqc := i.qcs[n]
	qc := fqc.QC()
	gr := qc.GlobalRange()
	h := fqc.Headers()[n-gr.First]
	r := qc.LaneRange(lane)
	x := lane.Compare(h.Lane())
	// NOTE: here we assume the specific ordering of lane blocks in the CommitQC:
	// TODO(gprusak): move this logic closer to CommitQC
	switch {
	case x < 0:
		return r.Next()
	case x > 0:
		return r.First()
	default:
		// This block is not executed yet.
		return h.BlockNumber()
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
// It waits for in-memory blocks to advance past the block persistence cursor,
// then writes covering QCs (first, per the BlockDB contract) and blocks, then
// flushes once per batch. nextBlockToPersist advances with the block tip to
// unblock PushAppHash only when data is durable.
// Errors propagate vertically (kill the component).
//
// Cursors seed from BlockDB.Status() when non-zero so PushQC-before-Run heights
// are not skipped. When a tip is zero, seed from post-load nextBlockToPersist
// (recovery floor), never bare registry.FirstBlock() — a QC-only store can
// skipTo past genesis while NextBlock is still zero.
//
// Under the BlockDB write contract (QC before covered blocks), NextQC is never
// behind NextBlock after a successful write. Persistence is driven by the block
// cursor; a QC is emitted only when n equals GlobalRange.First and that First
// is still at or past NextQC (enough coverage for each new block, not every
// in-memory QC, and no rewrite of QCs already on disk).
//
// In-memory eviction is driven by PushQC / PushAppHash (evictBelowBound), not here.
func (s *State) runPersist(ctx context.Context) error {
	tips := s.blockDB.Status()
	var nextToPersistQC, nextToPersistBlock types.GlobalBlockNumber
	for inner := range s.inner.Lock() {
		// After loadFromBlockDB, nextBlockToPersist is the durable recovery tip.
		nextToPersistQC = tips.NextQC
		if nextToPersistQC == 0 {
			nextToPersistQC = inner.nextBlockToPersist
		}
		nextToPersistBlock = tips.NextBlock
		if nextToPersistBlock == 0 {
			nextToPersistBlock = inner.nextBlockToPersist
		}
	}
	for {
		type batch struct {
			qcs       []*types.FullCommitQC
			blocks    []blockEntry
			nextBlock types.GlobalBlockNumber
		}
		var b batch
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return nextToPersistBlock < inner.nextBlock
			}); err != nil {
				return err
			}
			b.nextBlock = inner.nextBlock
			// Persist blocks in [nextToPersistBlock, nextBlock). Emit each covering
			// QC once at GlobalRange.First when it has not already been written
			// (First >= nextToPersistQC).
			for n := nextToPersistBlock; n < inner.nextBlock; n++ {
				qc := inner.qcs[n]
				gr := qc.QC().GlobalRange()
				if n == gr.First && gr.First >= nextToPersistQC {
					b.qcs = append(b.qcs, qc)
				}
				b.blocks = append(b.blocks, blockEntry{n: n, block: inner.blocks[n]})
			}
		}
		// Write QCs first (BlockDB contract: QC must precede covered blocks).
		for _, qc := range b.qcs {
			gr := qc.QC().GlobalRange()
			if err := s.blockDB.WriteQC(gr.First, gr.Next, qc); err != nil {
				return fmt.Errorf("write QC [%d,%d): %w", gr.First, gr.Next, err)
			}
			if gr.Next > nextToPersistQC {
				nextToPersistQC = gr.Next
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
		nextToPersistBlock = b.nextBlock
		for inner, ctrl := range s.inner.Lock() {
			if nextToPersistBlock > inner.nextBlockToPersist {
				inner.nextBlockToPersist = nextToPersistBlock
				ctrl.Updated()
			}
		}
	}
}

// certifiedAppFloor is the exclusive eviction floor from the tip CommitQC's
// embedded App, if any: App.GlobalNumber()+1. Only the tip QC is consulted
// (no backward walk). Returns 0 when maps are empty or the tip has no App.
func (i *inner) certifiedAppFloor() types.GlobalBlockNumber {
	if i.first == i.nextQC {
		return 0
	}
	// [first, nextQC) is dense in qcs, so nextQC-1 is present.
	app, ok := i.qcs[i.nextQC-1].QC().Proposal().App().Get()
	if !ok {
		return 0
	}
	return app.GlobalNumber() + 1
}

// evictBelowBound advances first toward the certified App floor and drops cached
// blocks/QCs/AppProposals with n < first. No-op when there is no certified App
// or the bound would not advance first. Caller must hold inner's lock. Invoked
// from PushQC / PushAppHash.
//
// Bound is min(nextAppProposal, App.GlobalNumber()+1). A zero floor (no App /
// empty maps) yields bound 0 and is a no-op via bound <= first. With the
// past-CommitQC App invariant (see State), App+1 never exceeds the tip QC
// start, so at least one CommitQC remains. nextToExecute uses qc[nextAppProposal]
// (or the tip when caught up), so nextAppProposal-1 need not be retained.
//
// TODO: At eviction we have both a local AppProposal and a CommitQC.App, so this
// is the right place to detect local-vs-quorum AppHash inconsistency. Surface
// any mismatch from data.State.Run() (node-fatal), not from PushQC/PushAppHash —
// e.g. stash an error on State for a Run monitor, or run eviction as its own
// Run subtask.
func evictBelowBound(inner *inner) {
	floor := inner.certifiedAppFloor()
	bound := min(inner.nextAppProposal, floor)
	if bound <= inner.first {
		return
	}
	for n := inner.first; n < bound; n++ {
		if b, ok := inner.blocks[n]; ok {
			delete(inner.blockHashes, b.Header().Hash())
			delete(inner.blocks, n)
		}
		delete(inner.qcs, n)
		delete(inner.appProposals, n)
	}
	inner.first = bound
}

// Run starts the background persistence loop.
func (s *State) Run(ctx context.Context) error {
	return s.runPersist(ctx)
}
