package avail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// ErrBadLane .
var ErrBadLane = errors.New("bad lane")

const BlocksPerLane = 3 * BlocksPerLanePerCommit
const BlocksPerLanePerCommit = 10

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
	persisters persisters
}

// persisters holds all disk persistence components. Either all are present
// (real I/O) or all are no-op (testing). It is a pure I/O struct — all inner
// state access goes through State methods.
type persisters struct {
	pruneAnchor persist.Persister[*pb.PersistedAvailPruneAnchor]
	blocks      *persist.BlockPersister
	commitQCs   *persist.CommitQCPersister
}

// innerFile is the A/B file prefix for avail inner state persistence.
const innerFile = "avail_inner"

// PruneAnchor is the decoded form of the persisted prune anchor
// (AppQC + matching CommitQC pair). It serves as the crash-recovery
// pruning watermark.
type PruneAnchor struct {
	AppQC    *types.AppQC
	CommitQC *types.CommitQC
}

// PruneAnchorConv converts between PruneAnchor and its protobuf representation.
var PruneAnchorConv = protoutils.Conv[*PruneAnchor, *pb.PersistedAvailPruneAnchor]{
	Encode: func(a *PruneAnchor) *pb.PersistedAvailPruneAnchor {
		return &pb.PersistedAvailPruneAnchor{
			AppQc:    types.AppQCConv.Encode(a.AppQC),
			CommitQc: types.CommitQCConv.Encode(a.CommitQC),
		}
	},
	Decode: func(p *pb.PersistedAvailPruneAnchor) (*PruneAnchor, error) {
		if p.AppQc == nil || p.CommitQc == nil {
			return nil, fmt.Errorf("incomplete prune anchor: AppQC=%v CommitQC=%v", p.AppQc != nil, p.CommitQc != nil)
		}
		appQC, err := types.AppQCConv.Decode(p.AppQc)
		if err != nil {
			return nil, fmt.Errorf("decode AppQC: %w", err)
		}
		commitQC, err := types.CommitQCConv.Decode(p.CommitQc)
		if err != nil {
			return nil, fmt.Errorf("decode CommitQC: %w", err)
		}
		return &PruneAnchor{AppQC: appQC, CommitQC: commitQC}, nil
	},
}

// loadPersistedState creates persisters for the given directory option and loads
// any existing state from disk. When dir is None, all persisters are no-op
// and no state is loaded. When a prune anchor is present, stale commitQCs and
// blocks below the anchor are filtered out before returning.
func loadPersistedState(dir utils.Option[string]) (utils.Option[*loadedAvailState], persisters, error) {
	prunePersister, persistedPruneAnchor, err := persist.NewPersister[*pb.PersistedAvailPruneAnchor](dir, innerFile)
	if err != nil {
		return utils.None[*loadedAvailState](), persisters{}, fmt.Errorf("NewPersister %s: %w", innerFile, err)
	}

	bp, blocks, err := persist.NewBlockPersister(dir)
	if err != nil {
		return utils.None[*loadedAvailState](), persisters{}, fmt.Errorf("NewBlockPersister: %w", err)
	}

	cp, commitQCs, err := persist.NewCommitQCPersister(dir)
	if err != nil {
		return utils.None[*loadedAvailState](), persisters{}, fmt.Errorf("NewCommitQCPersister: %w", err)
	}

	pers := persisters{pruneAnchor: prunePersister, blocks: bp, commitQCs: cp}

	if _, ok := dir.Get(); !ok {
		return utils.None[*loadedAvailState](), pers, nil
	}

	loaded := &loadedAvailState{commitQCs: commitQCs, blocks: blocks}

	if raw, ok := persistedPruneAnchor.Get(); ok {
		anchor, err := PruneAnchorConv.Decode(raw)
		if err != nil {
			return utils.None[*loadedAvailState](), persisters{}, fmt.Errorf("decode prune anchor: %w", err)
		}
		loaded.pruneAnchor = utils.Some(anchor)

		anchorIdx := anchor.AppQC.Proposal().RoadIndex()
		filtered := commitQCs[:0]
		for _, lqc := range commitQCs {
			if lqc.Index >= anchorIdx {
				filtered = append(filtered, lqc)
			}
		}
		loaded.commitQCs = filtered

		for lane, bs := range blocks {
			first := anchor.CommitQC.LaneRange(lane).First()
			j := 0
			for j < len(bs) && bs[j].Number < first {
				j++
			}
			if j > 0 {
				loaded.blocks[lane] = bs[j:]
			}
		}
	}

	return utils.Some(loaded), pers, nil
}

