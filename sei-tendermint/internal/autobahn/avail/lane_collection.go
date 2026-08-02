package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneCollection owns per-lane block/vote pipelines and their durable ranges.
type laneCollection struct {
	byID map[types.LaneID]*laneState
}

type laneState struct {
	blocks  *queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes   *queue[types.BlockNumber, *blockVotes]
	durable laneDurability
}

// laneDurability tracks the durable prune floor and write tip for one lane.
// Invariant: persistedBlockFirst ≤ blocks.first ≤ persistedBlockNext ≤ blocks.next
// (blocks.first/next live on laneState; this type owns only the durable cursors).
type laneDurability struct {
	// persistedBlockNext tracks how far block persistence has progressed.
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
	persistedBlockNext types.BlockNumber
	// persistedBlockFirst is the block number derived from the last durably
	// persisted prune anchor for this lane. Block admission (PushBlock,
	// ProduceBlock, WaitForCapacity, PushVote) uses persistedBlockFirst +
	// BlocksPerLane as the capacity limit, ensuring we never admit more blocks
	// than can be recovered after a crash.
	persistedBlockFirst types.BlockNumber
}

func newLaneState() *laneState {
	return &laneState{
		blocks: newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]](),
		votes:  newQueue[types.BlockNumber, *blockVotes](),
	}
}

func (c *laneCollection) get(lane types.LaneID) (*laneState, bool) {
	ls, ok := c.byID[lane]
	return ls, ok
}

func (c *laneCollection) getOrInsert(lane types.LaneID) *laneState {
	if ls, ok := c.byID[lane]; ok {
		return ls
	}
	ls := newLaneState()
	c.byID[lane] = ls
	return ls
}

func (c *laneCollection) laneQC(lane types.LaneID, n types.BlockNumber) utils.Option[*types.LaneQC] {
	bv, ok := c.byID[lane].votes.q[n]
	if !ok {
		return utils.None[*types.LaneQC]()
	}
	return bv.laneQC()
}

// pruneTo floors each lane's vote/block queues and durable cursor to the
// CommitQC's per-lane First (App tip prune watermark).
func (c *laneCollection) pruneTo(qc *types.CommitQC) {
	for lane, ls := range c.byID {
		ls.pruneTo(qc.LaneRange(lane).First())
	}
}

func (ls *laneState) pruneTo(first types.BlockNumber) {
	ls.votes.prune(first)
	ls.blocks.prune(first)
	ls.durable.floorNext(first)
}

func (d *laneDurability) admitLimit() types.BlockNumber {
	return d.persistedBlockFirst + BlocksPerLane
}

func (d *laneDurability) advanceFirst(first types.BlockNumber) bool {
	if first > d.persistedBlockFirst {
		d.persistedBlockFirst = first
		return true
	}
	return false
}

func (d *laneDurability) advanceNext(next types.BlockNumber) {
	d.persistedBlockNext = next
}

func (d *laneDurability) floorNext(first types.BlockNumber) {
	if d.persistedBlockNext < first {
		d.persistedBlockNext = first
	}
}
