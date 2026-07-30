package avail

import (
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// TODO: when dynamic committee changes are supported, newly joined members
// must be added via lanes.getOrInsert (blocks, votes, and durable cursors live
// on laneState). Currently lanes are initialized once in newInner from the
// start EpochDuo. BlockPersister creates lane WALs lazily inside
// MaybePruneAndPersistLane, but the new member must also appear in
// inner.lanes before the next persist cycle.
type inner struct {
	epoch   epochProgress
	app     appProgress
	commits commitProgress
	lanes   laneCollection
}

// loadedAvailState holds data loaded from disk on restart.
// pruneAnchor is the decoded prune anchor (if any).
// commitQCs and blocks are pre-filtered: stale entries below the
// anchor have already been removed by loadPersistedState.
// commitQCs are sorted by road index; blocks are sorted by number per lane.
// newInner requires both to be contiguous and returns an error on gaps.
type loadedAvailState struct {
	pruneAnchor utils.Option[*PruneAnchor]
	commitQCs   []persist.LoadedCommitQC
	blocks      map[types.LaneID][]persist.LoadedBlock
}

// nextCommitQC is the index of the next CommitQC to be inserted after restore:
// one past the last loaded CommitQC, floored by the prune-anchor tipcut when
// the WAL lags.
func (ls *loadedAvailState) nextCommitQC() types.RoadIndex {
	tip := types.RoadIndex(0)
	if n := len(ls.commitQCs); n > 0 {
		tip = ls.commitQCs[n-1].Index + 1
	}
	if anchor, ok := ls.pruneAnchor.Get(); ok {
		tip = max(tip, anchor.CommitQC.Proposal().Index()+1)
	}
	return tip
}

func newInner(registry *epoch.Registry, commitTip types.RoadIndex, loaded utils.Option[*loadedAvailState]) (*inner, error) {
	startEpochDuo, err := registry.DuoAt(commitTip)
	if err != nil {
		return nil, fmt.Errorf("DuoAt(%d): %w", commitTip, err)
	}
	lanes := map[types.LaneID]*laneState{}
	// TODO(lane-id): also seed Prev lanes before pruning so restart applies the
	// anchor boundary to them (today only Current is pre-created; Prev lanes
	// appear later via WAL getOrInsert and miss prune). Next Lane ID PR.
	for lane := range startEpochDuo.Current.Committee().Lanes().All() {
		lanes[lane] = newLaneState()
	}

	i := &inner{
		epoch: utils.NewAtomicSend(startEpochDuo),
		app: appProgress{
			latestAppQC: utils.None[*types.AppQC](),
			votes:       newQueue[types.GlobalBlockNumber, appVotes](),
		},
		commits: commitProgress{
			qcs:               newQueue[types.RoadIndex, *types.CommitQC](),
			persistedCommitQC: utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		},
		lanes: laneCollection{byID: lanes},
	}
	l, ok := loaded.Get()
	if !ok {
		if startEpochDuo.Current.EpochIndex() > 0 {
			return nil, fmt.Errorf("prune anchor required for epoch %d", startEpochDuo.Current.EpochIndex())
		}
		i.app.votes.prune(registry.FirstBlock())
		return i, nil
	}

	// Apply the persisted prune anchor first. It advances all queue boundaries,
	// retains the anchor CommitQC, and sets app.votes.first from that CommitQC.
	if anchor, ok := l.pruneAnchor.Get(); ok {
		logger.Info("loaded persisted prune anchor",
			slog.Uint64("roadIndex", uint64(anchor.AppQC.Proposal().RoadIndex())),
			slog.Uint64("globalNumber", uint64(anchor.AppQC.Proposal().GlobalNumber())),
		)
		if err := verifyCommitQCInDuo(startEpochDuo, anchor.CommitQC); err != nil {
			return nil, fmt.Errorf("load prune-anchor CommitQC: %w", err)
		}
		if _, err := i.pushPruneAnchor(anchor); err != nil {
			return nil, fmt.Errorf("push prune anchor: %w", err)
		}
		for lane, ls := range i.lanes.byID {
			ls.durable.persistedBlockFirst = anchor.CommitQC.LaneRange(lane).First()
		}
	} else if startEpochDuo.Current.EpochIndex() == 0 {
		// No anchor: floor app votes at genesis (registry), not tip Current —
		// live advanceEpoch also leaves app.votes at the genesis floor.
		i.app.votes.prune(registry.FirstBlock())
	} else {
		return nil, fmt.Errorf("prune anchor required for epoch %d", startEpochDuo.Current.EpochIndex())
	}

	// Restore persisted CommitQCs. The prune anchor may have already pushed the
	// anchor's CommitQC, so skip entries below commits.qcs.next.
	// Epoch must already be seeded.
	for _, lqc := range l.commitQCs {
		if lqc.Index < i.commits.qcs.next {
			continue
		}
		if lqc.Index != i.commits.qcs.next {
			return nil, fmt.Errorf("non-contiguous persisted commitQCs: expected %d, got %d", i.commits.qcs.next, lqc.Index)
		}
		if err := verifyCommitQCInDuo(startEpochDuo, lqc.QC); err != nil {
			return nil, fmt.Errorf("load CommitQC %d: %w", lqc.Index, err)
		}
		i.commits.qcs.pushBack(lqc.QC)
	}
	if i.commits.qcs.next > i.commits.qcs.first {
		i.commits.markPersisted(i.commits.qcs.q[i.commits.qcs.next-1])
	}

	// Restore blocks; create queues for any WAL lane (including outside Current).
	// Old lanes are retained until lane-expiry (TODO).
	for lane, bs := range l.blocks {
		if len(bs) == 0 {
			continue
		}
		ls := i.lanes.getOrInsert(lane)
		q := ls.blocks
		var lastHash types.BlockHeaderHash
		for j, b := range bs {
			if q.Len() >= BlocksPerLane {
				return nil, fmt.Errorf("lane %s: loaded %d blocks exceeds capacity %d", lane, len(bs), BlocksPerLane)
			}
			if b.Number != q.next {
				return nil, fmt.Errorf("lane %s: non-contiguous persisted blocks: expected %d, got %d", lane, q.next, b.Number)
			}
			if j > 0 {
				if got := b.Proposal.Msg().Block().Header().ParentHash(); got != lastHash {
					return nil, fmt.Errorf("lane %s: parent hash mismatch at block %d", lane, b.Number)
				}
			}
			lastHash = b.Proposal.Msg().Block().Header().Hash()
			q.pushBack(b.Proposal)
		}
		if q.next > q.first {
			ls.durable.persistedBlockNext = q.next
		}
	}

	return i, nil
}

// verifyCommitQCInDuo verifies qc against startEpochDuo (Prev|Current at restore).
func verifyCommitQCInDuo(duo types.EpochDuo, qc *types.CommitQC) error {
	ep, err := duo.EpochForRoad(qc.Proposal().Index())
	if err != nil {
		return fmt.Errorf("epoch lookup: %w", err)
	}
	if err := qc.Verify(ep); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	return nil
}

// advanceEpoch installs nextDuo at a boundary. Sole post-construction writer of
// epoch (via runAdvanceEpoch). Caller must ensure nextDuo is the next epoch
// after Current and that seal leashes (waitForAppQC, registry WaitForDuo) are
// already satisfied. Adds Current lanes; does not delete old lanes
// (TODO(lane-expiry)). Touches epoch + lane votes (reweight).
func (i *inner) advanceEpoch(nextDuo types.EpochDuo) {
	current := nextDuo.Current
	for lane := range current.Committee().Lanes().All() {
		i.lanes.getOrInsert(lane)
	}
	for _, ls := range i.lanes.byID {
		for n := ls.votes.first; n < ls.votes.next; n++ {
			ls.votes.q[n].reweight(current)
		}
	}
	i.epoch.Store(nextDuo)
}

// pushPruneAnchor advances queue boundaries for an AppQC and its matching
// CommitQC, retaining the CommitQC when it is the next tip. Returns true when
// the anchor advanced, or false when it was stale.
// Cross-progress orchestrator: touches app, commits, and lanes.
func (i *inner) pushPruneAnchor(anchor *PruneAnchor) (bool, error) {
	appQC := anchor.AppQC
	commitQC := anchor.CommitQC
	idx := appQC.Proposal().RoadIndex()
	if idx != commitQC.Proposal().Index() {
		return false, fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", idx, commitQC.Proposal().Index())
	}
	if idx < types.NextOpt(i.app.latestAppQC) {
		return false, nil
	}
	i.app.latestAppQC = utils.Some(appQC)
	metrics.ObserveAppQC(appQC)
	i.commits.qcs.prune(idx)
	i.commits.push(commitQC)
	i.app.votes.prune(commitQC.GlobalRange().First)
	for lane, ls := range i.lanes.byID {
		lr := commitQC.LaneRange(lane)
		ls.votes.prune(lr.First())
		ls.blocks.prune(lr.First())
		ls.durable.floorNext(lr.First())
	}
	return true, nil
}
