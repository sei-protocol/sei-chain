package avail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

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

	// startupWALPrune, if set, is the prune-anchor CommitQC used once at the
	// start of runPersist to truncate WAL entries filtered out of memory at load.
	startupWALPrune utils.Option[*types.CommitQC]
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

	// DuoAt(CommitQC tipcut) happens inside newInner. Seeding is
	// data.SetupInitialDuo; missing epoch hard-fails.
	// Tip order: consensus.NewState requires avail ≥ consensus; avail/consensus
	// may lag data and catch up in Run.
	commitTip := types.RoadIndex(0)
	startupWALPrune := utils.None[*types.CommitQC]()
	if ls, ok := loaded.Get(); ok {
		commitTip = ls.nextCommitQC()
		if anchor, ok := ls.pruneAnchor.Get(); ok {
			// Disk truncate of filtered-out WAL entries runs once in runPersist.
			startupWALPrune = utils.Some(anchor.CommitQC)
		}
	}
	inner, err := newInner(data.Registry(), commitTip, loaded)
	if err != nil {
		return nil, err
	}

	return &State{
		key:             key,
		data:            data,
		inner:           utils.NewWatch(inner),
		epochDuo:        inner.epochDuo.Subscribe(),
		persisters:      pers,
		startupWALPrune: startupWALPrune,
	}, nil
}

func (s *State) FirstCommitQC() types.RoadIndex {
	for inner := range s.inner.Lock() {
		return inner.commitQCs.first
	}
	panic("unreachable")
}

