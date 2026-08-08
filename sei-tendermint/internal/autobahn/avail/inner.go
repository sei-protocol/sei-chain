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

// Lane maps: joiners are added at ApplyEpoch; leavers stay in maps until the
// tipcut (first retained CommitQC) no longer lists them, then remove + DeleteLane.
// On restart, persisted leave-lane WALs are re-attached into memory (extras are
// safe — tryPruneLeaveLanes removes them once tipcut omits them).
type inner struct {
	epoch          *types.Epoch
	latestAppQC    utils.Option[*types.AppQC]
	latestCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]]
	appVotes       *queue[types.GlobalBlockNumber, appVotes]
	commitQCs      *queue[types.RoadIndex, *types.CommitQC]
	blocks         map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes          map[types.LaneID]*queue[types.BlockNumber, blockVotes]
	// nextBlockToPersist tracks per-lane how far block persistence has progressed.
	// RecvBatch only yields blocks below this cursor for voting.
	// Always initialized (even when persistence is disabled — the no-op persist
	// goroutine bumps it immediately). Not persisted to disk: on restart it is
	// reconstructed from the blocks already on disk (see newInner).
	//
	// TODO: consider giving this its own AtomicSend to avoid waking unrelated
	// inner waiters (PushVote, PushCommitQC, etc.) on markBlockPersisted calls.
	// Now that blocks are persisted concurrently by lane (one notification per
	// lane per batch, not per block), the frequency is lower, but still not
	// ideal. Only RecvBatch needs to be notified of cursor changes;
	// collectPersistBatch is in the same goroutine and reads it directly.
	nextBlockToPersist map[types.LaneID]types.BlockNumber

	// persistedBlockStart is the per-lane block number derived from the last
	// durably persisted prune anchor. Block admission (PushBlock, ProduceBlock,
	// WaitForCapacity, PushVote) uses persistedBlockStart + BlocksPerLane as
	// the capacity limit, ensuring we never admit more blocks than can be
	// recovered after a crash.
	persistedBlockStart map[types.LaneID]types.BlockNumber
}

// loadedAvailState holds data loaded from disk on restart.
// pruneAnchor is the decoded prune anchor (if any).
// commitQCs and blocks are pre-filtered: stale entries below the
// anchor have already been removed by loadPersistedState.
// commitQCs are sorted by road index; blocks are sorted by number per lane.
// newInner requires both to be contiguous and returns an error on gaps. That
// requirement is what makes persist.contiguousSuffix safe: it silently drops
// everything before the last hole it finds, so this is the only thing that
// distinguishes a lazily pruned record from genuinely lost data.
type loadedAvailState struct {
	pruneAnchor utils.Option[*PruneAnchor]
	commitQCs   []persist.LoadedCommitQC
	blocks      map[types.LaneID][]persist.LoadedBlock
}

