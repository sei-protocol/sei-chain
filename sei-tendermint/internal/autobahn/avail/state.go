package avail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// ErrBadLane .
var ErrBadLane = errors.New("bad lane")

const BlocksPerLane = 3 * types.MaxLaneRangeInProposal

// State represents the Data Availability Plane and Ordered Event Log.
// Although it resides in a sub-package, it serves as the "source of truth" for:
// - Block data: storing and disseminating raw transaction payloads (lanes).
// - Finality tracking: acting as a persistent buffer for CommitQCs and AppQCs.
// - Pruning: managing memory by deleting data once enough execution proofs (AppVotes) are seen.
//
// NOTE: This component is more than an observer; it actively aggregates AppVotes
// to trigger internal pruning, which allows it to manage memory independently
// of the main consensus loop.
type State struct {
	key   types.SecretKey
	data  *data.State
	inner utils.Watch[*inner]

	// persisters groups all disk persistence components.
	// Always initialized: real when stateDir is set, no-op otherwise.
	persisters *persisters
}

func (s *State) PublicKey() types.PublicKey {
	return s.key.Public()
}

// persisters holds all disk persistence components. Either all are present
// (real I/O) or all are no-op (testing). It is a pure I/O struct — all inner
// state access goes through State methods.
type persisters struct {
	blocks    *persist.BlockPersister
	commitQCs *persist.CommitQCPersister
}

// close releases the WALs these persisters own, and with them the exclusive lock each holds on its
// directory. The prune anchor persister owns nothing between writes, so it needs no release.
//
// The fields are nil-checked because loadPersistedState fills them one at a time and closes what it
// has when a later open fails.
func (p persisters) close() error {
	return errors.Join(p.blocks.Close(), p.commitQCs.Close())
}

// loadPersistedState creates persisters for the given directory option and loads
// any existing state from disk. When dir is None, all persisters are no-op
// and no state is loaded. When a prune anchor is present, stale commitQCs and
// blocks below the anchor are filtered out before returning.
func loadPersistedState(dir utils.Option[string]) (*loadedState, *persisters, error) {
	bp, blocks, err := persist.NewBlockPersister(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("NewBlockPersister: %w", err)
	}
	cp, commitQCs, err := persist.NewCommitQCPersister(dir)
	if err != nil {
		bp.Close()
		return nil, nil, fmt.Errorf("NewCommitQCPersister: %w", err)
	}
	pers := &persisters{blocks: bp, commitQCs: cp}
	loaded := &loadedState{commitQCs: commitQCs, blocks: blocks}
	return loaded, pers, nil
}

// NewState constructs a new availability state.
// stateDir is None when persistence is disabled (testing only); a no-op
// persist goroutine still runs to bump cursors without disk I/O.
func NewState(key types.SecretKey, data *data.State, stateDir utils.Option[string]) (_ *State, err error) {
	loaded, pers, err := loadPersistedState(stateDir)
	if err != nil {
		return nil, err
	}
	inner, err := newInner(data, loaded)
	if err != nil {
		pers.close()
		return nil, err
	}
	return &State{
		key:        key,
		data:       data,
		inner:      utils.NewWatch(inner),
		persisters: pers,
	}, nil
}

// Close releases the WALs this state owns, and with them the exclusive lock each holds on its
// directory.
//
// Production does not call this: a node exits by rugpull and the OS reclaims everything. It exists so
// that a process which opens the same state directory more than once in its lifetime — a test
// simulating a restart — can release the first State before constructing the second.
//
// TODO: tie the WALs to Run instead, opening and closing them inside its scope so the exclusive lock is
// released by scope teardown and this method can go away. That means moving loadPersistedState and the
// startup prune out of NewState, since inner is currently built from the loaded data before Run exists.
func (s *State) Close() error {
	return s.persisters.close()
}

func (s *State) First() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.roads.first
	}
	panic("unreachable")
}

// Data returns the data state.
func (s *State) Data() *data.State {
	return s.data
}

// LastCommitQC returns receiver of the LastCommitQC.
func (s *State) LastCommitQC() utils.AtomicRecv[utils.Option[*types.CommitQC]] {
	for inner := range s.inner.Lock() {
		return inner.persistedCommitQC.Subscribe()
	}
	panic("unreachable")
}