// NextCommitQC is the next CommitQC road after restore/admit (commitQCs.next).
func (s *State) NextCommitQC() types.RoadIndex {
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

func logStaleRoad(what string, roadIdx types.RoadIndex, duo types.EpochDuo) {
	// Debug: Info is too chatty at epoch boundaries (many peers a road behind).
	logger.Debug("dropping stale "+what+": road behind window",
		slog.Uint64("road", uint64(roadIdx)), "duo", duo.String())
}

// waitUntilRoad waits until status is not RoadFuture; RoadStale → ErrPruned
// with the deciding duo still returned for logging.
func (s *State) waitUntilRoad(
	ctx context.Context,
	roadIdx types.RoadIndex,
	status func(types.EpochDuo) types.RoadStatus,
) (types.EpochDuo, error) {
	duo, err := s.epochDuo.Wait(ctx, func(duo types.EpochDuo) bool {
		return status(duo) != types.RoadFuture
	})
	if err != nil {
		return types.EpochDuo{}, err
	}
	switch status(duo) {
	case types.RoadReady:
		return duo, nil
	case types.RoadStale:
		return duo, types.ErrPruned
	default:
		// Wait predicate forbids Future; hitting it is an internal bug.
		panic(fmt.Sprintf("waitUntilRoad: unexpected RoadFuture for road %d after Wait", roadIdx))
	}
}

// waitForEpoch waits until roadIdx is in Current (CommitQC tip).
func (s *State) waitForEpoch(ctx context.Context, roadIdx types.RoadIndex) (types.EpochDuo, error) {
	return s.waitUntilRoad(ctx, roadIdx, func(d types.EpochDuo) types.RoadStatus {
		return d.RoadStatusCurrent(roadIdx)
	})
}

// waitForEpochDuo waits until roadIdx is in Prev|Current (AppVote/AppQC).
func (s *State) waitForEpochDuo(ctx context.Context, roadIdx types.RoadIndex) (types.EpochDuo, error) {
	return s.waitUntilRoad(ctx, roadIdx, func(d types.EpochDuo) types.RoadStatus {
		return d.RoadStatusDuo(roadIdx)
	})
}

// waitForEpochOrDropStale is PushCommitQC admit: wait for Current, soft-drop if stale.
func (s *State) waitForEpochOrDropStale(
	ctx context.Context, what string, roadIdx types.RoadIndex,
) (utils.Option[types.EpochDuo], error) {
	return s.waitRoadOrDropStale(ctx, what, roadIdx, s.waitForEpoch)
}

// waitForEpochDuoOrDropStale is PushAppVote/PushAppQC admit: wait for Prev|Current, soft-drop if stale.
func (s *State) waitForEpochDuoOrDropStale(
	ctx context.Context, what string, roadIdx types.RoadIndex,
) (utils.Option[types.EpochDuo], error) {
	return s.waitRoadOrDropStale(ctx, what, roadIdx, s.waitForEpochDuo)
}

func (s *State) waitRoadOrDropStale(
	ctx context.Context,
	what string,
	roadIdx types.RoadIndex,
	wait func(context.Context, types.RoadIndex) (types.EpochDuo, error),
) (utils.Option[types.EpochDuo], error) {
	duo, err := wait(ctx, roadIdx)
	if err != nil {
		if errors.Is(err, types.ErrPruned) {
			logStaleRoad(what, roadIdx, duo)
			return utils.None[types.EpochDuo](), nil
		}
		return utils.None[types.EpochDuo](), err
	}
	return utils.Some(duo), nil
}

// waitForAppQC blocks until latest AppQC is from epochIdx or later.
//
// Called from runAdvanceEpoch when sealing epoch N (Current's last CommitQC
// already admitted). Epoch 0 is not special-cased: seal is {∅,0}→{0,1} (no Prev
// drop), but the leash still runs so leaving 0 always writes an AppQC anchor for
// newInner (Current>0 requires one). Do not reintroduce an epochIdx==0 skip:
// BlocksPerLane only caps local production; peers can PushCommitQC LastRoad(0)
// (then mid-epoch-1 QCs) with no local AppQC, which would otherwise restart
// without an anchor.
func (s *State) waitForAppQC(ctx context.Context, epochIdx types.EpochIndex) error {
	for inner, ctrl := range s.inner.Lock() {
		ready := func() bool {
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
		logger.Warn("waiting for AppQC before advancing epoch", attrs...)
		return ctrl.WaitUntil(ctx, ready)
	}
	panic("unreachable")
}

// LastAppQC returns the latest observed AppQC.
func (s *State) LastAppQC() utils.Option[*types.AppQC] {
	for inner := range s.inner.Lock() {
		return inner.latestAppQC
	}
	panic("unreachable")
}

// tipcutAppQC returns LastAppQC when its epoch is usable for a tipcut whose
// Current is want (want or want-1). Otherwise None.
func (s *State) tipcutAppQC(want *types.Epoch) utils.Option[*types.AppQC] {
	appQC, ok := s.LastAppQC().Get()
	if !ok || !want.AcceptsAppEpoch(appQC.Proposal().EpochIndex()) {
		return utils.None[*types.AppQC]()
	}
	return utils.Some(appQC)
}

// WaitForTipcutAppQC returns an AppQC usable for a tipcut whose Current is
// want (want or want-1). For epoch 0, returns tipcutAppQC immediately
// (may be None).
//
// For want>0, proposing must not fall back to a CommitQC App older than want-1.
// If commitQC already carries an in-window App, returns tipcutAppQC without
// waiting (None is fine: tipcut keeps that CommitQC App). Otherwise blocks
// until latestAppQC is in-window.
func (s *State) WaitForTipcutAppQC(
	ctx context.Context,
	want *types.Epoch,
	commitQC utils.Option[*types.CommitQC],
) (utils.Option[*types.AppQC], error) {
	if want.EpochIndex() == 0 {
		return s.tipcutAppQC(want), nil
	}
	if old, ok := types.AppOpt(types.ProposalOpt(commitQC)).Get(); ok && want.AcceptsAppEpoch(old.EpochIndex()) {
		return s.tipcutAppQC(want), nil
	}
	for inner, ctrl := range s.inner.Lock() {
		ready := func() bool {
			appQC, ok := inner.latestAppQC.Get()
			return ok && want.AcceptsAppEpoch(appQC.Proposal().EpochIndex())
		}
		if !ready() {
			logger.Warn("waiting for AppQC in EpochDuo before proposing",
				slog.Uint64("want_epoch", uint64(want.EpochIndex())))
			if err := ctrl.WaitUntil(ctx, ready); err != nil {
				return utils.None[*types.AppQC](), err
			}
		}
		appQC, ok := inner.latestAppQC.Get()
		if !ok || !want.AcceptsAppEpoch(appQC.Proposal().EpochIndex()) {
			return utils.None[*types.AppQC](), fmt.Errorf("WaitForTipcutAppQC: AppQC not in duo after wait")
		}
		return utils.Some(appQC), nil
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

// PushCommitQC admits qc for Current only (too early waits; stale drops).
// Epoch slide is async in runAdvanceEpoch (tip may sit at Current.Next while
// Current still N; N+1 CommitQCs park on waitForEpoch until the duo advances).
//
// Admit-then-verify is intentional backpressure for ahead-of-window QCs.
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	idx := qc.Proposal().Index()
	if idx > 0 {
		if err := s.waitForCommitQC(ctx, idx-1); err != nil {
			return err
		}
	}
	admitted, err := s.waitForEpochOrDropStale(ctx, "CommitQC", idx)
	if err != nil {
		return err
	}
	duo, ok := admitted.Get()
	if !ok {
		return nil
	}
	ep := duo.Current
	if err := qc.Verify(ep); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}

	for inner, ctrl := range s.inner.Lock() {
		if !inner.insertCommitQCAtTip(qc) {
			return nil
		}
		// latestCommitQC advances only after durable persist (or no-op persister).
		ctrl.Updated()
		return nil
	}
	return nil
}

// PushAppVote pushes an AppVote to the state.
// Same admit-then-verify as PushAppQC: far-future roads park until the duo
// and CommitQC tip catch up (one stream goroutine; does not block others).
func (s *State) PushAppVote(ctx context.Context, v *types.Signed[*types.AppVote]) error {
	idx := v.Msg().Proposal().RoadIndex()
	// A vote may arrive before its CommitQC advances the tip.
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return err
	}
	// Too-early roads (ahead of Prev|Current) backpressure; too-late are dropped.
	admitted, err := s.waitForEpochDuoOrDropStale(ctx, "AppVote", idx)
	if err != nil {
		return err
	}
	duo, ok := admitted.Get()
	if !ok {
		return nil
	}
	ep := utils.OrPanic1(duo.EpochForRoad(idx))
	if got, want := v.Msg().Proposal().EpochIndex(), ep.EpochIndex(); got != want {
		return fmt.Errorf("appVote epoch_index %d, want %d", got, want)
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

// PushAppQC requires a justifying CommitQC. Epoch slide is async in
// runAdvanceEpoch (same as PushCommitQC). Prune before insert so latestAppQC is
// visible before the advance task observes the new tip.
//
// Same admit-then-verify as PushCommitQC.
func (s *State) PushAppQC(ctx context.Context, appQC *types.AppQC, commitQC *types.CommitQC) error {
	// Check whether it is needed before verifying.
	for inner := range s.inner.Lock() {
		if types.NextOpt(inner.latestAppQC) > appQC.Proposal().RoadIndex() {
			return nil
		}
	}
	// Pair consistency only; ahead-of-window still waits in waitForEpochDuo.
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
	admitted, err := s.waitForEpochDuoOrDropStale(ctx, "AppQC", idx)
	if err != nil {
		return err
	}
	duo, ok := admitted.Get()
	if !ok {
		return nil
	}
	ep := utils.OrPanic1(duo.EpochForRoad(idx))
	if err := appQC.Verify(ep); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	if err := commitQC.Verify(ep); err != nil {
		return fmt.Errorf("commitQC.Verify(): %w", err)
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
		inner.insertCommitQCAtTip(commitQC)
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
	// Snapshot Current once for off-lock verify. Unlike PushVote (which parks
	// until Current accepts the signer), we do not wait for future committees —
	// lane proposals are not reweighted across epoch advances.
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

// PushVote parks until Current can accept the vote (signer weight + voted lane),
// verifies, then under lock waits for capacity and credits with the live duo
// (drop if Current advanced and signer left).
//
// Lane-vote streams are committee-only (giga RunServer/RunClient), so parking a
// future-epoch signer does not expose an unauthenticated DoS path. No p2p retry:
// without this wait, async epoch entry would drop the vote permanently.
func (s *State) PushVote(ctx context.Context, vote *types.Signed[*types.LaneVote]) error {
	h := vote.Msg().Header()
	// Future-epoch voters park (one stream goroutine) until Current includes them.
	var committee *types.Committee
	var verifiedEpoch types.EpochIndex
	for inner, ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			c := inner.epochDuo.Load().Current.Committee()
			return c.Weight(vote.Key()) > 0 && c.HasLane(h.Lane())
		}); err != nil {
			return err
		}
		duo := inner.epochDuo.Load()
		committee = duo.Current.Committee()
		verifiedEpoch = duo.Current.EpochIndex()
	}
	if err := vote.Msg().Verify(committee); err != nil {
		return fmt.Errorf("vote.Verify(): %w", err)
	}
	if err := vote.VerifySig(committee); err != nil {
		return fmt.Errorf("vote.Verify(): %w", err)
	}
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
			live.Current.Committee().Weight(vote.Key()) == 0 {
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
				if bv := q.q[n]; bv != nil {
					if set, ok := bv.byHash[want]; ok {
						want = set.header.ParentHash()
						headers[len(headers)-i-1] = set.header
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

// fullCommitQC returns the FullCommitQC for road n.
// ErrRoadBeforeWindow → ErrPruned (export may jump ahead). ErrRoadAfterWindow hard-fails.
func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
	qc, err := s.CommitQC(ctx, n)
	if err != nil {
		return nil, err
	}
	ep, err := s.epochDuo.Load().EpochForRoad(qc.Proposal().Index())
	if err != nil {
		if errors.Is(err, types.ErrRoadBeforeWindow) {
			return nil, types.ErrPruned
		}
		return nil, err
	}
	var commitHeaders []*types.BlockHeader
	for lane := range ep.Committee().Lanes().All() {
		headers, err := s.headers(ctx, qc.LaneRange(lane))
		if err != nil {
			return nil, err
		}
		commitHeaders = append(commitHeaders, headers...)
	}
	return types.NewFullCommitQC(qc, commitHeaders), nil
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
// (persist, epoch advance, and the FullCommitQC→data-state pusher). Inside
// runPersist, scope.Parallel spawns short-lived goroutines for concurrent
// per-lane block and commit-QC persistence. The persist package itself does
// not spawn goroutines.
func (s *State) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		scope.SpawnNamed("persist", func() error {
			return s.runPersist(ctx, s.persisters)
		})
		scope.SpawnNamed("advanceEpoch", func() error {
			return s.runAdvanceEpoch(ctx)
		})
		// Task inserting FullCommitQCs and local blocks to data state.
		// ErrPruned jumps n forward (AppQC/window prune during catch-up): skipped
		// roads need not be exported locally — peers can PushQC into data.
		scope.SpawnNamed("s.data.PushQC", func() error {
			for n := types.RoadIndex(0); ; n = max(n+1, s.FirstCommitQC()) {
				qc, err := s.fullCommitQC(ctx, n)
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
							// We don't need to check the blocks against the headers,
							// as bad blocks will be filtered out by PushQC anyway.
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

// runAdvanceEpoch is the sole post-construction writer of epochDuo. When
// commitQCs tip passes Current's last road, it waits for AppQC of Current and
// registry WaitForDuo, then advances. Push* must not slide the duo: tip may sit
// at Current.Next while Current is still N; N+1 CommitQCs park on waitForEpoch.
func (s *State) runAdvanceEpoch(ctx context.Context) error {
	for {
		duo := s.epochDuo.Load()
		epochIdx := duo.Current.EpochIndex()
		last := duo.Current.RoadRange().Next - 1

		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				return inner.commitQCs.next > last
			}); err != nil {
				return err
			}
		}

		if err := s.waitForAppQC(ctx, epochIdx); err != nil {
			return err
		}
		nextDuo, err := s.data.Registry().WaitForDuo(ctx, last+1)
		if err != nil {
			return err
		}

		for inner, ctrl := range s.inner.Lock() {
			live := inner.epochDuo.Load()
			if live.Current.EpochIndex() != epochIdx {
				break
			}
			if inner.commitQCs.next <= last {
				break
			}
			inner.advanceEpoch(nextDuo)
			ctrl.Updated()
		}
	}
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
	// Truncate WAL entries filtered out of memory at load (once).
	// TODO(lane-id): also prune Prev committee lanes on restart (same as
	// newInner Prev-lane seeding). Next Lane ID PR.
	if anchorQC, ok := s.startupWALPrune.Get(); ok {
		s.startupWALPrune = utils.None[*types.CommitQC]()
		for lane := range s.epochDuo.Load().Current.Committee().Lanes().All() {
			if err := pers.blocks.MaybePruneAndPersistLane(lane, utils.Some(anchorQC), nil, utils.None[func(*types.Signed[*types.LaneProposal])]()); err != nil {
				return fmt.Errorf("prune stale block WAL entries: %w", err)
			}
		}
		if err := pers.commitQCs.MaybePruneAndPersist(utils.Some(anchorQC), nil, utils.None[func(*types.CommitQC)]()); err != nil {
			return fmt.Errorf("prune stale commitQC WAL entries: %w", err)
		}
	}

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
