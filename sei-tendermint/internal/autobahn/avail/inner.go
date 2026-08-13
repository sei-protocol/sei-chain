package avail

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// inner holds roads and per-LaneID block/vote maps.
type inner struct {
	persistedCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]] // latest persisted CommitQC
	roads             *queue[types.RoadIndex, *road]

	// epoch is the applied (next-CommitQC) epoch. ApplyEpoch is the sole
	// writer after construction.
	epoch utils.AtomicSend[*types.Epoch]
	// anchorEpoch is the epoch of data's Anchor CommitQC. Before any Anchor
	// exists it equals the applied epoch.
	anchorEpoch *types.Epoch
	blocks      map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes       map[types.LaneID]*queue[types.BlockNumber, blockVotes]
	// nextBlockToPersist tracks per-lane how far block persistence has progressed.
	// RecvBatch only yields blocks below this cursor for voting.
	// Always initialized (even when persistence is disabled — the no-op persist
	// goroutine bumps it immediately). Not persisted to disk: on restart it is
	// reconstructed from the blocks already on disk (see newInner).
	//
	// TODO: consider giving this its own AtomicSend to avoid waking unrelated
	// inner waiters (PushVote, PushCommitQC, etc.) on setNextBlockToPersist calls.
	// Now that blocks are persisted concurrently by lane (one notification per
	// lane per batch, not per block), the frequency is lower, but still not
	// ideal. Only RecvBatch needs to be notified of cursor changes;
	// collectPersistBatch is in the same goroutine and reads it directly.
	nextBlockToPersist map[types.LaneID]types.BlockNumber
}

// loadedState holds data loaded from disk on restart.
// commitQCs are sorted by road index; blocks are sorted by number per lane.
// newInner requires both to be contiguous and returns an error on gaps. That
// requirement is what makes persist.contiguousSuffix safe: it silently drops
// everything before the last hole it finds, so this is the only thing that
// distinguishes a lazily pruned record from genuinely lost data.
// newInner requires both to be contiguous and returns an error on gaps.
type loadedState struct {
	commitQCs []*types.CommitQC
	blocks    map[types.LaneID][]persist.LoadedBlock
}

func newInner(ds *data.State, loaded *loadedState) (*inner, error) {
	start := ds.Registry().LatestEpoch()
	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		roads:              newQueue[types.RoadIndex, *road](),
		epoch:              utils.NewAtomicSend(start),
		anchorEpoch:        start,
		blocks:             map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, blockVotes]{},
		nextBlockToPersist: map[types.LaneID]types.BlockNumber{},
	}
	for lane := range start.Committee().Lanes().All() {
		i.addLane(lane)
	}
	for lane := range loaded.blocks {
		i.addLane(lane)
	}

	// Apply the persisted prune anchor from the data.State.
	if anchor, ok := ds.Anchor().Load().Get(); ok {
		ep, err := anchorEpochOf(ds.Registry(), anchor)
		if err != nil {
			return nil, err
		}
		i.prune(anchor, ep)
	}

	// Restore persisted CommitQCs. prune() may have already pushed the
	// anchor's CommitQC, so skip entries below commitQCs.next.
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
	}
	if i.roads.Len() > 0 {
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
		for _, b := range bs {
			if q.Len() >= BlocksPerLane {
				return nil, fmt.Errorf("lane %s: loaded %d blocks exceeds capacity %d", lane, len(bs), BlocksPerLane)
			}
			if b.Number < q.next {
				continue
			}
			if b.Number != q.next {
				return nil, fmt.Errorf("lane %s: non-contiguous persisted blocks: expected %d, got %d", lane, q.next, b.Number)
			}
			// We check the parent hash only for the blocks above the anchor, because:
			// * node can cast LaneVote for the block of the lane without checking the parent hash,
			//   in case the previous block was already (executed and) pruned from memory.
			// * current WAL implementation is lazily pruning on disk, so old executed blocks might be loaded on startup.
			if q.Len() > 0 {
				ph := b.Proposal.Msg().Block().Header().ParentHash()
				if q.q[q.next-1].Msg().Block().Header().Hash() != ph {
					return nil, fmt.Errorf("lane %s: parent hash mismatch at block %d", lane, b.Number)
				}
			}
			q.pushBack(b.Proposal)
		}
		i.nextBlockToPersist[lane] = q.next
	}
	return i, nil
}

// laneQC returns a LaneQC for (lane, n) if weight under the applied epoch's
// committee meets LaneQuorum. Does not reweight votes when the committee changes.
func (i *inner) laneQC(lane types.LaneID, n types.BlockNumber) (*types.LaneQC, bool) {
	c := i.epoch.Load().Committee()
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

// addLane ensures empty block/vote queues and a zero persist cursor for lane.
// No-op if the lane is already present.
func (i *inner) addLane(lane types.LaneID) {
	if _, ok := i.blocks[lane]; ok {
		return
	}
	i.blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
	i.votes[lane] = newQueue[types.BlockNumber, blockVotes]()
	i.nextBlockToPersist[lane] = 0
}

func (i *inner) dropLanes(lanes []types.LaneID) int {
	n := 0
	for _, lane := range lanes {
		if _, ok := i.blocks[lane]; !ok {
			continue
		}
		delete(i.blocks, lane)
		delete(i.votes, lane)
		delete(i.nextBlockToPersist, lane)
		n++
	}
	return n
}

// anchorEpochOf returns the epoch of anchor's CommitQC from the registry.
func anchorEpochOf(registry *epoch.Registry, anchor data.Anchor) (*types.Epoch, error) {
	ei := anchor.CommitQC.Proposal().EpochIndex()
	ep, ok := registry.EpochByIndex(ei)
	if !ok {
		return nil, fmt.Errorf("unknown epoch_index %d for anchor", ei)
	}
	return ep, nil
}

// prune advances the state up to the data Anchor and drops lanes closed as of
// anchorEpoch. Returns the number of lanes dropped.
func (i *inner) prune(anchor data.Anchor, anchorEpoch *types.Epoch) int {
	idx := anchor.CommitQC.Index()
	if idx >= i.roads.first {
		i.roads.prune(idx + 1)
		for lane, vq := range i.votes {
			lr := anchor.CommitQC.LaneRange(lane)
			bq := i.blocks[lane]
			vq.prune(lr.Next())
			bq.prune(lr.Next())
			if i.nextBlockToPersist[lane] < lr.Next() {
				i.nextBlockToPersist[lane] = lr.Next()
			}
		}
		if i.roads.Len() == 0 {
			i.persistedCommitQC.Store(utils.Some(anchor.CommitQC))
		}
	}
	i.anchorEpoch = anchorEpoch
	var closed []types.LaneID
	for lane := range i.blocks {
		if anchorEpoch.IsClosed(lane) {
			closed = append(closed, lane)
		}
	}
	return i.dropLanes(closed)
}
