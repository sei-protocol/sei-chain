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
	// blockPersisted tracks per-lane how far block persistence has progressed.
	// RecvBatch only yields blocks below this cursor for voting.
	// nil when persistence is disabled (testing); RecvBatch then uses q.next.
	// blockPersisted itself is not persisted to disk: on restart it is
	// reconstructed from the blocks already on disk (see newInner).
	blockPersisted map[types.LaneID]types.BlockNumber
}

// loadedAvailState holds data loaded from disk on restart.
// nil means fresh start (no persisted data).
// blocks are sorted by number and contiguous (gaps already resolved by loader).
type loadedAvailState struct {
	appQC  utils.Option[*types.AppQC]
	blocks map[types.LaneID][]persist.LoadedBlock
}

func newInner(c *types.Committee, loaded *loadedAvailState) *inner {
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

	if loaded == nil {
		return i
	}

	// Restore AppQC and advance queues past already-processed indices.
	i.latestAppQC = loaded.appQC
	if aq, ok := loaded.appQC.Get(); ok {
		// CommitQCs through this index have been processed; skip them.
		i.commitQCs.first = aq.Proposal().RoadIndex() + 1
		i.commitQCs.next = i.commitQCs.first
		// AppVotes through this global block number have been processed.
		i.appVotes.first = aq.Proposal().GlobalNumber() + 1
		i.appVotes.next = i.appVotes.first
	}

	// blockPersisted gates RecvBatch: only blocks below this cursor are
	// eligible for voting. Lanes without loaded blocks start at 0, which
	// is safe — an empty queue has nothing to vote on. New blocks must
	// arrive in order via PushBlock, get persisted, and the callback will
	// advance blockPersisted accordingly.
	i.blockPersisted = make(map[types.LaneID]types.BlockNumber, c.Lanes().Len())

	// Restore persisted blocks into their lane queues.
	for lane, bs := range loaded.blocks {
		q, ok := i.blocks[lane]
		if !ok || len(bs) == 0 {
			continue
		}
		first := bs[0].Number
		q.first = first
		q.next = first
		for _, b := range bs {
			q.q[q.next] = b.Proposal
			q.next++
		}
		// Loaded blocks are already on disk, so immediately consider them persisted.
		i.blockPersisted[lane] = q.next
		// Advance the votes queue to match so headers() returns ErrPruned
		// for already-committed blocks instead of blocking forever.
		vq := i.votes[lane]
		vq.first = first
		vq.next = first
	}

	return i
}

func (i *inner) laneQC(c *types.Committee, lane types.LaneID, n types.BlockNumber) (*types.LaneQC, bool) {
	for _, vs := range i.votes[lane].q[n].byHeader {
		if len(vs) >= c.LaneQuorum() {
			return types.NewLaneQC(vs[:c.LaneQuorum()]), true
		}
	}
	return nil, false
}

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
