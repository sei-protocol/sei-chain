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
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
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
	key      types.SecretKey
	data     *data.State
	inner    utils.Watch[*inner]
	epochDuo utils.AtomicRecv[types.EpochDuo] // Load-only view of inner.epochDuo

	// persisters groups all disk persistence components.
	// Always initialized: real when stateDir is set, no-op otherwise.
	persisters persisters
}

func (s *State) PublicKey() types.PublicKey {
	return s.key.Public()
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

	// DuoAt(CommitQC tipcut). Seeding is data.SetupInitialDuo; missing epoch hard-fails.
	// Tip order: consensus.NewState (avail≥consensus), p2p.checkRestartTips (consensus≥data).
	commitTip := types.RoadIndex(0)
	if ls, ok := loaded.Get(); ok {
		commitTip = ls.nextCommitQC()
	}
	startDuo, err := data.Registry().DuoAt(commitTip)
	if err != nil {
		return nil, fmt.Errorf("DuoAt(%d): %w", commitTip, err)
	}
	inner, err := newInner(data.Registry(), startDuo, loaded)
	if err != nil {
		return nil, err
	}

	// Truncate WAL entries below the prune anchor that were filtered out by
	// loadPersistedState. Lanes come from the operating (tip) duo.
	if ls, ok := loaded.Get(); ok {
		if anchor, ok := ls.pruneAnchor.Get(); ok {
			for lane := range startDuo.Current.Committee().Lanes().All() {
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
		epochDuo:   inner.epochDuo.Subscribe(),
		persisters: pers,
	}, nil
}

func (s *State) FirstCommitQC() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.commitQCs.first
	}
	panic("unreachable")
}

// CommitTipCut is the next CommitQC road after restore/admit (commitQCs.next).
func (s *State) CommitTipCut() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.commitQCs.next
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

// waitRoadInWindow blocks while roadIdx is ahead of the admitted window
// (too early / backpressure). Returns Some(epoch) when lookup admits the road,
// or None when roadIdx has fallen behind windowFirst (too late / stale).
func (s *State) waitRoadInWindow(
	ctx context.Context,
	roadIdx types.RoadIndex,
	lookup func(types.EpochDuo) utils.Option[*types.Epoch],
	windowFirst func(types.EpochDuo) types.RoadIndex,
) (utils.Option[*types.Epoch], error) {
	duo, err := s.epochDuo.Wait(ctx, func(duo types.EpochDuo) bool {
		if lookup(duo).IsPresent() {
			return true
		}
		return roadIdx < windowFirst(duo)
	})
	if err != nil {
		return utils.None[*types.Epoch](), err
	}
	return lookup(duo), nil
}

// waitEpochForRoad: too early waits; behind window → None (stale).
func (s *State) waitEpochForRoad(ctx context.Context, roadIdx types.RoadIndex) (utils.Option[*types.Epoch], error) {
	return s.waitRoadInWindow(ctx, roadIdx,
		func(duo types.EpochDuo) utils.Option[*types.Epoch] { return duo.EpochOptForRoad(roadIdx) },
		types.EpochDuo.WindowFirst,
	)
}

// waitCurrentForRoad blocks until roadIdx is in Current (too early);
// None if behind Current (stale). Prev is not admitted — CommitQCs are Current-only.
func (s *State) waitCurrentForRoad(ctx context.Context, roadIdx types.RoadIndex) (utils.Option[*types.Epoch], error) {
	return s.waitRoadInWindow(ctx, roadIdx,
		func(duo types.EpochDuo) utils.Option[*types.Epoch] { return duo.CurrentForRoad(roadIdx) },
		func(duo types.EpochDuo) types.RoadIndex { return duo.Current.RoadRange().First },
	)
}