func (s *State) appQC(ctx context.Context, idx types.RoadIndex) (*types.AppQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		for {
			if idx < inner.roads.first {
				return nil, types.ErrPruned
			}
			if idx < inner.roads.next {
				if appQC, ok := inner.roads.q[idx].appQC.Get(); ok {
					return appQC, nil
				}
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}

func (s *State) commitQC(ctx context.Context, idx types.RoadIndex) (*types.Epoch, *types.CommitQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return idx < inner.roads.next }); err != nil {
			return nil, nil, err
		}
		if idx < inner.roads.first {
			return nil, nil, types.ErrPruned
		}
		r := inner.roads.q[idx]
		return r.epoch, r.commitQC, nil
	}
	panic("unreachable")
}

func (s *State) CommitQC(ctx context.Context, idx types.RoadIndex) (*types.CommitQC, error) {
	_, qc, err := s.commitQC(ctx, idx)
	return qc, err
}

// PushCommitQC pushes a CommitQC to the state.
// Waits until all previous CommitQCs are pushed.
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	idx := qc.Proposal().Index()
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return idx <= inner.roads.next }); err != nil {
			return err
		}
		if inner.roads.next > idx {
			return nil
		}
	}
	epoch, ok := s.data.Registry().EpochByIndex(qc.Proposal().EpochIndex())
	if !ok {
		return fmt.Errorf("unknown epoch_index %d", qc.Proposal().EpochIndex())
	}
	if err := qc.Verify(epoch); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if idx != inner.roads.next {
			return nil
		}
		inner.roads.pushBack(newRoad(qc, epoch))
		metrics.ObserveCommitQC(qc)
		// The persist goroutine publishes persistedCommitQC after writing to disk
		// (or immediately for no-op persisters), so consensus won't advance
		// until the CommitQC is durable.
		ctrl.Updated()
		return nil
	}
	return nil
}

// PushAppVote pushes an AppVote to the state.
func (s *State) PushAppVote(ctx context.Context, v *types.Signed[*types.AppVote]) error {
	// Wait for the corresponding commitQC.
	idx := v.Msg().Proposal().RoadIndex()
	epoch, commitQC, err := s.commitQC(ctx, idx)
	if err != nil {
		if errors.Is(err, types.ErrPruned) {
			return nil
		}
		return err
	}
	if err := v.Msg().Proposal().Verify(commitQC); err != nil {
		return fmt.Errorf("invalid vote: %w", err)
	}
	if err := v.VerifySig(epoch.Committee()); err != nil {
		return fmt.Errorf("v.VerifySig(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if idx < inner.roads.first || inner.roads.next <= idx {
			return nil
		}
		if inner.roads.q[idx].pushAppVote(v) {
			ctrl.Updated()
		}
	}
	return nil
}

// NextBlock returns the index of the next missing block in local storage for the given lane.
func (s *State) NextBlock(lane types.LaneID) types.BlockNumber {
	for inner := range s.inner.Lock() {
		if l, ok := inner.blocks[lane]; ok {
			return l.next
		}
	}
	return 0
}

// Block returns block n of the given lane.
// Waits until the block is available.
// Returns ErrPruned if the block has been already pruned.
func (s *State) Block(ctx context.Context, lane types.LaneID, n types.BlockNumber) (*types.Signed[*types.LaneProposal], error) {
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.blocks[lane]
		if !ok {
			return nil, ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool { return n < q.next }); err != nil {
			return nil, err
		}
		if n < q.first {
			return nil, types.ErrPruned
		}
		return q.q[n], nil
	}
	panic("unreachable")
}

// PushBlock pushes a block to the state.
// Waits until all previous blocks are available.
func (s *State) PushBlock(ctx context.Context, p *types.Signed[*types.LaneProposal]) error {
	h := p.Msg().Block().Header()
	if p.Key() != h.Lane() {
		return fmt.Errorf("signer %v does not match lane %v", p.Key(), h.Lane())
	}
	if _, err := s.data.Registry().VerifyInWindow(func(c *types.Committee) error {
		if err := p.Msg().Verify(c); err != nil {
			return err
		}
		return p.VerifySig(c)
	}); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.blocks[h.Lane()]
		if !ok {
			return ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() <= min(q.next, q.first+BlocksPerLane-1)
		}); err != nil {
			return err
		}
		// not needed any more
		if q.next != h.BlockNumber() {
			return nil
		}
		// Verify parent hash chain to prevent a malicious producer from
		// breaking the block chain, which would deadlock header reconstruction.
		// A mismatch means the producer equivocated (produced a different
		// chain than we already have). We log it to aid debugging stalled
		// lanes but do not return an error — the caller should not tear
		// down the peer connection over an equivocating producer.
		// NOTE: after pruning (q.first >= q.next), we cannot verify the parent
		// hash because the previous block is gone. This is safe because
		// headers() never follows the first block's parentHash in a LaneRange.
		if q.first < q.next {
			prevHash := q.q[q.next-1].Msg().Block().Header().Hash()
			if h.ParentHash() != prevHash {
				logger.Error("parent hash mismatch (producer equivocation)",
					"lane", h.Lane(),
					slog.Uint64("block", uint64(h.BlockNumber())),
					"got", h.ParentHash(),
					"want", prevHash)
				return nil
			}
		}
		q.pushBack(p)
		ctrl.Updated()
	}
	return nil
}

