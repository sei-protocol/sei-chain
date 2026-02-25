package avail

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type inner struct {
	latestAppQC    utils.Option[*types.AppQC]
	latestCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]]
	appVotes       *queue[types.GlobalBlockNumber, appVotes]
	commitQCs      *queue[types.RoadIndex, *types.CommitQC]
	blocks         map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes          map[types.LaneID]*queue[types.BlockNumber, blockVotes]
	// nextBlockToPersist tracks per-lane how far block persistence has progressed.
	// RecvBatch only yields blocks below this cursor for voting.
	// nil when persistence is disabled (testing); RecvBatch then uses q.next.
	// nextBlockToPersist itself is not persisted to disk: on restart it is
	// reconstructed from the blocks already on disk (see newInner).
	nextBlockToPersist map[types.LaneID]types.BlockNumber
}

// loadedAvailState holds data loaded from disk on restart.
// blocks are sorted by number and contiguous (gaps already resolved by loader).
// commitQCs are sorted by road index and contiguous (gaps already resolved by loader).
type loadedAvailState struct {
	appQC     utils.Option[*types.AppQC]
	commitQCs []persist.LoadedCommitQC
	blocks    map[types.LaneID][]persist.LoadedBlock
}

func newInner(c *types.Committee, loaded utils.Option[*loadedAvailState]) (*inner, error) {
	votes := map[types.LaneID]*queue[types.BlockNumber, blockVotes]{}
	blocks := map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{}
	for _, lane := range c.Lanes().All() {
		votes[lane] = newQueue[types.BlockNumber, blockVotes]()
		blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
	}

	i := &inner{
		latestAppQC:    utils.None[*types.AppQC](),
		latestCommitQC: utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		appVotes:       newQueue[types.GlobalBlockNumber, appVotes](),
		commitQCs:      newQueue[types.RoadIndex, *types.CommitQC](),
		blocks:         blocks,
		votes:          votes,
	}

	l, ok := loaded.Get()
	if !ok {
		return i, nil
	}

	// Restore persisted CommitQCs into the queue.
	if len(l.commitQCs) > 0 {
		i.commitQCs.reset(l.commitQCs[0].Index)
		for _, lqc := range l.commitQCs {
			if lqc.Index != i.commitQCs.next {
				return nil, fmt.Errorf("non-consecutive commitQC: got index %d, want %d", lqc.Index, i.commitQCs.next)
			}
			i.commitQCs.pushBack(lqc.QC)
		}
		i.latestCommitQC.Store(utils.Some(i.commitQCs.q[i.commitQCs.next-1]))
	}

	// nextBlockToPersist gates RecvBatch: only blocks below this cursor are
	// eligible for voting. Lanes without loaded blocks start at 0, which
	// is safe — an empty queue has nothing to vote on. New blocks must
	// arrive in order via PushBlock, get persisted, and the callback will
	// advance nextBlockToPersist accordingly.
	i.nextBlockToPersist = make(map[types.LaneID]types.BlockNumber, c.Lanes().Len())

	// Restore persisted blocks into their lane queues.
	for lane, bs := range l.blocks {
		q, ok := i.blocks[lane]
		if !ok || len(bs) == 0 {
			continue
		}
		first := bs[0].Number
		q.reset(first)
		for _, b := range bs {
			q.pushBack(b.Proposal)
		}
		// Loaded blocks are already on disk, so immediately consider them persisted.
		i.nextBlockToPersist[lane] = q.next
		// Votes are not persisted (cheap to re-gossip, short-lived, and
		// high-volume per block × validator). Advance the votes queue past
		// loaded blocks so that headers() returns ErrPruned for blocks
		// before `first` instead of blocking forever waiting for votes
		// that will never arrive.
		i.votes[lane].reset(first)
	}

	// Prune all queues based on the persisted AppQC. Called after loading
	// so that prune() operates on populated queues (same path as runtime).
	if aq, ok := l.appQC.Get(); ok {
		idx := aq.Proposal().RoadIndex()
		qc, ok := i.commitQCs.q[idx]
		if !ok {
			// AppQC is persisted after CommitQCs in the same goroutine,
			// so the matching CommitQC is always on disk before the AppQC.
			// If we get here, the persisted state is corrupt.
			return nil, fmt.Errorf("persisted AppQC at road index %d but no matching commitQC on disk", idx)
		}
		if _, err := i.prune(aq, qc); err != nil {
			return nil, fmt.Errorf("prune: %w", err)
		}
	}

	return i, nil
}

func (i *inner) laneQC(c *types.Committee, lane types.LaneID, n types.BlockNumber) (*types.LaneQC, bool) {
	for _, vs := range i.votes[lane].q[n].byHeader {
		if len(vs) >= c.LaneQuorum() {
			return types.NewLaneQC(vs[:c.LaneQuorum()]), true
		}
	}
	return nil, false
}

// prune advances the state to account for a new AppQC/CommitQC pair.
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
	i.commitQCs.prune(idx)
	if i.commitQCs.next == idx {
		i.commitQCs.pushBack(commitQC)
		i.latestCommitQC.Store(utils.Some(commitQC))
	}
	i.appVotes.prune(commitQC.GlobalRange().First)
	for lane := range i.votes {
		lr := commitQC.LaneRange(lane)
		i.votes[lr.Lane()].prune(lr.First())
		i.blocks[lr.Lane()].prune(lr.First())
	}
	return true, nil
}
