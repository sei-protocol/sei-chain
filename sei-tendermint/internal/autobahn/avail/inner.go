package avail

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// TODO: when dynamic committee changes are supported, newly joined members
// must be added to blocks, votes, and nextBlockToPersist.
// Currently all four are initialized once in newInner from c.Lanes().All().
// BlockPersister creates lane WALs lazily inside MaybePruneAndPersistLane, but the new
// member must also appear in inner.blocks before the next persist cycle.
type inner struct {
	persistedCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]] // latest persisted CommitQC
	roads             *queue[types.RoadIndex, *road]
	nextAppQC         types.RoadIndex

	// Epoch is the current epoch for blocks votes collection.
	epoch  *types.Epoch
	blocks map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes  map[types.LaneID]*queue[types.BlockNumber, blockVotes]
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
}

// loadedState holds data loaded from disk on restart.
// pruneAnchor is the decoded prune anchor (if any).
// commitQCs and blocks are pre-filtered: stale entries below the
// anchor have already been removed by loadPersistedState.
// commitQCs are sorted by road index; blocks are sorted by number per lane.
// newInner requires both to be contiguous and returns an error on gaps.
type loadedState struct {
	commitQCs []*types.CommitQC
	blocks    map[types.LaneID][]persist.LoadedBlock
}

func newInner(ds *data.State, loaded *loadedState) (*inner, error) {
	epoch := ds.Registry().LatestEpoch()
	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		roads:              newQueue[types.RoadIndex, *road](),
		epoch:              epoch,
		blocks:             map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, blockVotes]{},
		nextBlockToPersist: map[types.LaneID]types.BlockNumber{},
	}
	for lane := range epoch.Committee().Lanes().All() {
		i.blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
		i.votes[lane] = newQueue[types.BlockNumber, blockVotes]()
	}

	// Apply the persisted prune anchor from the data.State:
	// avail.State can drop everything below AppQC persisted in data.State.
	if anchor, ok := ds.Anchor().Load().Get(); ok {
		epoch, ok := ds.Registry().EpochByIndex(anchor.CommitQC.Proposal().EpochIndex())
		if !ok {
			return nil, fmt.Errorf("epoch not found")
		}
		i.prune(epoch, anchor)
	}

	// Restore persisted CommitQCs. prune() may have already pushed the
	// anchor's CommitQC, so skip entries below commitQCs.next.
	setPersisted := false
	for _, qc := range loaded.commitQCs {
		if qc.Index() < i.roads.next {
			continue
		}
		if qc.Index() != i.roads.next {
			return nil, fmt.Errorf("non-contiguous persisted commitQCs: expected %d, got %d", i.roads.next, qc.Index())
		}
		epoch, ok := ds.Registry().EpochByIndex(qc.Proposal().EpochIndex())
		if !ok {
			return nil, fmt.Errorf("epoch not found")
		}
		i.roads.pushBack(newRoad(qc, epoch))
		setPersisted = true
	}
	// It may happen that data.State has progressed beyond avail state.
	// In this case the whole persisted avail.State is invalidated and anchor.CommitQC
	// is NOT stored in avail.State. We need it to get persisted before we update persistedCommitQC.
	if setPersisted {
		i.persistedCommitQC.Store(utils.Some(i.roads.q[i.roads.next-1].commitQC))
	}

	// Restore persisted blocks. Since the anchor is persisted first and
	// blocks are written sequentially per lane, gaps, parent-hash
	// mismatches, and over-capacity indicate corruption or a bug.
	for lane, bs := range loaded.blocks {
		q, ok := i.blocks[lane]
		if !ok || len(bs) == 0 {
			continue
		}
		var lastHash types.BlockHeaderHash
		for j, b := range bs {
			if q.Len() >= BlocksPerLane {
				return nil, fmt.Errorf("lane %s: loaded %d blocks exceeds capacity %d", lane, len(bs), BlocksPerLane)
			}
			if j > 0 {
				if got := b.Proposal.Msg().Block().Header().ParentHash(); got != lastHash {
					return nil, fmt.Errorf("lane %s: parent hash mismatch at block %d", lane, b.Number)
				}
			}
			lastHash = b.Proposal.Msg().Block().Header().Hash()
			if b.Number < q.next {
				continue
			}
			if b.Number != q.next {
				return nil, fmt.Errorf("lane %s: non-contiguous persisted blocks: expected %d, got %d", lane, q.next, b.Number)
			}
			q.pushBack(b.Proposal)
		}
		i.nextBlockToPersist[lane] = q.next
	}
	return i, nil
}

// TODO: filter votes per-epoch committee once epoch transitions are wired up.
func (i *inner) laneQC(lane types.LaneID, n types.BlockNumber) (*types.LaneQC, bool) {
	c := i.epoch.Committee()
	for _, byHash := range i.votes[lane].q[n].byHash {
		if byHash.weight >= c.LaneQuorum() {
			return types.NewLaneQC(byHash.votes[:]), true
		}
	}
	return nil, false
}

func (i *inner) updateNextAppQC() bool {
	updated := false
	for i.nextAppQC < i.roads.next && i.roads.q[i.nextAppQC].appQC.IsPresent() {
		i.nextAppQC += 1
		updated = true
	}
	return updated
}

// prune advances the state up to Anchor of the data state.
// Returns true iff pruning occurred.
func (i *inner) prune(epoch *types.Epoch, anchor data.Anchor) {
	idx := anchor.CommitQC.Index()
	if idx < i.roads.first {
		return
	}
	i.roads.prune(idx)
	i.nextAppQC = max(idx, i.nextAppQC)
	if idx == i.roads.next {
		i.roads.pushBack(newRoad(anchor.CommitQC, epoch))
	}
	i.roads.q[idx].appQC = utils.Some(anchor.AppQC)
	i.updateNextAppQC()
	for lane := range i.votes {
		lr := anchor.CommitQC.LaneRange(lane)
		i.votes[lr.Lane()].prune(lr.First())
		i.blocks[lr.Lane()].prune(lr.First())
		if i.nextBlockToPersist[lr.Lane()] < lr.First() {
			i.nextBlockToPersist[lr.Lane()] = lr.First()
		}
	}
}