// PushVote pushes a LaneVote to the state.
// Waits until the lane has enough capacity for the new vote.
// It does NOT wait for the previous votes.
func (s *State) PushVote(ctx context.Context, vote *types.Signed[*types.LaneVote]) error {
	var epoch *types.Epoch
	for inner, ctrl := range s.inner.Lock() {
		// TODO(gprusak): we should wait only if LaneID is from the future.
		if err := ctrl.WaitUntil(ctx, func() bool { return inner.epoch.Committee().HasLane(vote.Key()) }); err != nil {
			return err
		}
		epoch = inner.epoch
	}
	if err := vote.Msg().Verify(epoch.Committee()); err != nil {
		return fmt.Errorf("vote.Verify(): %w", err)
	}
	if err := vote.VerifySig(epoch.Committee()); err != nil {
		return fmt.Errorf("vote.VerifySig(): %w", err)
	}
	h := vote.Msg().Header()
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.votes[h.Lane()]
		if !ok {
			return ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() < q.first+BlocksPerLane
		}); err != nil {
			return err
		}
		if h.BlockNumber() < q.first {
			return nil
		}
		for q.next <= h.BlockNumber() {
			q.pushBack(newBlockVotes())
		}
		if _, ok := q.q[h.BlockNumber()].pushVote(inner.epoch, vote); ok {
			ctrl.Updated()
		}
	}
	return nil
}

// headers collects headers for the given range.
func (s *State) headers(ctx context.Context, lr *types.LaneRange) ([]*types.BlockHeader, error) {
	// Empty range is always available.
	if lr.First() == lr.Next() {
		return nil, nil
	}
	want := lr.LastHash()
	headers := make([]*types.BlockHeader, lr.Next()-lr.First())
	for inner, ctrl := range s.inner.Lock() {
		q := inner.votes[lr.Lane()]
		for i := range headers {
			n := lr.Next() - types.BlockNumber(i) - 1 //nolint:gosec // i is bounded by len(headers) which is a small block range; no overflow risk
			for {
				// If pruned, then give up.
				if q.first > lr.First() {
					return nil, types.ErrPruned
				}
				// Check if we have the header.
				if entry, ok := q.q[n].byHash[want]; ok {
					h := entry.votes[0].Msg().Header()
					want = h.ParentHash()
					headers[len(headers)-i-1] = h
					break
				}
				// Otherwise, wait.
				if err := ctrl.Wait(ctx); err != nil {
					return nil, err
				}
			}
		}
	}
	return headers, nil
}

func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.Epoch, *types.FullCommitQC, error) {
	// Collect the CommitQC.
	epoch, qc, err := s.commitQC(ctx, n)
	if err != nil {
		return nil, nil, err
	}
	// Collect the headers from the votes.
	var commitHeaders []*types.BlockHeader
	for lane := range epoch.Committee().Lanes().All() {
		headers, err := s.headers(ctx, qc.LaneRange(lane))
		if err != nil {
			return nil, nil, err
		}
		commitHeaders = append(commitHeaders, headers...)
	}
	return epoch, types.NewFullCommitQC(qc, commitHeaders), nil
}

// WaitForLocalCapacity waits until the lane owned by this node has capacity for toProduce block.
func (s *State) WaitForLocalCapacity(ctx context.Context, toProduce types.BlockNumber) error {
	lane := s.key.Public()
	for inner, ctrl := range s.inner.Lock() {
		q := inner.blocks[lane]
		if err := ctrl.WaitUntil(ctx, func() bool {
			return toProduce < q.first+BlocksPerLane
		}); err != nil {
			return err
		}
	}
	return nil
}

