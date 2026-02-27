package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
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
// State is the block availability state provided by the node for consensus.
// It contains:
// * commitQCs
// * locally available blocks
// * availability votes (LaneVotes) of other validators.
// * execution votes (AppVotes) of other validators.
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
	appQC     persist.Persister[*apb.AppQC]
	blocks    *persist.BlockPersister
	commitQCs *persist.CommitQCPersister
}

func newNoOpPersisters() persisters {
	return persisters{
		appQC:     persist.NewNoOpPersister[*apb.AppQC](),
		blocks:    persist.NewNoOpBlockPersister(),
		commitQCs: persist.NewNoOpCommitQCPersister(),
	}
}

// innerFile is the A/B file prefix for avail inner state persistence.
const innerFile = "avail_inner"

// loadPersistedState loads persisted avail state from disk and creates persisters for ongoing writes.
func loadPersistedState(dir string) (*loadedAvailState, persist.Persister[*apb.AppQC], *persist.BlockPersister, *persist.CommitQCPersister, error) {
	persister, persistedProto, err := persist.NewPersister[*apb.AppQC](dir, innerFile)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("NewPersister %s: %w", innerFile, err)
	}

	var appQC utils.Option[*types.AppQC]
	if pb, ok := persistedProto.Get(); ok {
		qc, err := types.AppQCConv.Decode(pb)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("decode persisted AppQC: %w", err)
		}
		log.Info().
			Uint64("roadIndex", uint64(qc.Proposal().RoadIndex())).
			Uint64("globalNumber", uint64(qc.Proposal().GlobalNumber())).
			Msg("loaded persisted AppQC")
		appQC = utils.Some(qc)
	}

	bp, blocks, err := persist.NewBlockPersister(dir)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("NewBlockPersister: %w", err)
	}

	cp, commitQCs, err := persist.NewCommitQCPersister(dir)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("NewCommitQCPersister: %w", err)
	}

	return &loadedAvailState{appQC: appQC, commitQCs: commitQCs, blocks: blocks}, persister, bp, cp, nil
}