func newInner(registry *epoch.Registry, loaded utils.Option[*loadedAvailState]) (*inner, error) {
	ep := registry.LatestEpoch()
	votes := map[types.LaneID]*queue[types.BlockNumber, blockVotes]{}
	blocks := map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{}
	for lane := range ep.Committee().Lanes().All() {
		votes[lane] = newQueue[types.BlockNumber, blockVotes]()
		blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
	}

	i := &inner{
		epoch:               ep,
		latestAppQC:         utils.None[*types.AppQC](),
		latestCommitQC:      utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		appVotes:            newQueue[types.GlobalBlockNumber, appVotes](),
		commitQCs:           newQueue[types.RoadIndex, *types.CommitQC](),
		blocks:              blocks,
		votes:               votes,
		nextBlockToPersist:  make(map[types.LaneID]types.BlockNumber, len(votes)),
		persistedBlockStart: make(map[types.LaneID]types.BlockNumber, len(votes)),
	}
	i.appVotes.prune(ep.FirstBlock())

	l, ok := loaded.Get()
	if !ok {
		return i, nil
	}

	// Re-attach persisted lane WALs before prune so leave lanes still named by
	// the tipcut get positioned correctly. Restoring extras is safe: they will
	// just be pruned later by tryPruneLeaveLanes. With an anchor CommitQC at
	// epoch N, skip lanes with e_join <= N absent from that epoch's committee —
	// those LaneIDs never rejoin (proposal laneRanges may omit empty lanes, so
	// membership is the registry committee, not the proposal map).
	var anchorEpoch types.EpochIndex
	var anchorCommittee *types.Committee
	if anchor, ok := l.pruneAnchor.Get(); ok {
		anchorEpoch = anchor.CommitQC.Proposal().EpochIndex()
		ep, ok := registry.EpochByIndex(anchorEpoch)
		if !ok {
			return nil, fmt.Errorf("unknown epoch_index %d for prune anchor", anchorEpoch)
		}
		anchorCommittee = ep.Committee()
	}
	for lane := range l.blocks {
		if anchorCommittee != nil && lane.EJoin() <= anchorEpoch && !anchorCommittee.HasLane(lane) {
			continue
		}
		if _, ok := i.blocks[lane]; ok {
			continue
		}
		i.blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
		i.votes[lane] = newQueue[types.BlockNumber, blockVotes]()
		i.nextBlockToPersist[lane] = 0
		i.persistedBlockStart[lane] = 0
	}

	// Apply the persisted prune anchor first: prune() positions all queues
	// (commitQCs, blocks, votes) so that subsequent pushBack calls insert
	// at the correct indices without needing reset().
	if anchor, ok := l.pruneAnchor.Get(); ok {
		logger.Info("loaded persisted prune anchor",
			slog.Uint64("roadIndex", uint64(anchor.AppQC.Proposal().RoadIndex())),
			slog.Uint64("globalNumber", uint64(anchor.AppQC.Proposal().GlobalNumber())),
		)
		if _, err := i.prune(anchorCommittee, anchor.AppQC, anchor.CommitQC); err != nil {
			return nil, fmt.Errorf("prune: %w", err)
		}
		for lane := range i.blocks {
			i.persistedBlockStart[lane] = anchor.CommitQC.LaneRange(lane).First()
		}
	}

	// Restore persisted CommitQCs. prune() may have already pushed the
	// anchor's CommitQC, so skip entries below commitQCs.next.
	for _, lqc := range l.commitQCs {
		if lqc.Index < i.commitQCs.next {
			continue
		}
		if lqc.Index != i.commitQCs.next {
			return nil, fmt.Errorf("non-contiguous persisted commitQCs: expected %d, got %d", i.commitQCs.next, lqc.Index)
		}
		i.commitQCs.pushBack(lqc.QC)
	}
	if i.commitQCs.next > i.commitQCs.first {
		i.latestCommitQC.Store(utils.Some(i.commitQCs.q[i.commitQCs.next-1]))
	}

	// Restore persisted blocks for every lane re-attached above. Gaps,
	// parent-hash mismatches, and over-capacity indicate corruption or a bug.
	for lane, bs := range l.blocks {
		q, ok := i.blocks[lane]
		if !ok || len(bs) == 0 {
			continue
		}
		// Anchor prune floors each tip via LaneRange.First(); lanes the tipcut
		// does not name get a synthetic First=0 (leave extras, or joiners that
		// only appear on post-anchor CommitQCs). WAL may still start later —
		// advance before load. A later AppQC moves those tips when its CommitQC
		// names the lane.
		if bs[0].Number > q.next {
			q.prune(bs[0].Number)
		}
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
			i.nextBlockToPersist[lane] = q.next
		}
	}

	return i, nil
}

// addCommitteeLanes adds empty queues for LaneIDs in c that are not yet tracked.
// Joiner tip catch-up is separate: new lanes start empty and fill via PushBlock / peer sync.
func (i *inner) addCommitteeLanes(c *types.Committee) {
	for lane := range c.Lanes().All() {
		if _, ok := i.blocks[lane]; ok {
			continue
		}
		i.blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
		i.votes[lane] = newQueue[types.BlockNumber, blockVotes]()
		i.nextBlockToPersist[lane] = 0
		i.persistedBlockStart[lane] = 0
	}
}

// removeLeaveLanes drops inactive leave lanes that the tipcut committee no
// longer names. A left LaneID never returns (rejoin is a new ID), so once the
// first retained CommitQC's committee omits it, no later CQ in the window can
// need it either. Returns removed LaneIDs for DeleteLane.
func (i *inner) removeLeaveLanes(current, tipcut *types.Committee) []types.LaneID {
	var removed []types.LaneID
	for lane := range i.blocks {
		if current.HasLane(lane) || tipcut.HasLane(lane) {
			continue
		}
		delete(i.blocks, lane)
		delete(i.votes, lane)
		delete(i.nextBlockToPersist, lane)
		delete(i.persistedBlockStart, lane)
		removed = append(removed, lane)
	}
	return removed
}

// TODO: filter votes per-epoch committee once epoch transitions are wired up.
func (i *inner) laneQC(lane types.LaneID, n types.BlockNumber) (*types.LaneQC, bool) {
	c := i.epoch.Committee()
	votes, ok := i.votes[lane]
	if !ok {
		return nil, false
	}
	entry, ok := votes.q[n]
	if !ok {
		return nil, false
	}
	for _, byHash := range entry.byHash {
		if byHash.weight >= c.LaneQuorum() {
			return types.NewLaneQC(byHash.votes[:]), true
		}
	}
	return nil, false
}

// prune advances the state to account for a new AppQC/CommitQC pair.
// Returns true if pruning occurred, false if the QC was stale.
func (i *inner) prune(c *types.Committee, appQC *types.AppQC, commitQC *types.CommitQC) (bool, error) {
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
	if i.commitQCs.next == idx {
		i.commitQCs.pushBack(commitQC)
		metrics.ObserveCommitQC(commitQC)
	}
	i.appVotes.prune(commitQC.GlobalRange().First)
	for lane := range i.votes {
		lr := commitQC.LaneRange(lane)
		i.votes[lr.Lane()].prune(lr.First())
		i.blocks[lr.Lane()].prune(lr.First())
		if i.nextBlockToPersist[lr.Lane()] < lr.First() {
			i.nextBlockToPersist[lr.Lane()] = lr.First()
		}
	}
	return true, nil
}