// WaitForLaneQCs waits until there is at least 1 LaneQC (for the given epoch)
// with a block not finalized by prev.
func (s *State) WaitForLaneQCs(
	ctx context.Context, ep *types.Epoch, prev utils.Option[*types.CommitQC],
) (map[types.LaneID]*types.LaneQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		laneQCs := map[types.LaneID]*types.LaneQC{}
		for {
			for lane := range ep.Committee().Lanes().All() {
				first := types.LaneRangeOpt(prev, lane).Next()
				for i := range types.BlockNumber(types.MaxLaneRangeInProposal) {
					if qc, ok := inner.laneQC(lane, first+i); ok {
						laneQCs[lane] = qc
					} else {
						break
					}
				}
			}
			if len(laneQCs) > 0 {
				return laneQCs, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}

// ProduceLocalBlock appends a new block to the producers lane.
// Fails in case there is not enough capacity in the lane, or it is not the next block expected.
func (s *State) ProduceLocalBlock(n types.BlockNumber, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	return s.produceLocalBlock(n, s.key, payload)
}

// TODO: produceLocalBlock is a separate function for testing - consider improving the tests to use ProduceBlock only.
func (s *State) produceLocalBlock(n types.BlockNumber, key types.SecretKey, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	lane := key.Public()
	var result *types.Signed[*types.LaneProposal]
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.blocks[lane]
		if !ok {
			return nil, ErrBadLane
		}
		if n >= q.first+BlocksPerLane {
			return nil, fmt.Errorf("lane full")
		}
		if q.next != n {
			return nil, fmt.Errorf("unexpected block number: got %v, want %v", n, q.next)
		}
		var parent types.BlockHeaderHash
		if q.first < q.next {
			parent = q.q[q.next-1].Msg().Block().Header().Hash()
		}
		result = types.Sign(key, types.NewLaneProposal(types.NewBlock(lane, q.next, parent, payload)))
		q.pushBack(result)
		ctrl.Updated()
	}
	return result, nil
}

// Task inserting CommitQCs and local blocks to data state.
func (s *State) runPushQC(ctx context.Context) error {
	for n := types.RoadIndex(0); ; n = max(n+1, s.First()) {
		epoch, qc, err := s.fullCommitQC(ctx, n)
		if err != nil {
			if errors.Is(err, types.ErrPruned) {
				continue
			}
			return err
		}

		// Collect the blocks we have locally.
		c := epoch.Committee()
		var blocks []*types.Block
		for inner := range s.inner.Lock() {
			for lane := range c.Lanes().All() {
				lr := qc.QC().LaneRange(lane)
				for n := lr.First(); n < lr.Next(); n++ {
					// We are not expected to have all the blocks locally - only the available ones.
					if b, ok := inner.blocks[lr.Lane()].q[n]; ok {
						// We don't need to check the blocks against the headers,
						// as bad blocks will be filtered out by PushQC anyway.
						blocks = append(blocks, b.Msg().Block())
					}
				}
			}
		}
		if err := s.data.PushQC(ctx, qc, blocks); err != nil {
			return fmt.Errorf("s.data.PushQC(): %w", err)
		}
	}
}

// Task inserting AppQCs to data state.
func (s *State) runPushAppQC(ctx context.Context) error {
	for n := types.RoadIndex(0); ; n = max(n+1, s.First()) {
		appQC, err := s.appQC(ctx, n)
		if err != nil {
			if errors.Is(err, types.ErrPruned) {
				continue
			}
			return err
		}
		if err := s.data.PushAppQC(ctx, appQC); err != nil {
			return fmt.Errorf("s.data.PushAppQC(): %w", err)
		}
	}
}

func (s *State) runEvict(ctx context.Context) error {
	return s.data.Anchor().Iter(ctx, func(ctx context.Context, anchor utils.Option[data.Anchor]) error {
		if anchor, ok := anchor.Get(); ok {
			for inner, ctrl := range s.inner.Lock() {
				inner.prune(anchor)
				ctrl.Updated()
			}
		}
		return nil
	})
}

// Run runs the background tasks of the state.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("runEvict", func() error { return s.runEvict(ctx) })
		scope.SpawnNamed("runPersist", func() error { return s.runPersist(ctx) })
		scope.SpawnNamed("runPushQC", func() error { return s.runPushQC(ctx) })
		scope.SpawnNamed("runPushAppQC", func() error { return s.runPushAppQC(ctx) })
		return nil
	})
}

