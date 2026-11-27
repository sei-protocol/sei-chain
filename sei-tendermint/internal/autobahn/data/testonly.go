package data

import (
	"slices"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestAppQC(keys []types.SecretKey, proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	votes := make([]*types.Signed[*types.AppVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, k.Sign(vote))
	}
	return types.NewAppQC(votes)
}

// TestCommitQCOnLane builds the CommitQC of the road block after parent that finalizes one empty
// lane block on lane, and returns it with that lane block.
func TestCommitQCOnLane(
	ep *types.Epoch,
	keys []types.SecretKey,
	parent utils.Option[*types.CommitQC],
	lane types.LaneID,
) (*types.FullCommitQC, []*types.LaneBlock) {
	block := types.NewLaneBlock(lane, types.LaneRangeOpt(parent, lane).Next(), types.LaneBlockHeaderHash{}, &types.Payload{})
	laneQCs := map[types.LaneID]*types.LaneQC{lane: TestLaneQC(ep.EpochIndex(), keys, block.Header())}
	qc := types.BuildCommitQC(ep, keys, parent, laneQCs)
	return types.NewFullCommitQC(qc, []*types.LaneBlockHeader{block.Header()}), []*types.LaneBlock{block}
}

func TestLaneQC(epochIndex types.EpochIndex, keys []types.SecretKey, header *types.LaneBlockHeader) *types.LaneQC {
	vote := types.NewLaneVote(epochIndex, header)
	votes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, k.Sign(vote))
	}
	return types.NewLaneQC(votes)
}

func TestCommitQC(
	rng utils.Rng,
	ep *types.Epoch,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
) (*types.FullCommitQC, []*types.LaneBlock) {
	return testCommitQCBlocks(rng, ep, keys, prev, 10)
}

func testCommitQCBlocks(
	rng utils.Rng,
	ep *types.Epoch,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
	blockCount int,
) (*types.FullCommitQC, []*types.LaneBlock) {
	return testCommitQCBlocksWithPayload(rng, ep, keys, prev, blockCount, func() *types.Payload { return types.GenPayload(rng) })
}

// TestCommitQCWithPayload builds a commit QC with blocks from payload.
func TestCommitQCWithPayload(rng utils.Rng, ep *types.Epoch, keys []types.SecretKey, prev utils.Option[*types.CommitQC], payload func() *types.Payload) (*types.FullCommitQC, []*types.LaneBlock) {
	return testCommitQCBlocksWithPayload(rng, ep, keys, prev, 10, payload)
}

func testCommitQCBlocksWithPayload(rng utils.Rng, ep *types.Epoch, keys []types.SecretKey, prev utils.Option[*types.CommitQC], blockCount int, payload func() *types.Payload) (*types.FullCommitQC, []*types.LaneBlock) {
	committee := ep.Committee()
	blocks := map[types.LaneID][]*types.LaneBlock{}
	makeBlock := func(producer types.LaneID) *types.LaneBlock {
		if bs := blocks[producer]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			return types.NewLaneBlock(producer, parent.Header().Next(), parent.Header().Hash(), payload())
		}
		return types.NewLaneBlock(producer, types.LaneRangeOpt(prev, producer).Next(), types.GenLaneBlockHeaderHash(rng), payload())
	}
	available := slices.Collect(committee.Lanes().All())
	for range blockCount {
		i := rng.Intn(len(available))
		producer := available[i]
		blocks[producer] = append(blocks[producer], makeBlock(producer))
		if len(blocks[producer]) == types.MaxLaneRangeInProposal {
			available = slices.Delete(available, i, i+1)
		}
	}
	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.LaneBlockHeader
	var blockList []*types.LaneBlock
	for lane := range committee.Lanes().All() {
		if bs := blocks[lane]; len(bs) > 0 {
			laneQCs[lane] = TestLaneQC(ep.EpochIndex(), keys, bs[len(bs)-1].Header())
			for _, b := range bs {
				headers = append(headers, b.Header())
				blockList = append(blockList, b)
			}
		}
	}
	cqc := types.BuildCommitQC(ep, keys, prev, laneQCs)
	return types.NewFullCommitQC(cqc, headers), blockList
}

// TestBlockMap indexes blocks by their offsets in header order.
func TestBlockMap(blocks []*types.LaneBlock) map[uint64]*types.LaneBlock {
	result := make(map[uint64]*types.LaneBlock, len(blocks))
	for i, block := range blocks {
		result[uint64(i)] = block
	}
	return result
}

// TestAnchor constructs an uncertified anchor and its successor with the same committee.
func TestAnchor(ep *types.Epoch) Anchor {
	next := types.NewEpoch(ep.EpochIndex()+1, types.RoadRange{First: ep.RoadRange().Next, Next: ep.RoadRange().Next + (ep.RoadRange().Next - ep.RoadRange().First)}, ep.Committee())
	return Anchor{Epochs: [2]*types.Epoch{ep, next}}
}

// TestSetAnchor publishes an anchor to data state.
func TestSetAnchor(state *State, anchor Anchor) {
	for inner, ctrl := range state.inner.Lock() {
		inner.anchor.Store(anchor)
		ctrl.Updated()
	}
}
