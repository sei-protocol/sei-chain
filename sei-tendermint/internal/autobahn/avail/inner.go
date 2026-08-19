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
	consensusSpec     utils.AtomicSend[types.ConsensusSpec]
	roads             *queue[types.RoadIndex, *road]

	// epoch is the applied (next-CommitQC) epoch. installEpoch is the sole
	// writer after construction. Distinct from consensusSpec.Epoch, which is the
	// next-view epoch paired with the publishable tip and may lag while withheld.
	epoch utils.AtomicSend[*types.Epoch]
	// anchorEpoch is the epoch of data's Anchor CommitQC when one exists.
	// None until the first Anchor arrives (construction prune or runEvict).
	anchorEpoch utils.Option[*types.Epoch]
	blocks      map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes       map[types.LaneID]*queue[types.BlockNumber, *blockVotes]
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
type loadedState struct {
	commitQCs []*types.CommitQC
	blocks    map[types.LaneID][]persist.LoadedBlock
}

func newInner(ds *data.State, loaded *loadedState) (*inner, error) {
	start := ds.Registry().LatestEpoch()
	genesis, ok := ds.Registry().EpochByIndex(0)
	if !ok {
		return nil, fmt.Errorf("genesis epoch 0 not registered")
	}
	i := &inner{
		persistedCommitQC:  utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		consensusSpec:      utils.NewAtomicSend(types.ConsensusSpec{CommitQC: utils.None[*types.CommitQC](), Epoch: genesis}),
		roads:              newQueue[types.RoadIndex, *road](),
		epoch:              utils.NewAtomicSend(start),
		blocks:             map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{},
		votes:              map[types.LaneID]*queue[types.BlockNumber, *blockVotes]{},
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
		i.prune(anchor)
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
		if err := qc.Verify(epoch); err != nil {
			return nil, fmt.Errorf("persisted commitQC %d verify: %w", qc.Index(), err)
		}
		i.roads.pushBack(newRoad(qc, epoch))
	}
	if i.roads.Len() > 0 {
		last := i.roads.q[i.roads.next-1]
		i.persistedCommitQC.Store(utils.Some(last.commitQC))
		// Floor applied at the durable tip's verify-epoch. Bare Store on
		// purpose: this is a rewind from LatestEpoch, not an install.
		// The install loop below re-drives it from the durable leashes.
		i.epoch.Store(last.epoch)
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
	// Restart catch-up: install every epoch the durable leashes already allow.
	// The live path (runEpochAdvance) installs one waited-for epoch at a time.
	if err := i.installReadyEpochs(ds); err != nil {
		return nil, err
	}
	i.refreshConsensusSpec()
	return i, nil
}

// installEpoch makes ep the applied epoch: opens its lanes, reweights votes,
// and republishes ConsensusSpec.
func (i *inner) installEpoch(ep *types.Epoch) {
	for lane := range ep.Committee().Lanes().All() {
		i.addLane(lane)
	}
	// Publish applied epoch before reweight so reweightVotes reads i.epoch.
	// Callers hold the avail lock, so Epoch() waiters cannot observe votes
	// between the Store and the reweight.
	i.epoch.Store(ep)
	i.reweightVotes()
	i.refreshConsensusSpec()
}

// leashesMet reports whether the applied epoch is sealed and its prune leash is
// met. Sealed means roads hold the epoch's last CommitQC. The prune leash is met
// when the Anchor epoch covers the applied epoch (an AppQC for that epoch
// exists). The execution leash — registry contains the next epoch — is checked
// separately so live waiters are not parked on avail's lock for a registry update.
func (i *inner) leashesMet() bool {
	ep := i.epoch.Load()
	if i.roads.next < ep.RoadRange().Next {
		return false
	}
	ae, ok := i.anchorEpoch.Get()
	return ok && ae.EpochIndex() >= ep.EpochIndex()
}

// installReadyEpochs installs every epoch whose seal and prune leashes are
// already met. A missing next registry epoch in that state is an invariant
// violation (execution leash should already have registered it).
func (i *inner) installReadyEpochs(ds *data.State) error {
	for i.leashesMet() {
		nextIdx := i.epoch.Load().EpochIndex() + 1
		next, ok := ds.Registry().EpochByIndex(nextIdx)
		if !ok {
			return fmt.Errorf("epoch %d not registered with seal+prune leashes met", nextIdx)
		}
		i.installEpoch(next)
	}
	return nil
}

// refreshConsensusSpec publishes ConsensusSpec for the durable tip, paired with
// the epoch of the view that follows it. The spec is withheld — the previously
// published one stands — until that epoch is applied and resolvable.
//
// Withholding rather than publishing an earlier tip is what keeps the spec
// monotonic. At an epoch boundary the durable tip sits on LastRoad(E) while
// applied is still E, and a node that already entered E+1 before a restart must
// not be handed a predecessor of the tip it holds: installing it would roll the
// view backwards and discard that view's votes.
func (i *inner) refreshConsensusSpec() {
	tip := i.persistedCommitQC.Load()
	cqc, ok := tip.Get()
	if !ok {
		return
	}
	next := cqc.Index() + 1
	if epoch.IndexForRoad(next) > i.epoch.Load().EpochIndex() {
		return
	}
	ep, ok := i.epochForRoad(next).Get()
	if !ok {
		return
	}
	i.consensusSpec.Store(types.ConsensusSpec{CommitQC: tip, Epoch: ep})
}

func (i *inner) epochForRoad(road types.RoadIndex) utils.Option[*types.Epoch] {
	if ep := i.epoch.Load(); ep.RoadRange().Has(road) {
		return utils.Some(ep)
	}
	if road >= i.roads.first && road < i.roads.next {
		return utils.Some(i.roads.q[road].epoch)
	}
	// Persist may lag installEpoch: tip's next view can sit in an earlier epoch
	// still present on some admitted road.
	for idx := i.roads.first; idx < i.roads.next; idx++ {
		if ep := i.roads.q[idx].epoch; ep.RoadRange().Has(road) {
			return utils.Some(ep)
		}
	}
	return utils.None[*types.Epoch]()
}

func (i *inner) addLane(lane types.LaneID) bool {
	if _, ok := i.blocks[lane]; ok {
		return false
	}
	i.blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
	i.votes[lane] = newQueue[types.BlockNumber, *blockVotes]()
	i.nextBlockToPersist[lane] = 0
	return true
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

// laneQC returns the LaneQC for (lane, n) under the applied epoch's vote
// weighting (i.epoch), if one has formed.
func (i *inner) laneQC(lane types.LaneID, n types.BlockNumber) utils.Option[*types.LaneQC] {
	votes, ok := i.votes[lane]
	if !ok {
		return utils.None[*types.LaneQC]()
	}
	entry, ok := votes.q[n]
	if !ok {
		return utils.None[*types.LaneQC]()
	}
	return entry.qc
}

// reweightVotes recounts retained block votes under the applied epoch (i.epoch).
func (i *inner) reweightVotes() {
	ep := i.epoch.Load()
	for _, vq := range i.votes {
		for n := vq.first; n < vq.next; n++ {
			vq.q[n].reweight(ep)
		}
	}
}

// prune advances the state up to the data Anchor and drops lanes closed as of
// anchor.Epoch. Returns the number of lanes dropped.
func (i *inner) prune(anchor data.Anchor) int {
	anchorEpoch := anchor.Epoch
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
	i.anchorEpoch = utils.Some(anchorEpoch)
	var closed []types.LaneID
	for lane := range i.blocks {
		if anchorEpoch.IsClosed(lane) {
			closed = append(closed, lane)
		}
	}
	return i.dropLanes(closed)
}