// admitRoadOrDrop waits for window admission. On stale it logs and returns
// (nil, nil) so Push* callers can drop without repeating the boilerplate.
// what is a short label for the log line (e.g. "CommitQC", "AppVote").
func (s *State) admitRoadOrDrop(
	ctx context.Context,
	roadIdx types.RoadIndex,
	what string,
	wait func(context.Context, types.RoadIndex) (utils.Option[*types.Epoch], error),
) (*types.Epoch, error) {
	epOpt, err := wait(ctx, roadIdx)
	if err != nil {
		return nil, err
	}
	ep, ok := epOpt.Get()
	if !ok {
		logger.Info("dropping stale "+what+": road behind window",
			slog.Uint64("road", uint64(roadIdx)), "duo", s.epochDuo.Load().String())
		return nil, nil
	}
	return ep, nil
}

// waitPruneLeash blocks until latest AppQC is from epochIdx or later.
// incoming, if present, counts before latestAppQC is updated.
func (s *State) waitPruneLeash(ctx context.Context, epochIdx types.EpochIndex, incoming utils.Option[*types.AppQC]) error {
	for inner, ctrl := range s.inner.Lock() {
		ready := func() bool {
			if c, ok := incoming.Get(); ok && c.Proposal().EpochIndex() >= epochIdx {
				return true
			}
			appQC, ok := inner.latestAppQC.Get()
			if !ok {
				return false
			}
			return appQC.Proposal().EpochIndex() >= epochIdx
		}
		if ready() {
			return nil
		}
		attrs := []any{slog.Uint64("want_epoch", uint64(epochIdx))}
		if appQC, ok := inner.latestAppQC.Get(); ok {
			attrs = append(attrs,
				slog.Uint64("latest_app_qc_road", uint64(appQC.Proposal().RoadIndex())),
				slog.Uint64("latest_app_qc_epoch", uint64(appQC.Proposal().EpochIndex())),
			)
		}
		logger.Warn("waiting for AppQC before accepting CommitQC from next epoch", attrs...)
		return ctrl.WaitUntil(ctx, ready)
	}
	panic("unreachable")
}

// waitCommitEpochLeashes enforces tip-interlock before sealing epoch N>0
// (closingEpoch = last road of N). Mid-N admits are not gated. Epoch 0: no-op.
// incoming: AppQC on PushAppQC (None for PushCommitQC). See Registry invariants.
func (s *State) waitCommitEpochLeashes(
	ctx context.Context,
	epochIdx types.EpochIndex,
	closingEpoch bool,
	incoming utils.Option[*types.AppQC],
) error {
	if epochIdx == 0 || !closingEpoch {
		return nil
	}
	// Sealing N drops Prev (N-1) and advances Current into N+1.
	// Prune leash: AppQC in N ⇒ N-1 fully pruned before it leaves the duo.
	if err := s.waitPruneLeash(ctx, epochIdx, incoming); err != nil {
		return err
	}
	// Execution leash: N+1 existing ⇒ execution finished N-1 (usually AdvanceIfNeeded).
	_, err := s.data.Registry().WaitForDuo(ctx, epoch.FirstRoad(epochIdx+1))
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
			return nil, types.ErrPruned
		}
		return inner.commitQCs.q[idx], nil
	}
	panic("unreachable")
}

// PushCommitQC admits qc for Current only (too early waits; stale drops),
// then tip-interlock leashes when sealing (waitCommitEpochLeashes).
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	idx := qc.Proposal().Index()
	if idx > 0 {
		if err := s.waitForCommitQC(ctx, idx-1); err != nil {
			return err
		}
	}
	ep, err := s.admitRoadOrDrop(ctx, idx, "CommitQC", s.waitCurrentForRoad)
	if err != nil || ep == nil {
		return err
	}
	if err := qc.Verify(ep); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	closing := idx+1 == ep.RoadRange().Next
	if err := s.waitCommitEpochLeashes(ctx, ep.EpochIndex(), closing, utils.None[*types.AppQC]()); err != nil {
		return err
	}

	// Boundary: switch to the next epoch on Current's last CommitQC.
	// Resolve next duo off-lock (WaitForDuo).
	var nextDuo *types.EpochDuo
	if idx+1 == ep.RoadRange().Next {
		nt, err := s.data.Registry().WaitForDuo(ctx, idx+1)
		if err != nil {
			return err
		}
		nextDuo = &nt
	}

	for inner, ctrl := range s.inner.Lock() {
		if idx != inner.commitQCs.next {
			return nil
		}
		if nextDuo != nil {
			inner.advanceEpoch(*nextDuo)
		}
		inner.commitQCs.pushBack(qc)
		metrics.ObserveCommitQC(qc)
		// latestCommitQC advances only after durable persist (or no-op persister).
		ctrl.Updated()
		return nil
	}
	return nil
}