// NewState constructs a new availability state.
// stateDir is None when persistence is disabled (testing only); a no-op
// persist goroutine still runs to bump cursors without disk I/O.
func NewState(key types.SecretKey, data *data.State, stateDir utils.Option[string]) (*State, error) {
	loaded, pers, err := loadPersistedState(stateDir)
	if err != nil {
		return nil, err
	}

	inner, err := newInner(data.Committee(), loaded)
	if err != nil {
		return nil, err
	}

	// Truncate WAL entries below the prune anchor that were filtered out by
	// loadPersistedState.
	if ls, ok := loaded.Get(); ok {
		if anchor, ok := ls.pruneAnchor.Get(); ok {
			for lane := range data.Committee().Lanes().All() {
				if err := pers.blocks.MaybePruneAndPersistLane(lane, utils.Some(anchor.CommitQC), nil, utils.None[func(*types.Signed[*types.LaneProposal])]()); err != nil {
					return nil, fmt.Errorf("prune stale block WAL entries: %w", err)
				}
			}
			if err := pers.commitQCs.MaybePruneAndPersist(utils.Some(anchor.CommitQC), nil, utils.None[func(*types.CommitQC)]()); err != nil {
				return nil, fmt.Errorf("prune stale commitQC WAL entries: %w", err)
			}
		}
	}

	return &State{
		key:        key,
		data:       data,
		inner:      utils.NewWatch(inner),
		persisters: pers,
	}, nil
}

func (s *State) FirstCommitQC() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.commitQCs.first
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
		return inner.latestCommitQC.Subscribe()
	}
	panic("unreachable")
}

func (s *State) waitForCommitQC(ctx context.Context, idx types.RoadIndex) error {
	_, err := s.LastCommitQC().Wait(ctx, func(qc utils.Option[*types.CommitQC]) bool {
		return types.NextIndexOpt(qc) > idx
	})
	return err
}

// LastAppQC returns the latest observed AppQC.
func (s *State) LastAppQC() utils.Option[*types.AppQC] {
	for inner := range s.inner.Lock() {
		return inner.latestAppQC
	}
	panic("unreachable")
}

// WaitForAppQC waits until there is an AppQC for the given index or higher.
// Returns this AppQC and the corresponding CommitQC.
// Together they provide enough information to prune the availability state.
func (s *State) WaitForAppQC(ctx context.Context, idx types.RoadIndex) (*types.AppQC, *types.CommitQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		for {
			if appQC, ok := inner.latestAppQC.Get(); ok {
				if x := appQC.Proposal().RoadIndex(); x >= idx && inner.commitQCs.next > x {
					return appQC, inner.commitQCs.q[x], nil
				}
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, nil, err
			}
		}
	}
	panic("unreachable")
}

// CommitQC returns the CommitQC for the given index.
func (s *State) CommitQC(ctx context.Context, idx types.RoadIndex) (*types.CommitQC, error) {
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return nil, err
	}
	for inner := range s.inner.Lock() {
		if idx < inner.commitQCs.first {
			return nil, data.ErrPruned
		}
		return inner.commitQCs.q[idx], nil
	}
	panic("unreachable")
}

// PushCommitQC pushes a CommitQC to the state.
// Waits until all previous CommitQCs are pushed.
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	idx := qc.Proposal().Index()
	if idx > 0 {
		if err := s.waitForCommitQC(ctx, idx-1); err != nil {
			return err
		}
	}
	if err := qc.Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if idx != inner.commitQCs.next {
			return nil
		}
		inner.commitQCs.pushBack(qc)
		// The persist goroutine publishes latestCommitQC after writing to disk
		// (or immediately for no-op persisters), so consensus won't advance
		// until the CommitQC is durable.
		ctrl.Updated()
		return nil
	}
	return nil
}