// NewState constructs a new availability state.
// stateDir is None when persistence is disabled (testing only); a no-op
// persist goroutine still runs to bump cursors without disk I/O.
func NewState(key types.SecretKey, data *data.State, stateDir utils.Option[string]) (*State, error) {
	var loaded utils.Option[*loadedAvailState]
	var pers persisters

	if dir, ok := stateDir.Get(); ok {
		l, appQC, blocks, commitQCs, err := loadPersistedState(dir)
		if err != nil {
			return nil, err
		}
		loaded = utils.Some(l)
		pers = persisters{appQC: appQC, blocks: blocks, commitQCs: commitQCs}
	} else {
		pers = newNoOpPersisters()
	}

	inner, err := newInner(data.Committee(), loaded)
	if err != nil {
		return nil, err
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
		if err := v.Msg().Proposal().Verify(qc); err != nil {
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
		updated, err := inner.prune(appQC, qc)
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
	if err := appQC.Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	if err := commitQC.Verify(s.data.Committee()); err != nil {
		return fmt.Errorf("commitQC.Verify(): %w", err)
	}
	if appQC.Proposal().RoadIndex() != commitQC.Proposal().Index() {
		return fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", appQC.Proposal().RoadIndex(), commitQC.Proposal().Index())
	}
	for inner, ctrl := range s.inner.Lock() {
		updated, err := inner.prune(appQC, commitQC)
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
				log.Error().
					Stringer("lane", h.Lane()).
					Uint64("block", uint64(h.BlockNumber())).
					Hex("got", h.ParentHash().Bytes()).
					Hex("want", prevHash.Bytes()).
					Msg("parent hash mismatch (producer equivocation)")
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
		return fmt.Errorf("block.Verify(): %w", err)
	}
	if err := vote.VerifySig(s.data.Committee()); err != nil {
		return fmt.Errorf("p.VerifySig(): %w", err)
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
	for _, lane := range s.data.Committee().Lanes().All() {
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
		if err := ctrl.WaitUntil(ctx, func() bool { return q.Len() < BlocksPerLane }); err != nil {
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
			for _, lane := range c.Lanes().All() {
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
		if err := ctrl.WaitUntil(ctx, func() bool { return q.Len() < BlocksPerLane }); err != nil {
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
					for _, lane := range c.Lanes().All() {
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
// Write order: blocks → new CommitQCs → AppQC → delete old CommitQCs.
// AppQC is written before deleting old CommitQCs so that a crash never
// leaves the on-disk AppQC pointing at a deleted CommitQC.
//
// Blocks are persisted one at a time with inner.nextBlockToPersist
// updated after each write, so vote latency equals single-block write
// time regardless of batch size.
func (s *State) runPersist(ctx context.Context, pers persisters) error {
	var lastPersistedAppQC utils.Option[*types.AppQC]
	for {
		batch, err := s.collectPersistBatch(ctx)
		if err != nil {
			return err
		}

		for _, proposal := range batch.blocks {
			h := proposal.Msg().Block().Header()
			if err := pers.blocks.PersistBlock(proposal); err != nil {
				return fmt.Errorf("persist block %s/%d: %w", h.Lane(), h.BlockNumber(), err)
			}
			s.markBlockPersisted(h.Lane(), h.BlockNumber()+1)
		}
		if err := pers.blocks.DeleteBefore(batch.laneFirsts); err != nil {
			return fmt.Errorf("block deleteBefore: %w", err)
		}

		commitQCCur := batch.commitQCCur
		for _, qc := range batch.commitQCs {
			if err := pers.commitQCs.PersistCommitQC(qc); err != nil {
				return fmt.Errorf("persist commitqc %d: %w", qc.Index(), err)
			}
			commitQCCur = qc.Index() + 1
		}

		// Persist AppQC after new CommitQCs (so the matching
		// CommitQC is on disk) but before deleting old ones (so a
		// crash never leaves the on-disk AppQC pointing at a
		// deleted CommitQC).
		// TODO: use a single WAL for AppQC and CommitQCs to make
		// this atomic rather than relying on write order.
		if batch.appQC != lastPersistedAppQC {
			if appQC, ok := batch.appQC.Get(); ok {
				if err := pers.appQC.Persist(types.AppQCConv.Encode(appQC)); err != nil {
					return fmt.Errorf("persist AppQC: %w", err)
				}
			}
			lastPersistedAppQC = batch.appQC
		}

		if len(batch.commitQCs) > 0 {
			if err := pers.commitQCs.DeleteBefore(batch.commitQCFirst); err != nil {
				return fmt.Errorf("commitqc deleteBefore: %w", err)
			}
			s.markCommitQCsPersisted(commitQCCur, utils.Some(batch.commitQCs[len(batch.commitQCs)-1]))
		}
	}
}

// persistBatch holds the data collected under lock for one persist iteration.
type persistBatch struct {
	blocks        []*types.Signed[*types.LaneProposal]
	commitQCs     []*types.CommitQC
	appQC         utils.Option[*types.AppQC]
	laneFirsts    map[types.LaneID]types.BlockNumber
	commitQCFirst types.RoadIndex
	commitQCCur   types.RoadIndex // snapshot of nextCommitQCToPersist (clamped)
}

// markBlockPersisted advances the per-lane block persistence cursor.
// Called after each individual block write so that RecvBatch (and therefore
// voting) unblocks with single-block latency regardless of batch size.
func (s *State) markBlockPersisted(lane types.LaneID, next types.BlockNumber) {
	for inner, ctrl := range s.inner.Lock() {
		inner.nextBlockToPersist[lane] = next
		ctrl.Updated()
	}
}

// markCommitQCsPersisted advances the CommitQC persistence cursor and
// publishes the latest persisted CommitQC (gating consensus from advancing).
func (s *State) markCommitQCsPersisted(commitQCCur types.RoadIndex, commitQC utils.Option[*types.CommitQC]) {
	for inner, ctrl := range s.inner.Lock() {
		inner.nextCommitQCToPersist = commitQCCur
		if qc, ok := commitQC.Get(); ok {
			inner.latestCommitQC.Store(utils.Some(qc))
		}
		ctrl.Updated()
	}
}

// collectPersistBatch waits for new blocks or commitQCs and collects them under lock.
func (s *State) collectPersistBatch(ctx context.Context) (persistBatch, error) {
	var b persistBatch
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			for lane, q := range inner.blocks {
				if inner.nextBlockToPersist[lane] < q.next {
					return true
				}
			}
			return inner.nextCommitQCToPersist < inner.commitQCs.next
		}); err != nil {
			return b, err
		}
		b.laneFirsts = make(map[types.LaneID]types.BlockNumber, len(inner.blocks))
		for lane, q := range inner.blocks {
			// Clamp cursor: prune may have deleted entries below q.first
			// between iterations while the lock was not held.
			start := max(inner.nextBlockToPersist[lane], q.first)
			for n := start; n < q.next; n++ {
				b.blocks = append(b.blocks, q.q[n])
			}
			b.laneFirsts[lane] = q.first
		}
		commitQCCur := max(inner.nextCommitQCToPersist, inner.commitQCs.first)
		b.commitQCFirst = inner.commitQCs.first
		for n := commitQCCur; n < inner.commitQCs.next; n++ {
			b.commitQCs = append(b.commitQCs, inner.commitQCs.q[n])
		}
		b.commitQCCur = commitQCCur
		b.appQC = inner.latestAppQC
	}
	return b, nil
}