// runPersist is the main loop for the persist goroutine.
//  2. commitQCs.PruneAndPersist and each lane's blocks.PruneAndPersist run
//     concurrently via scope.Parallel (separate WALs, no early cancellation; first error
//     is returned after all tasks finish).
//     Each path publishes (markCommitQCsPersisted / markBlockPersisted) per entry so voting
//     unblocks ASAP.
func (s *State) runPersist(ctx context.Context) error {
	for {
		batch, err := s.collectPersistBatch(ctx)
		if err != nil {
			return err
		}

		// 2. Persist commit-QCs and per-lane blocks in parallel.
		// Callees handle empty inputs gracefully (no-op when nothing to write/truncate).
		if err := scope.Parallel(func(ps scope.ParallelScope) error {
			ps.Spawn(func() error {
				if err := s.persisters.commitQCs.PruneAndPersist(batch.commitQCs.first, batch.commitQCs.tail); err != nil {
					return err
				}
				if t := batch.commitQCs.tail; len(t) > 0 {
					s.markCommitQCsPersisted(t[len(t)-1])
				}
				return nil
			})
			for lane, batch := range batch.blocks {
				ps.Spawn(func() error {
					if err := s.persisters.blocks.PruneAndPersist(lane, batch.first, batch.tail); err != nil {
						return err
					}
					if n := len(batch.tail); n > 0 {
						header := batch.tail[n-1].Msg().Block().Header()
						s.markBlockPersisted(header.Lane(), header.BlockNumber()+1)
					}
					return nil
				})
			}
			return nil
		}); err != nil {
			return err
		}
	}
}

type batch[I any, T any] struct {
	first I
	tail  []T
}

type blocksBatch = batch[types.BlockNumber, *types.Signed[*types.LaneProposal]]
type commitQCsBatch = batch[types.RoadIndex, *types.CommitQC]

// persistBatch holds the data collected under lock for one persist iteration.
type persistBatch struct {
	epoch     *types.Epoch
	blocks    map[types.LaneID]blocksBatch
	commitQCs commitQCsBatch
}

// markBlockPersisted advances the per-lane block persistence cursor.
// Called after each block is persisted so that RecvBatch (and therefore
// voting) can unblock as soon as the block is durable. Safe for concurrent
// callers (acquires s.inner lock internally).
func (s *State) markBlockPersisted(lane types.LaneID, next types.BlockNumber) {
	for inner, ctrl := range s.inner.Lock() {
		inner.nextBlockToPersist[lane] = next
		ctrl.Updated()
	}
}

// markCommitQCsPersisted publishes the latest persisted CommitQC,
// gating consensus from advancing until the QC is durable.
func (s *State) markCommitQCsPersisted(qc *types.CommitQC) {
	for inner := range s.inner.Lock() {
		inner.persistedCommitQC.Store(utils.Some(qc))
	}
}

// collectPersistBatch waits for new blocks or commitQCs and collects them under lock.
func (s *State) collectPersistBatch(ctx context.Context) (*persistBatch, error) {
	for inner, ctrl := range s.inner.Lock() {
		// Derive the CommitQC persist cursor from persistedCommitQC. This is
		// safe because persistedCommitQC is only advanced by markCommitQCsPersisted
		// (after disk write) and on startup (from disk). prune() does NOT
		// update persistedCommitQC, so this always reflects persistence state.
		// The max clamp with commitQCs.first handles the case where prune()
		// fast-forwarded the queue past the cursor.
		next := types.NextIndexOpt(inner.persistedCommitQC.Load())
		if err := ctrl.WaitUntil(ctx, func() bool {
			for lane, q := range inner.blocks {
				if inner.nextBlockToPersist[lane] < q.next {
					return true
				}
			}
			return next < inner.roads.next
		}); err != nil {
			return nil, err
		}
		b := &persistBatch{
			blocks:    map[types.LaneID]blocksBatch{},
			commitQCs: commitQCsBatch{first: inner.roads.first},
		}
		for n := max(next, inner.roads.first); n < inner.roads.next; n++ {
			b.commitQCs.tail = append(b.commitQCs.tail, inner.roads.q[n].commitQC)
		}
		for lane, q := range inner.blocks {
			bb := blocksBatch{first: q.first}
			for n := max(inner.nextBlockToPersist[lane], q.first); n < q.next; n++ {
				bb.tail = append(bb.tail, q.q[n])
			}
			b.blocks[lane] = bb
		}
		return b, nil
	}
	panic("unreachable")
}
