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

// TODO: add dynamic committee members via getOrInsertLane.
type inner struct {
	latestAppQC    utils.Option[*types.AppQC]
	latestCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]]
	epochDuo       utils.AtomicSend[types.EpochDuo] // Store under Lock; State holds Recv
	appVotes       *queue[types.GlobalBlockNumber, appVotes]
	commitQCs      *queue[types.RoadIndex, *types.CommitQC]
	lanes          map[types.LaneID]*laneState
}

type laneState struct {
	blocks *queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes  *queue[types.BlockNumber, *blockVotes]
	// nextBlockToPersist is reconstructed from persisted blocks on restart.
	//
	// TODO: consider giving this its own AtomicSend to avoid waking unrelated
	// inner waiters (PushVote, PushCommitQC, etc.) on markBlockPersisted calls.
	nextBlockToPersist types.BlockNumber
	// persistedBlockStart gates PushBlock/PushVote capacity (start+BlocksPerLane).
	// Set from the prune-anchor LaneRange on load / after durable anchor write.
	// TODO: revisit whether in-mem tipcut alone is enough (side note for another PR).
	persistedBlockStart types.BlockNumber
}

func newLaneState() *laneState {
	return &laneState{
		blocks: newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]](),
		votes:  newQueue[types.BlockNumber, *blockVotes](),
	}
}

