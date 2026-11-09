// Package avail is the Data Availability Plane and Ordered Event Log: lane
// blocks, CommitQC/AppQC buffers, and pruning.
package avail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail/metrics"
	consmetrics "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// ErrLaneClosed is returned when a lane's maps are gone (closed) or by
// SubscribeLaneProposals.Recv after that lane's maps are dropped.
var ErrLaneClosed = errors.New("lane closed")

const BlocksPerLane = 3 * types.MaxLaneRangeInProposal

var (
	meters     = metrics.Get()
	consMeters = consmetrics.Get()
)

// State represents the Data Availability Plane and Ordered Event Log.
// Although it resides in a sub-package, it serves as the "source of truth" for:
// - Block data: storing and disseminating raw transaction payloads (lanes).
// - Finality tracking: acting as a persistent buffer for CommitQCs and AppQCs.
// - Pruning: managing memory by deleting data once enough execution proofs (AppVotes) are seen.
//
// NOTE: This component is more than an observer; it actively aggregates AppVotes
// to trigger internal pruning, which allows it to manage memory independently
// of the main consensus loop.
//
// Lane maps and WALs are retained while either anchor epoch contains that LaneID.
type State struct {
	key   types.SecretKey
	data  *data.State
	inner utils.Watch[*inner]
	spec  utils.AtomicRecv[types.ConsensusSpec]

	// persisters groups all disk persistence components.
	// Always initialized: real when stateDir is set, no-op otherwise.
	persisters *persisters
}

func (s *State) PublicKey() types.PublicKey {
	return s.key.Public()
}

func (s *State) LocalLane() utils.Option[types.LaneID] {
	return s.Lane(s.key.Public())
}

func (s *State) Lane(pk types.PublicKey) utils.Option[types.LaneID] {
	return s.ConsensusSpec().Load().Epoch.Committee().Lane(pk)
}

func (s *State) WaitForLocalLane(ctx context.Context) (types.LaneID, error) {
	return s.WaitForNextLane(ctx, s.key.Public(), utils.None[types.LaneID]())
}

func (s *State) WaitUntilClosed(ctx context.Context, lane types.LaneID) error {
	_, err := s.ConsensusSpec().Wait(ctx, func(spec types.ConsensusSpec) bool {
		return spec.Epoch.IsClosed(lane)
	})
	return err
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
		_ = bp.Close()
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
	inner, err := newInner(data.Anchor().Load(), data.GenesisTimestamp(), loaded)
	if err != nil {
		_ = pers.close()
		return nil, err
	}
	// Remove orphaned lanes on disk
	if err := persist.SyncLanes(pers.blocks, inner.lanes); err != nil {
		_ = pers.close()
		return nil, fmt.Errorf("sync lane WALs to memory set: %w", err)
	}
	return &State{
		key:        key,
		data:       data,
		inner:      utils.NewWatch(inner),
		spec:       inner.consensusSpec.Subscribe(),
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

// ConsensusSpec returns a receiver of the durable CommitQC tip paired
// with the epoch of the RoadIndex that follows it. CommitQC is None before
// the first tip; until then Epoch is genesis epoch 0.
func (s *State) ConsensusSpec() utils.AtomicRecv[types.ConsensusSpec] {
	return s.spec
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

// PushCommitQC admits a CommitQC once its epoch is applied and it is the next
// road. Stale QCs are a no-op.
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	idx := qc.Index()
	want := qc.View().EpochIndex
	var epoch *types.Epoch
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			return inner.applied().EpochIndex() >= want && idx <= inner.roads.next
		}); err != nil {
			return err
		}
		if inner.applied().EpochIndex() != want || inner.roads.next > idx {
			return nil
		}
		epoch = inner.applied()
	}
	if err := qc.Verify(epoch); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if idx != inner.roads.next || inner.applied().EpochIndex() != want {
			return nil
		}
		inner.roads.pushBack(newRoad(qc, epoch))
		metrics.ObserveCommitQC(qc)
		leader := epoch.Committee().Leader(qc.View())
		consMeters.Commits.WithLabelValues(leader.ED25519().Address().String()).Add(1)
		// The persist goroutine advances nextCommitQCToPersist after writing to disk
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
	idx := v.Msg().Proposal().Index()
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
	c := epoch.Committee()
	if !c.HasReplica(v.Key()) {
		return fmt.Errorf("%q is not a replica", v.Key())
	}
	if err := v.VerifySig(); err != nil {
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
		if ls, ok := inner.lanes[lane]; ok {
			return ls.blocks.next
		}
	}
	return 0
}