// PushAppVote pushes an AppVote to the state.
func (s *State) PushAppVote(ctx context.Context, v *types.Signed[*types.AppVote]) error {
	if err := v.VerifySig(s.data.Committee()); err != nil {
		return fmt.Errorf("v.VerifySig(): %w", err)
	}
	idx := v.Msg().Proposal().RoadIndex()
	// Wait for the corresponding commitQC.
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return err
	}
	for inner, ctrl := range s.inner.Lock() {
		// Early exit if not useful (we collect <=1 AppQC per road index).
		if idx < types.NextOpt(inner.latestAppQC) {
			return nil
		}
		// Verify the vote against the CommitQC.
		qc := inner.commitQCs.q[idx]
		if err := v.Msg().Proposal().Verify(s.data.Committee(), qc); err != nil {
			return fmt.Errorf("invalid vote: %w", err)
		}
		// Push the vote.
		n := v.Msg().Proposal().GlobalNumber()
		q := inner.appVotes
		for q.next <= n {
			q.pushBack(newAppVotes())
		}
		appQC, ok := q.q[n].pushVote(s.data.Committee(), v)
		if !ok {
			return nil
		}
		updated, err := inner.prune(s.data.Committee(), appQC, qc)
		if err != nil {
			return err
		}
		if updated {
			ctrl.Updated()
		}
	}
	return nil
}