func (i *inner) getOrInsertLane(lane types.LaneID) *laneState {
	if ls, ok := i.lanes[lane]; ok {
		return ls
	}
	ls := newLaneState()
	i.lanes[lane] = ls
	return ls
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
	// TODO(lane-id): also seed Prev lanes before prune so restart applies the
	// anchor watermark to them (today only Current is pre-created; Prev lanes
	// appear later via WAL getOrInsertLane and miss prune). Next Lane ID PR.
	for lane := range startEpochDuo.Current.Committee().Lanes().All() {
		lanes[lane] = newLaneState()
	}

	i := &inner{
		latestAppQC:    utils.None[*types.AppQC](),
		latestCommitQC: utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		epochDuo:       utils.NewAtomicSend(startEpochDuo),
		appVotes:       newQueue[types.GlobalBlockNumber, appVotes](),
		commitQCs:      newQueue[types.RoadIndex, *types.CommitQC](),
		lanes:          lanes,
	}
	l, ok := loaded.Get()
	if !ok {
		if startEpochDuo.Current.EpochIndex() > 0 {
			return nil, fmt.Errorf("prune anchor required for epoch %d", startEpochDuo.Current.EpochIndex())
		}
		i.appVotes.prune(registry.FirstBlock())
		return i, nil
	}

	// Apply the persisted prune anchor first: prune() advances watermarks on
	// all queues (commitQCs, blocks, votes). Tipcut CommitQC insert is
	// explicit below — prune never silently pushBacks.
	// prune also sets appVotes.first from the anchor CommitQC.
	if anchor, ok := l.pruneAnchor.Get(); ok {
		logger.Info("loaded persisted prune anchor",
			slog.Uint64("roadIndex", uint64(anchor.AppQC.Proposal().RoadIndex())),
			slog.Uint64("globalNumber", uint64(anchor.AppQC.Proposal().GlobalNumber())),
		)
		if err := verifyCommitQCInDuo(startEpochDuo, anchor.CommitQC); err != nil {
			return nil, fmt.Errorf("load prune-anchor CommitQC: %w", err)
		}
		if _, err := i.prune(anchor.AppQC, anchor.CommitQC); err != nil {
			return nil, fmt.Errorf("prune: %w", err)
		}
		// prune advances next to idx; pushBack the justifying tipcut QC.
		if i.commitQCs.next == anchor.CommitQC.Proposal().Index() {
			i.commitQCs.pushBack(anchor.CommitQC)
			metrics.ObserveCommitQC(anchor.CommitQC)
		}
		for lane, ls := range i.lanes {
			ls.persistedBlockStart = anchor.CommitQC.LaneRange(lane).First()
		}
	} else if startEpochDuo.Current.EpochIndex() == 0 {
		// No anchor: floor appVotes at genesis (registry), not tip Current —
		// live advanceEpoch also leaves appVotes at the genesis floor.
		i.appVotes.prune(registry.FirstBlock())
	} else {
		return nil, fmt.Errorf("prune anchor required for epoch %d", startEpochDuo.Current.EpochIndex())
	}

	// Restore CommitQCs above commitQCs.next. Epoch must already be seeded.
	for _, lqc := range l.commitQCs {
		if lqc.Index < i.commitQCs.next {
			continue
		}
		if lqc.Index != i.commitQCs.next {
			return nil, fmt.Errorf("non-contiguous persisted commitQCs: expected %d, got %d", i.commitQCs.next, lqc.Index)
		}
		if err := verifyCommitQCInDuo(startEpochDuo, lqc.QC); err != nil {
			return nil, fmt.Errorf("load CommitQC %d: %w", lqc.Index, err)
		}
		i.commitQCs.pushBack(lqc.QC)
	}
	if i.commitQCs.next > i.commitQCs.first {
		i.latestCommitQC.Store(utils.Some(i.commitQCs.q[i.commitQCs.next-1]))
	}

	// Restore blocks; create queues for any WAL lane (including outside Current).
	// Old lanes are retained until lane-expiry (TODO).
	for lane, bs := range l.blocks {
		if len(bs) == 0 {
			continue
		}
		ls := i.getOrInsertLane(lane)
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
			ls.nextBlockToPersist = q.next
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

func (i *inner) laneQC(lane types.LaneID, n types.BlockNumber) utils.Option[*types.LaneQC] {
	bv, ok := i.lanes[lane].votes.q[n]
	if !ok {
		return utils.None[*types.LaneQC]()
	}
	return bv.laneQC()
}

// advanceEpoch installs nextDuo at a boundary. Caller must ensure nextDuo is
// the next epoch after Current and that seal leashes (waitForAppQC, registry
// WaitForDuo) are already satisfied. Adds Current lanes; does not delete old
// lanes (TODO(lane-expiry)).
func (i *inner) advanceEpoch(nextDuo types.EpochDuo) {
	current := nextDuo.Current
	for lane := range current.Committee().Lanes().All() {
		i.getOrInsertLane(lane)
	}
	for _, ls := range i.lanes {
		for n := ls.votes.first; n < ls.votes.next; n++ {
			ls.votes.q[n].reweight(current)
		}
	}
	i.epochDuo.Store(nextDuo)
}

// insertCommitQCAtTip inserts qc at commitQCs.next. If nextDuo is set and idx
// still closes live Current, advances the duo first (same order as PushCommitQC /
// PushAppQC). Returns false if idx is not the tip (race / already applied).
func (i *inner) insertCommitQCAtTip(qc *types.CommitQC, nextDuo utils.Option[types.EpochDuo]) bool {
	idx := qc.Proposal().Index()
	if idx != i.commitQCs.next {
		return false
	}
	if nd, ok := nextDuo.Get(); ok && i.epochDuo.Load().Current.RoadRange().Has(idx) {
		i.advanceEpoch(nd)
	}
	i.commitQCs.pushBack(qc)
	metrics.ObserveCommitQC(qc)
	return true
}

// prune advances watermarks for a new AppQC/CommitQC pair (commitQCs/appVotes/
// lane queues). It does not insert CommitQCs — callers that tipcut-catch-up
// must pushBack after prune when next==idx.
// Returns true if pruning occurred, false if the QC was stale.
func (i *inner) prune(appQC *types.AppQC, commitQC *types.CommitQC) (bool, error) {
	idx := appQC.Proposal().RoadIndex()
	if idx != commitQC.Proposal().Index() {
		return false, fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", idx, commitQC.Proposal().Index())
	}
	if idx < types.NextOpt(i.latestAppQC) {
		return false, nil
	}
	i.latestAppQC = utils.Some(appQC)
	metrics.ObserveAppQC(appQC)
	i.commitQCs.prune(idx)
	i.appVotes.prune(commitQC.GlobalRange().First)
	for lane, ls := range i.lanes {
		lr := commitQC.LaneRange(lane)
		ls.votes.prune(lr.First())
		ls.blocks.prune(lr.First())
		if ls.nextBlockToPersist < lr.First() {
			ls.nextBlockToPersist = lr.First()
		}
	}
	return true, nil
}