// Block waits for block n of the given lane. It returns ErrPruned for pruned
// blocks and past lanes. It waits for future lanes to become available.
func (s *State) Block(ctx context.Context, lane types.LaneID, n types.BlockNumber) (*types.Signed[*types.LaneProposal], error) {
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			if ls, ok := inner.lanes[lane]; ok {
				return n < ls.blocks.next
			}
			return inner.anchor.Epochs[1].IsClosed(lane)
		}); err != nil {
			return nil, err
		}
		ls, ok := inner.lanes[lane]
		if !ok {
			return nil, types.ErrPruned
		}
		if n < ls.blocks.first {
			return nil, types.ErrPruned
		}
		return ls.blocks.q[n], nil
	}
	panic("unreachable")
}

// PushBlock pushes a block to the state once its lane is in the applied committee
// and all previous blocks are available. Blocks for past lanes are silently dropped.
func (s *State) PushBlock(ctx context.Context, p *types.Signed[*types.LaneProposal]) error {
	h := p.Msg().Block().Header()
	if p.Key() != h.Lane().Validator {
		return fmt.Errorf("signer %v does not match lane %v", p.Key(), h.Lane())
	}
	lane := h.Lane()
	n := h.BlockNumber()
	if err := p.Msg().Verify(); err != nil {
		return fmt.Errorf("Verify(): %w", err)
	}
	spec, err := s.spec.Wait(ctx, func(spec types.ConsensusSpec) bool {
		return lane.Joined <= spec.Epoch.EpochIndex()
	})
	if err != nil {
		return err
	}
	if !spec.Epoch.Committee().HasLane(lane) {
		return nil
	}
	if err := p.VerifySig(); err != nil {
		return fmt.Errorf("VerifySig(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			ls, ok := inner.lanes[lane]
			return !ok || n <= min(ls.blocks.next, ls.blocks.first+BlocksPerLane-1)
		}); err != nil {
			return err
		}
		if !inner.applied().Committee().HasLane(lane) {
			return nil
		}
		ls, ok := inner.lanes[lane]
		if !ok || ls.blocks.next != n {
			return nil
		}
		// Parent is checked only while the predecessor is in the suffix.
		// See blockQueue. A mismatch is logged and dropped; the peer stays up.
		if ls.blocks.first < ls.blocks.next {
			want := ls.blocks.q[ls.blocks.next-1].Msg().Block().Header().Hash()
			if h.ParentHash() != want {
				logger.Error("parent hash mismatch (producer equivocation)",
					"lane", lane,
					slog.Uint64("block", uint64(n)),
					"got", h.ParentHash(),
					"want", want)
				return nil
			}
		}
		ls.blocks.pushBack(p)
		ctrl.Updated()
	}
	meters.LaneBlockReceive.Observe(time.Since(p.Msg().Block().Payload().CreatedAt()).Seconds())
	return nil
}

// PushVote pushes a LaneVote to the state.
// Waits until the lane has enough capacity for the new vote.
// It does NOT wait for the previous votes.
// Accepts a vote valid under the applied or Anchor epoch (header evidence may
// come from either); LaneQC weight uses only the applied epoch.
func (s *State) PushVote(ctx context.Context, vote *types.Signed[*types.LaneVote]) error {
	h := vote.Msg().Header()
	lane := h.Lane()
	n := h.BlockNumber()
	if err := vote.VerifySig(); err != nil {
		return fmt.Errorf("VerifySig(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			ls, ok := inner.lanes[lane]
			if !ok {
				return true
			}
			return n < ls.votes.first+BlocksPerLane
		}); err != nil {
			return err
		}
		ls, ok := inner.lanes[lane]
		if !ok {
			return nil
		}
		if n < ls.votes.first {
			return nil
		}
		// TODO: PushVote should block on votes for future-lanes from future-voters
		if !inner.epochForVote(vote).IsPresent() {
			return nil
		}
		applied := inner.applied()
		for ls.votes.next <= n {
			ls.votes.pushBack(newBlockVotes())
		}
		if ls.votes.q[n].pushVote(applied, vote) {
			ctrl.Updated()
		}
	}
	return nil
}

