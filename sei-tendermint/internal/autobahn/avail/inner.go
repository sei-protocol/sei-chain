package avail

import (
	"fmt"
	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
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
	epoch   utils.AtomicSend[*types.Epoch]
	app     appProgress
	commits commitProgress
	lanes   laneCollection
}

// loadedAvailState holds data loaded from disk on restart.
// appTip is the decoded App tip watermark (if any); Epoch is filled in newInner.
// commitQCs and blocks are pre-filtered: stale entries below the tip have
// already been removed by loadPersistedState.
// commitQCs are sorted by road index; blocks are sorted by number per lane.
// newInner requires both to be contiguous and returns an error on gaps.
type loadedAvailState struct {
	appTip    utils.Option[*AppTip]
	commitQCs []persist.LoadedCommitQC
	blocks    map[types.LaneID][]persist.LoadedBlock
}

// nextCommitQC is the index of the next CommitQC to be inserted after restore:
// one past the last loaded CommitQC, floored by the App tip when the WAL lags.
func (ls *loadedAvailState) nextCommitQC() types.RoadIndex {
	tip := types.RoadIndex(0)
	if n := len(ls.commitQCs); n > 0 {
		tip = ls.commitQCs[n-1].Index + 1
	}
	if appTip, ok := ls.appTip.Get(); ok {
		tip = max(tip, appTip.CommitQC.Proposal().Index()+1)
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
		epoch: utils.NewAtomicSend(startEpochDuo.Current),
		app: appProgress{
			tip:   utils.None[*AppTip](),
			votes: newQueue[types.GlobalBlockNumber, appVotes](),
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
			return nil, fmt.Errorf("app tip required for epoch %d", startEpochDuo.Current.EpochIndex())
		}
		i.app.votes.prune(registry.FirstBlock())
		return i, nil
	}

	// Apply the persisted App tip first. It advances all queue boundaries,
	// retains the tip CommitQC, and sets app.votes.first from that CommitQC.
	if tip, ok := l.appTip.Get(); ok {
		logger.Info("loaded persisted app tip",
			slog.Uint64("roadIndex", uint64(tip.AppQC.Proposal().RoadIndex())),
			slog.Uint64("globalNumber", uint64(tip.AppQC.Proposal().GlobalNumber())),
		)
		if err := verifyCommitQCInDuo(startEpochDuo, tip.CommitQC); err != nil {
			return nil, fmt.Errorf("load app-tip CommitQC: %w", err)
		}
		// Persisted proto does not yet carry Epoch; recover from the restore duo
		// so avail stays self-contained after load.
		if tip.Epoch == nil {
			ep, err := startEpochDuo.ByRoad(tip.CommitQC.Proposal().Index())
			if err != nil {
				return nil, fmt.Errorf("load app-tip Epoch: %w", err)
			}
			tip.Epoch = ep
		}
		if _, err := i.pushAppTip(tip); err != nil {
			return nil, fmt.Errorf("push app tip: %w", err)
		}
		for lane, ls := range i.lanes.byID {
			ls.durable.persistedBlockFirst = tip.CommitQC.LaneRange(lane).First()
		}
	} else if startEpochDuo.Current.EpochIndex() == 0 {
		// No tip: floor app votes at genesis (registry), not tip Current —
		// live advanceEpoch also leaves app.votes at the genesis floor.
		i.app.votes.prune(registry.FirstBlock())
	} else {
		return nil, fmt.Errorf("app tip required for epoch %d", startEpochDuo.Current.EpochIndex())
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
	ep, err := duo.ByRoad(qc.Proposal().Index())
	if err != nil {
		return fmt.Errorf("epoch lookup: %w", err)
	}
	return qc.Verify(ep)
}

// advanceEpoch installs Current. Sole post-construction writer is
// runAdvanceEpoch (after commit tip + seal leashes).
func (i *inner) advanceEpoch(epoch *types.Epoch) bool {
	if i.epoch.Load().EpochIndex() >= epoch.EpochIndex() {
		return false
	}
	i.lanes.onAdvance(epoch)
	i.epoch.Store(epoch)
	return true
}

// pushAppTip validates tip pair consistency, then asks each progress owner to
// apply its App-tip watermark. Returns true when the tip advanced, or false
// when it was stale.
func (i *inner) pushAppTip(tip *AppTip) (bool, error) {
	if tip.Epoch == nil {
		return false, fmt.Errorf("app tip missing Epoch")
	}
	idx := tip.AppQC.Proposal().RoadIndex()
	if idx != tip.CommitQC.Proposal().Index() {
		return false, fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", idx, tip.CommitQC.Proposal().Index())
	}
	if got, want := tip.Epoch.EpochIndex(), tip.AppQC.Proposal().EpochIndex(); got != want {
		return false, fmt.Errorf("app tip epoch %d != appQC epoch %d", got, want)
	}
	if !i.app.setTip(tip) {
		return false, nil
	}
	i.commits.applyJustifying(tip.CommitQC)
	i.lanes.pruneTo(tip.CommitQC)
	return true, nil
}