// PushAppQC pushes an AppQC to the state. It requires a corresponding CommitQC
// as a justification.
func (s *State) PushAppQC(appQC *types.AppQC, commitQC *types.CommitQC) error {
	// Check whether it is needed before verifying.
	for inner := range s.inner.Lock() {
		if types.NextOpt(inner.latestAppQC) > appQC.Proposal().RoadIndex() {
			return nil
		}
	}
	c := s.data.Committee()
	if err := appQC.Verify(c); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	if err := commitQC.Verify(c); err != nil {
		return fmt.Errorf("commitQC.Verify(): %w", err)
	}
	if appQC.Proposal().RoadIndex() != commitQC.Proposal().Index() {
		return fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", appQC.Proposal().RoadIndex(), commitQC.Proposal().Index())
	}
	// Defense-in-depth check, it should never happen that >f validators sign
	// a proposal which does not match the commitQC's global range.
	if !commitQC.GlobalRange(c).Has(appQC.Proposal().GlobalNumber()) {
		return fmt.Errorf("appQC GlobalNumber not in commitQC range")
	}
	for inner, ctrl := range s.inner.Lock() {
		updated, err := inner.prune(s.data.Committee(), appQC, commitQC)
		if err != nil {
			return err
		}
		if updated {
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
			return nil, data.ErrPruned
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
	if err := p.Msg().Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	if err := p.VerifySig(s.data.Committee()); err != nil {
		return fmt.Errorf("p.VerifySig(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.blocks[h.Lane()]
		if !ok {
			return ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() <= min(q.next, inner.persistedBlockStart[h.Lane()]+BlocksPerLane-1)
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
	if err := vote.Msg().Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("vote.Msg().Verify(): %w", err)
	}
	if err := vote.VerifySig(s.data.Committee()); err != nil {
		return fmt.Errorf("vote.VerifySig(): %w", err)
	}
	h := vote.Msg().Header()
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.votes[h.Lane()]
		if !ok {
			return ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() < inner.persistedBlockStart[h.Lane()]+BlocksPerLane
		}); err != nil {
			return err
		}
		if h.BlockNumber() < q.first {
			return nil
		}
		for q.next <= h.BlockNumber() {
			q.pushBack(newBlockVotes())
		}
		if _, ok := q.q[h.BlockNumber()].pushVote(s.data.Committee(), vote); ok {
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
					return nil, data.ErrPruned
				}
				// Check if we have the header.
				if vs := q.q[n].byHeader[want]; len(vs) > 0 {
					h := vs[0].Msg().Header()
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

func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
	// Collect the CommitQC.
	qc, err := s.CommitQC(ctx, n)
	if err != nil {
		return nil, err
	}
	// Collect the headers from the votes.
	var commitHeaders []*types.BlockHeader
	for lane := range s.data.Committee().Lanes().All() {
		headers, err := s.headers(ctx, qc.LaneRange(lane))
		if err != nil {
			return nil, err
		}
		commitHeaders = append(commitHeaders, headers...)
	}
	return types.NewFullCommitQC(qc, commitHeaders), nil
}

// WaitForCapacity waits until the given lane has enough capacity for a new block.
func (s *State) WaitForCapacity(ctx context.Context, lane types.LaneID) error {
	for inner, ctrl := range s.inner.Lock() {
		q := inner.blocks[lane]
		if err := ctrl.WaitUntil(ctx, func() bool {
			return q.next < inner.persistedBlockStart[lane]+BlocksPerLane
		}); err != nil {
			return err
		}
	}
	return nil
}

// WaitForLaneQCs waits until there is at least 1 LaneQC with a block not finalized by prev.
func (s *State) WaitForLaneQCs(
	ctx context.Context, prev utils.Option[*types.CommitQC],
) (map[types.LaneID]*types.LaneQC, error) {
	c := s.data.Committee()
	for inner, ctrl := range s.inner.Lock() {
		laneQCs := map[types.LaneID]*types.LaneQC{}
		for {
			for lane := range c.Lanes().All() {
				first := types.LaneRangeOpt(prev, lane).Next()
				for i := range types.BlockNumber(BlocksPerLanePerCommit) {
					if qc, ok := inner.laneQC(c, lane, first+i); ok {
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

// ProduceBlock appends a new block to the producers lane.
// Blocks until the lane has enough capacity for the new block.
func (s *State) ProduceBlock(ctx context.Context, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	return s.produceBlock(ctx, s.key, payload)
}

// TODO: produceBlock is a separate function for testing - consider improving the tests to use ProduceBlock only.
func (s *State) produceBlock(ctx context.Context, key types.SecretKey, payload *types.Payload) (*types.Signed[*types.LaneProposal], error) {
	lane := key.Public()
	var result *types.Signed[*types.LaneProposal]
	for inner, ctrl := range s.inner.Lock() {
		q, ok := inner.blocks[lane]
		if !ok {
			return nil, ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return q.next < inner.persistedBlockStart[lane]+BlocksPerLane
		}); err != nil {
			return nil, err
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

// Run runs the background tasks of the state.
//
// Goroutines: this method spawns long-lived goroutines via scope.SpawnNamed
// (the persist loop and the FullCommitQC→data-state pusher). Inside
// runPersist, scope.Parallel spawns short-lived goroutines for concurrent
// per-lane block and commit-QC persistence. The persist package itself does
// not spawn goroutines.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("persist", func() error {
			return s.runPersist(ctx, s.persisters)
		})
		// Task inserting FullCommitQCs and local blocks to data state.
		scope.SpawnNamed("s.data.PushQC", func() error {
			c := s.data.Committee()
			for n := types.RoadIndex(0); ; n = max(n+1, s.FirstCommitQC()) {
				qc, err := s.fullCommitQC(ctx, n)
				if err != nil {
					if errors.Is(err, data.ErrPruned) {
						continue
					}
					return err
				}

				// Collect the blocks we have locally.
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
		})
		return nil
	})
}

// runPersist is the main loop for the persist goroutine.
// Write order:
//  1. Prune anchor (AppQC + CommitQC pair) — the crash-recovery watermark (sequential).
//  2. commitQCs.MaybePruneAndPersist and each lane's blocks.MaybePruneAndPersistLane run
//     concurrently via scope.Parallel (separate WALs, no early cancellation; first error
//     is returned after all tasks finish).
//     Each path publishes (markCommitQCsPersisted / markBlockPersisted) per entry so voting
//     unblocks ASAP.
//
// The prune anchor is a pruning watermark: on restart we resume from it.
//
// TODO: use a single WAL for anchor and CommitQCs to make
// this atomic rather than relying on write order.
func (s *State) runPersist(ctx context.Context, pers persisters) error {
	var lastPersistedAppQCNext types.RoadIndex
	for {
		batch, err := s.collectPersistBatch(ctx, lastPersistedAppQCNext)
		if err != nil {
			return err
		}

		// Prune CommitQC anchor: same Option drives commit-QC WAL and per-lane block WAL
		// (truncate-then-append below this QC).
		var anchorQC utils.Option[*types.CommitQC]
		// 1. Persist prune anchor first — establishes the crash-recovery watermark.
		if anchor, ok := batch.pruneAnchor.Get(); ok {
			if err := pers.pruneAnchor.Persist(PruneAnchorConv.Encode(anchor)); err != nil {
				return fmt.Errorf("persist prune anchor: %w", err)
			}
			s.advancePersistedBlockStart(anchor.CommitQC)
			lastPersistedAppQCNext = anchor.CommitQC.Proposal().Index() + 1
			anchorQC = utils.Some(anchor.CommitQC)
		}

		markBlock := func(p *types.Signed[*types.LaneProposal]) {
			header := p.Msg().Block().Header()
			s.markBlockPersisted(header.Lane(), header.BlockNumber()+1)
		}

		blocksByLane := make(map[types.LaneID][]*types.Signed[*types.LaneProposal], s.data.Committee().Lanes().Len())
		for _, proposal := range batch.blocks {
			lane := proposal.Msg().Block().Header().Lane()
			blocksByLane[lane] = append(blocksByLane[lane], proposal)
		}

		// 2. Persist commit-QCs and per-lane blocks in parallel.
		// Callees handle empty inputs gracefully (no-op when nothing to write/truncate).
		if err := scope.Parallel(func(ps scope.ParallelScope) error {
			ps.Spawn(func() error {
				return pers.commitQCs.MaybePruneAndPersist(anchorQC, batch.commitQCs, utils.Some(func(qc *types.CommitQC) {
					s.markCommitQCsPersisted(qc)
				}))
			})
			for lane := range s.data.Committee().Lanes().All() {
				proposals := blocksByLane[lane]
				ps.Spawn(func() error {
					return pers.blocks.MaybePruneAndPersistLane(lane, anchorQC, proposals, utils.Some(markBlock))
				})
			}
			return nil
		}); err != nil {
			return err
		}
	}
}

// persistBatch holds the data collected under lock for one persist iteration.
type persistBatch struct {
	blocks      []*types.Signed[*types.LaneProposal]
	commitQCs   []*types.CommitQC
	pruneAnchor utils.Option[*PruneAnchor]
}

// advancePersistedBlockStart updates the per-lane block admission watermark
// after durably writing the prune anchor. This unblocks PushBlock/ProduceBlock
// waiters that are gated on persistedBlockStart + BlocksPerLane.
func (s *State) advancePersistedBlockStart(commitQC *types.CommitQC) {
	for inner, ctrl := range s.inner.Lock() {
		for lane := range inner.blocks {
			start := commitQC.LaneRange(lane).First()
			if start > inner.persistedBlockStart[lane] {
				inner.persistedBlockStart[lane] = start
			}
		}
		ctrl.Updated()
	}
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
	for inner, ctrl := range s.inner.Lock() {
		inner.latestCommitQC.Store(utils.Some(qc))
		ctrl.Updated()
	}
}

// collectPersistBatch waits for new blocks or commitQCs and collects them under lock.
func (s *State) collectPersistBatch(ctx context.Context, lastPersistedAppQCNext types.RoadIndex) (persistBatch, error) {
	var b persistBatch
	for inner, ctrl := range s.inner.Lock() {
		// Derive the CommitQC persist cursor from latestCommitQC. This is
		// safe because latestCommitQC is only advanced by markCommitQCsPersisted
		// (after disk write) and on startup (from disk). prune() does NOT
		// update latestCommitQC, so this always reflects persistence state.
		// The max clamp with commitQCs.first handles the case where prune()
		// fast-forwarded the queue past the cursor.
		commitQCNext := types.NextIndexOpt(inner.latestCommitQC.Load())
		if err := ctrl.WaitUntil(ctx, func() bool {
			if types.NextOpt(inner.latestAppQC) != lastPersistedAppQCNext {
				return true
			}
			for lane, q := range inner.blocks {
				if inner.nextBlockToPersist[lane] < q.next {
					return true
				}
			}
			return commitQCNext < inner.commitQCs.next
		}); err != nil {
			return b, err
		}
		for lane, q := range inner.blocks {
			start := max(inner.nextBlockToPersist[lane], q.first)
			for n := start; n < q.next; n++ {
				b.blocks = append(b.blocks, q.q[n])
			}
		}
		commitQCNext = max(commitQCNext, inner.commitQCs.first)
		for n := commitQCNext; n < inner.commitQCs.next; n++ {
			b.commitQCs = append(b.commitQCs, inner.commitQCs.q[n])
		}
		if types.NextOpt(inner.latestAppQC) != lastPersistedAppQCNext {
			if appQC, ok := inner.latestAppQC.Get(); ok {
				idx := appQC.Proposal().RoadIndex()
				if qc, ok := inner.commitQCs.q[idx]; ok {
					b.pruneAnchor = utils.Some(&PruneAnchor{
						AppQC:    appQC,
						CommitQC: qc,
					})
				}
			}
		}
	}
	return b, nil
}