// headers collects headers for the given range under ep (the CommitQC's road epoch).
// Returns ErrPruned if the lane is closed under ep or the range is unavailable.
// Does not wait for future lanes.
func (s *State) headers(ctx context.Context, ep *types.Epoch, lr *types.LaneRange) ([]*types.BlockHeader, error) {
	// Empty range is always available.
	if lr.First() == lr.Next() {
		return nil, nil
	}
	want := lr.LastHash()
	headers := make([]*types.BlockHeader, lr.Next()-lr.First())
	for inner, ctrl := range s.inner.Lock() {
		if ep.IsClosed(lr.Lane()) {
			return nil, types.ErrPruned
		}
		for i := range headers {
			n := lr.Next() - types.BlockNumber(i) - 1 //nolint:gosec // i is bounded by len(headers) which is a small block range; no overflow risk
			for {
				ls, ok := inner.lanes[lr.Lane()]
				if !ok {
					return nil, types.ErrPruned
				}
				// If pruned, then give up.
				if ls.votes.first > lr.First() {
					return nil, types.ErrPruned
				}
				// Check if we have the header.
				if bv, ok := ls.votes.q[n]; ok {
					if h, ok := bv.header(want).Get(); ok {
						want = h.ParentHash()
						headers[len(headers)-i-1] = h
						break
					}
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

// fullCommitQC builds a FullCommitQC for road index n.
// Headers are collected from votes; this does not wait for future lanes.
func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
	// Collect the CommitQC.
	epoch, qc, err := s.commitQC(ctx, n)
	if err != nil {
		return nil, err
	}
	// Collect the headers from the votes.
	var commitHeaders []*types.BlockHeader
	for lane := range epoch.Committee().Lanes().All() {
		headers, err := s.headers(ctx, epoch, qc.LaneRange(lane))
		if err != nil {
			return nil, err
		}
		commitHeaders = append(commitHeaders, headers...)
	}
	return types.NewFullCommitQC(qc, commitHeaders), nil
}

// WaitForCapacity waits until lane has room for toProduce.
// Returns ErrLaneClosed if the lane map is missing. Does not wait for unretained lanes.
func (s *State) WaitForCapacity(ctx context.Context, lane types.LaneID, toProduce types.BlockNumber) error {
	var blocked bool
	var start time.Time
	defer func() {
		if blocked {
			meters.LaneCapacityWait.Observe(time.Since(start).Seconds())
		}
	}()
	for inner, ctrl := range s.inner.Lock() {
		ready := func() bool {
			ls, ok := inner.lanes[lane]
			return !ok || toProduce < ls.blocks.first+BlocksPerLane
		}
		if !ready() {
			meters.LaneCapacityInFlight.Add(1)
			defer meters.LaneCapacityInFlight.Add(-1)
			start = time.Now()
			blocked = true
			if err := ctrl.WaitUntil(ctx, ready); err != nil {
				return err
			}
		}
		if _, ok := inner.lanes[lane]; !ok {
			return ErrLaneClosed
		}
	}
	return nil
}

// WaitForLaneQCs waits until there is at least 1 LaneQC (for the given epoch)
// with a block not finalized by prev.
func (s *State) WaitForLaneQCs(
	ctx context.Context, ep *types.Epoch, prev utils.Option[*types.CommitQC],
) (map[types.LaneID]*types.LaneQC, error) {
	var blocked bool
	var start time.Time
	defer func() {
		if blocked {
			meters.LaneQCWait.Observe(time.Since(start).Seconds())
		}
	}()
	for inner, ctrl := range s.inner.Lock() {
		laneQCs := map[types.LaneID]*types.LaneQC{}
		for {
			for lane := range ep.Committee().Lanes().All() {
				first := types.LaneRangeOpt(prev, lane).Next()
				for i := range types.BlockNumber(types.MaxLaneRangeInProposal) {
					if qc, ok := inner.laneQC(lane, first+i).Get(); ok {
						laneQCs[lane] = qc
					} else {
						break
					}
				}
			}
			if len(laneQCs) > 0 {
				return laneQCs, nil
			}
			if !blocked {
				meters.LaneQCInFlight.Add(1)
				defer meters.LaneQCInFlight.Add(-1)
				start = time.Now()
				blocked = true
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}

// LaneTip is how far one lane's blocks have reached on this node.
type LaneTip struct {
	// WithCommitQC is one past the highest block finalized by the latest persisted CommitQC.
	WithCommitQC types.BlockNumber
	// WithLaneQC is one past the highest block with a LaneQC, and at least WithCommitQC.
	WithLaneQC types.BlockNumber
	// Held is one past the highest block this node holds, and at least WithLaneQC.
	Held types.BlockNumber
}

// LaneTips returns how far each lane of the applied committee has reached on this node.
func (s *State) LaneTips() map[types.LaneID]LaneTip {
	for inner := range s.inner.Lock() {
		committed := inner.persistedCommitQC()
		tips := map[types.LaneID]LaneTip{}
		for lane := range inner.applied().Committee().Lanes().All() {
			t := LaneTip{WithCommitQC: types.LaneRangeOpt(committed, lane).Next()}
			t.WithLaneQC = t.WithCommitQC
			t.Held = t.WithLaneQC
			if ls, ok := inner.lanes[lane]; ok {
				for n := ls.votes.next; n > max(ls.votes.first, t.WithCommitQC); n-- {
					if ls.votes.q[n-1].qc.IsPresent() {
						t.WithLaneQC = n
						break
					}
				}
				t.Held = max(t.WithLaneQC, ls.blocks.next)
			}
			tips[lane] = t
		}
		return tips
	}
	panic("unreachable")
}

// ProduceLocalBlock appends a new block to the producers lane.
// Fails in case there is not enough capacity in the lane, or it is not the next block expected.
func (s *State) ProduceLocalBlock(lane types.LaneID, n types.BlockNumber, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	if lane.Validator != s.key.Public() {
		return nil, fmt.Errorf("ProduceLocalBlock: lane validator %v != local %v", lane.Validator, s.key.Public())
	}
	var result *types.Signed[*types.LaneProposal]
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes[lane]
		if !ok {
			return nil, ErrLaneClosed
		}
		if n >= ls.blocks.first+BlocksPerLane {
			return nil, fmt.Errorf("lane full")
		}
		if ls.blocks.next != n {
			return nil, fmt.Errorf("unexpected block number: got %v, want %v", n, ls.blocks.next)
		}
		result = types.Sign(s.key, types.NewLaneProposal(types.NewBlock(lane, ls.blocks.next, ls.blocks.parentHash(), payload)))
		ls.blocks.pushBack(result)
		ctrl.Updated()
	}
	metrics.ObserveProducedTxs(len(payload.Txs()))
	return result, nil
}

// Task inserting CommitQCs and local blocks to data state.
func (s *State) runPushPartialBlock(ctx context.Context) error {
	for n := s.First(); ; n = max(n+1, s.First()) {
		qc, err := s.fullCommitQC(ctx, n)
		if err != nil {
			if errors.Is(err, types.ErrPruned) {
				continue
			}
			return err
		}

		// Collect the blocks we have locally.
		blocks := make(map[uint64]*types.Block, len(qc.Headers()))
		for inner := range s.inner.Lock() {
			for i, h := range qc.Headers() {
				if ls, ok := inner.lanes[h.Lane()]; ok {
					// We are not expected to have all the blocks locally - only the available ones.
					if b, ok := ls.blocks.q[h.BlockNumber()]; ok {
						// We don't need to check the blocks against the headers,
						// as bad blocks will be filtered out by PushPartialBlock anyway.
						blocks[uint64(i)] = b.Msg().Block()
					}
				}
			}
		}
		if err := s.data.PushPartialBlock(ctx, qc, blocks); err != nil {
			return fmt.Errorf("s.data.PushPartialBlock(): %w", err)
		}
	}
}

// Task inserting AppQCs to data state.
func (s *State) runPushAppQC(ctx context.Context) error {
	for n := s.First(); ; n = max(n+1, s.First()) {
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
	return s.data.Anchor().Iter(ctx, func(ctx context.Context, anchor data.Anchor) error {
		for inner, ctrl := range s.inner.Lock() {
			inner.setAnchor(anchor)
			ctrl.Updated()
		}
		return nil
	})
}

// Run runs the background tasks of the state.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("runEvict", func() error { return s.runEvict(ctx) })
		scope.SpawnNamed("runPersist", func() error { return s.runPersist(ctx) })
		scope.SpawnNamed("runPushPartialBlock", func() error { return s.runPushPartialBlock(ctx) })
		scope.SpawnNamed("runPushAppQC", func() error { return s.runPushAppQC(ctx) })
		return nil
	})
}

// runPersist persists CommitQCs and per-lane blocks until ctx is cancelled.
func (s *State) runPersist(ctx context.Context) error {
	for ctx.Err() == nil {
		batch, err := s.collectPersistBatch(ctx)
		if err != nil {
			return err
		}

		if err := persist.SyncLanes(s.persisters.blocks, batch.blocks); err != nil {
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
					if t := batch.tail; len(t) > 0 {
						s.setNextBlockToPersist(lane, t[len(t)-1].Msg().Block().Header().BlockNumber()+1)
					}
					return nil
				})
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return ctx.Err()
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

// setNextBlockToPersist advances the per-lane persistence cursor to next when
// next is ahead of the current cursor. RecvBatch yields headers strictly below
// this cursor, so a successful flush unblocks voting. Safe for concurrent callers.
func (s *State) setNextBlockToPersist(lane types.LaneID, next types.BlockNumber) {
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes[lane]
		if !ok {
			return
		}
		if ls.nextBlockToPersist >= next {
			// setAnchor may have already advanced the cursor while this batch was on disk.
			return
		}
		ls.nextBlockToPersist = next
		ctrl.Updated()
	}
}

// markCommitQCsPersisted publishes qc as the durable tip when it is not behind
// the current tip, then refreshes ConsensusSpec.
func (s *State) markCommitQCsPersisted(qc *types.CommitQC) {
	for inner, ctrl := range s.inner.Lock() {
		if qc.Index()+1 < inner.nextCommitQCToPersist {
			// setAnchor may have jumped the empty-queue tip to the Anchor while this
			// persist batch was still on disk.
			return
		}
		inner.nextCommitQCToPersist = qc.Index() + 1
		inner.advance()
		ctrl.Updated()
	}
}

// collectPersistBatch returns the next non-empty persist batch.
func (s *State) collectPersistBatch(ctx context.Context) (*persistBatch, error) {
	for inner, ctrl := range s.inner.Lock() {
		for {
			// Pruning keeps the persistence cursor at or beyond roads.first.
			b := &persistBatch{
				blocks:    map[types.LaneID]blocksBatch{},
				commitQCs: commitQCsBatch{first: inner.roads.first},
			}
			for n := inner.nextCommitQCToPersist; n < inner.roads.next; n++ {
				b.commitQCs.tail = append(b.commitQCs.tail, inner.roads.q[n].commitQC)
			}
			nonEmpty := len(b.commitQCs.tail) > 0
			for lane, ls := range inner.lanes {
				cursor := ls.nextBlockToPersist
				bb := blocksBatch{first: ls.blocks.retentionFloor()}
				if p, ok := ls.blocks.unpersistedLocalTip(cursor).Get(); ok {
					bb.tail = append(bb.tail, p)
				}
				for n := max(cursor, ls.blocks.first); n < ls.blocks.next; n++ {
					bb.tail = append(bb.tail, ls.blocks.q[n])
				}
				b.blocks[lane] = bb
				nonEmpty = nonEmpty || len(bb.tail) > 0
			}
			if nonEmpty {
				return b, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}