// PushAppVote pushes an AppVote to the state.
func (s *State) PushAppVote(ctx context.Context, v *types.Signed[*types.AppVote]) error {
	idx := v.Msg().Proposal().RoadIndex()
	// A vote may arrive before its CommitQC advances the tip.
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return err
	}
	// Too-early roads (ahead of Prev|Current) backpressure; too-late are dropped.
	ep, err := s.admitRoadOrDrop(ctx, idx, "AppVote", s.waitEpochForRoad)
	if err != nil || ep == nil {
		return err
	}
	committee := ep.Committee()
	if err := v.VerifySig(committee); err != nil {
		return fmt.Errorf("v.VerifySig(): %w", err)
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
		appQC, ok := q.q[n].pushVote(committee, v)
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

// PushAppQC requires a justifying CommitQC; tipcut insert uses the same
// sealing leashes as PushCommitQC.
func (s *State) PushAppQC(ctx context.Context, appQC *types.AppQC, commitQC *types.CommitQC) error {
	// Check whether it is needed before verifying.
	for inner := range s.inner.Lock() {
		if types.NextOpt(inner.latestAppQC) > appQC.Proposal().RoadIndex() {
			return nil
		}
	}
	// Reject mismatched pairs before waiting on the commitQC road — a far-future
	// Index() would otherwise stall admitRoadOrDrop indefinitely.
	if appQC.Proposal().RoadIndex() != commitQC.Proposal().Index() {
		return fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", appQC.Proposal().RoadIndex(), commitQC.Proposal().Index())
	}
	if got, want := appQC.Proposal().EpochIndex(), commitQC.Proposal().EpochIndex(); got != want {
		return fmt.Errorf("appQC epoch_index %d != commitQC epoch_index %d", got, want)
	}
	if !commitQC.GlobalRange().Has(appQC.Proposal().GlobalNumber()) {
		return fmt.Errorf("appQC GlobalNumber not in commitQC range")
	}
	idx := commitQC.Proposal().Index()
	ep, err := s.admitRoadOrDrop(ctx, idx, "AppQC", s.waitEpochForRoad)
	if err != nil || ep == nil {
		return err
	}
	if err := appQC.Verify(ep.Committee()); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	if err := commitQC.Verify(ep); err != nil {
		return fmt.Errorf("commitQC.Verify(): %w", err)
	}
	// Same leashes as PushCommitQC: tipcut pushBack is a CommitQC insert path.
	// Pass this AppQC as incoming so a tipcut that first enters epoch N can close N.
	closing := idx+1 == ep.RoadRange().Next
	if err := s.waitCommitEpochLeashes(ctx, ep.EpochIndex(), closing, utils.Some(appQC)); err != nil {
		return err
	}
	// Tipcut insert of a boundary CommitQC must slide Current like PushCommitQC.
	var nextDuo *types.EpochDuo
	if idx+1 == ep.RoadRange().Next {
		nt, err := s.data.Registry().WaitForDuo(ctx, idx+1)
		if err != nil {
			return err
		}
		nextDuo = &nt
	}
	for inner, ctrl := range s.inner.Lock() {
		updated, err := inner.prune(appQC, commitQC)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
		// prune advances pointers first; only then can pushBack land at idx.
		if inner.commitQCs.next == idx {
			// Slide duo before insert when this tipcut closes Current (same
			// order as PushCommitQC). Skip if Current already moved on.
			if nextDuo != nil && inner.epochDuo.Load().Current.RoadRange().Has(idx) {
				inner.advanceEpoch(*nextDuo)
			}
			inner.commitQCs.pushBack(commitQC)
			metrics.ObserveCommitQC(commitQC)
		}
		ctrl.Updated()
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

// Block returns block n of the given lane.
// Waits until the block is available.
// Returns ErrPruned if the block has been already pruned.
func (s *State) Block(ctx context.Context, lane types.LaneID, n types.BlockNumber) (*types.Signed[*types.LaneProposal], error) {
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes[lane]
		if !ok {
			return nil, ErrBadLane
		}
		q := ls.blocks
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
	// Snapshot once so verify cannot race a boundary advanceEpoch (same as PushVote).
	duo := s.epochDuo.Load()
	c := duo.Current.Committee()
	if err := p.Msg().Verify(c); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	if err := p.VerifySig(c); err != nil {
		return fmt.Errorf("block.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes[h.Lane()]
		if !ok {
			return ErrBadLane
		}
		q := ls.blocks
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() <= min(q.next, ls.persistedBlockStart+BlocksPerLane-1)
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

// PushVote verifies off-lock against Current, then under lock credits with the
// live duo (drop if Current advanced and signer left). Does not wait for prior votes.
func (s *State) PushVote(ctx context.Context, vote *types.Signed[*types.LaneVote]) error {
	// Verify off-lock against a duo snapshot.
	duo := s.epochDuo.Load()
	c := duo.Current.Committee()
	if err := vote.Msg().Verify(c); err != nil {
		return fmt.Errorf("vote.Verify(): %w", err)
	}
	if err := vote.VerifySig(c); err != nil {
		return fmt.Errorf("vote.Verify(): %w", err)
	}
	verifiedEpoch := duo.Current.EpochIndex()
	h := vote.Msg().Header()
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes[h.Lane()]
		if !ok {
			return ErrBadLane
		}
		q := ls.votes
		if err := ctrl.WaitUntil(ctx, func() bool {
			return h.BlockNumber() < ls.persistedBlockStart+BlocksPerLane
		}); err != nil {
			return err
		}
		// WaitUntil may release the lock; re-check membership under live Current.
		live := inner.epochDuo.Load()
		if live.Current.EpochIndex() != verifiedEpoch &&
			!live.Current.Committee().HasReplica(vote.Key()) {
			return nil
		}
		if h.BlockNumber() < q.first {
			return nil
		}
		for q.next <= h.BlockNumber() {
			q.pushBack(newBlockVotes())
		}
		if q.q[h.BlockNumber()].pushVote(live.Current, vote).IsPresent() {
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
		ls, ok := inner.lanes[lr.Lane()]
		if !ok {
			return nil, types.ErrPruned
		}
		q := ls.votes
		for i := range headers {
			n := lr.Next() - types.BlockNumber(i) - 1 //nolint:gosec // i is bounded by len(headers) which is a small block range; no overflow risk
			for {
				// If pruned, then give up.
				if q.first > lr.First() {
					return nil, types.ErrPruned
				}
				if set, ok := q.q[n].byHash[want]; ok && set.header != nil {
					want = set.header.ParentHash()
					headers[len(headers)-i-1] = set.header
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

// fullCommitQC returns the FullCommitQC for road n and its signing epoch.
// ErrRoadBeforeWindow → ErrPruned (export may jump ahead). ErrRoadAfterWindow hard-fails.
func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, *types.Epoch, error) {
	qc, err := s.CommitQC(ctx, n)
	if err != nil {
		return nil, nil, err
	}
	ep, err := s.epochDuo.Load().EpochForRoad(qc.Proposal().Index())
	if err != nil {
		if errors.Is(err, types.ErrRoadBeforeWindow) {
			return nil, nil, types.ErrPruned
		}
		return nil, nil, err
	}
	var commitHeaders []*types.BlockHeader
	for lane := range ep.Committee().Lanes().All() {
		headers, err := s.headers(ctx, qc.LaneRange(lane))
		if err != nil {
			return nil, nil, err
		}
		commitHeaders = append(commitHeaders, headers...)
	}
	return types.NewFullCommitQC(qc, commitHeaders), ep, nil
}

// WaitForLocalCapacity waits until the lane owned by this node has capacity for toProduce block.
func (s *State) WaitForLocalCapacity(ctx context.Context, toProduce types.BlockNumber) error {
	lane := s.key.Public()
	for inner, ctrl := range s.inner.Lock() {
		ls, ok := inner.lanes[lane]
		if !ok {
			return ErrBadLane
		}
		if err := ctrl.WaitUntil(ctx, func() bool {
			return toProduce < ls.persistedBlockStart+BlocksPerLane
		}); err != nil {
			return err
		}
	}
	return nil
}

// WaitForLaneQCs waits until there is at least 1 LaneQC in the Current epoch
// with a block not finalized by prev. Returns the Current epoch alongside the
// QCs so the caller can verify it matches the epoch it intends to propose in.
func (s *State) WaitForLaneQCs(
	ctx context.Context, prev utils.Option[*types.CommitQC],
) (map[types.LaneID]*types.LaneQC, *types.Epoch, error) {
	for inner, ctrl := range s.inner.Lock() {
		laneQCs := map[types.LaneID]*types.LaneQC{}
		for {
			ep := inner.epochDuo.Load().Current
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
				return laneQCs, ep, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, nil, err
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
		ls, ok := inner.lanes[lane]
		if !ok {
			return nil, ErrBadLane
		}
		q := ls.blocks
		if n >= ls.persistedBlockStart+BlocksPerLane {
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
			for n := types.RoadIndex(0); ; n = max(n+1, s.FirstCommitQC()) {
				qc, _, err := s.fullCommitQC(ctx, n)
				if err != nil {
					if errors.Is(err, types.ErrPruned) {
						continue
					}
					return err
				}

				// Collect locally available blocks for the QC's headers.
				var blocks []*types.Block
				for inner := range s.inner.Lock() {
					for _, h := range qc.Headers() {
						ls, ok := inner.lanes[h.Lane()]
						if !ok {
							continue
						}
						if b, ok := ls.blocks.q[h.BlockNumber()]; ok {
							// No need to check against headers; PushQC filters mismatches.
							blocks = append(blocks, b.Msg().Block())
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

		blocksByLane := make(map[types.LaneID][]*types.Signed[*types.LaneProposal])
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
			// Collect lanes: any lane with blocks in this batch, plus all lanes
			// in the anchor epoch (for WAL pruning).
			// TODO: when epoch transitions land, also union in lanes from all
			// epochs that appear in batch.commitQCs so new-epoch lanes are
			// never skipped in a cross-epoch batch.
			batchLanes := map[types.LaneID]struct{}{}
			for lane := range blocksByLane {
				batchLanes[lane] = struct{}{}
			}
			if anchor, ok := anchorQC.Get(); ok {
				// Resolve via epochDuo, not the registry: the prune anchor is live
				// Availability metadata and must remain inside the Prev|Current
				// operating window (interlock: an epoch leaves that window only
				// after its AppQC floor has made it obsolete). The registry is
				// independent of Availability pruning (restart + admission /
				// execution leash), not the live window.
				ep, err := s.epochDuo.Load().EpochForRoad(anchor.Proposal().Index())
				if err != nil {
					return fmt.Errorf("EpochForRoad(%d): %w", anchor.Proposal().Index(), err)
				}
				for lane := range ep.Committee().Lanes().All() {
					batchLanes[lane] = struct{}{}
				}
			}
			for lane := range batchLanes {
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
		for lane, ls := range inner.lanes {
			start := commitQC.LaneRange(lane).First()
			if start > ls.persistedBlockStart {
				ls.persistedBlockStart = start
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
		ls, ok := inner.lanes[lane]
		if !ok {
			return
		}
		ls.nextBlockToPersist = next
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
			for _, ls := range inner.lanes {
				if ls.nextBlockToPersist < ls.blocks.next {
					return true
				}
			}
			return commitQCNext < inner.commitQCs.next
		}); err != nil {
			return b, err
		}
		for _, ls := range inner.lanes {
			start := max(ls.nextBlockToPersist, ls.blocks.first)
			for n := start; n < ls.blocks.next; n++ {
				b.blocks = append(b.blocks, ls.blocks.q[n])
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
