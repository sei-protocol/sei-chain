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
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewAppQC(votes)
}

func TestLaneQC(keys []types.SecretKey, header *types.BlockHeader) *types.LaneQC {
	vote := types.NewLaneVote(header)
	votes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewLaneQC(votes)
}

func TestCommitQC(
	rng utils.Rng,
	ep *types.Epoch,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
) (*types.FullCommitQC, []*types.Block) {
	return testCommitQCBlocks(rng, ep, keys, prev, 10)
}

func testCommitQCBlocks(
	rng utils.Rng,
	ep *types.Epoch,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
	blockCount int,
) (*types.FullCommitQC, []*types.Block) {
	committee := ep.Committee()
	blocks := map[types.LaneID][]*types.Block{}
	makeBlock := func(producer types.LaneID) *types.Block {
		if bs := blocks[producer]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			return types.NewBlock(producer, parent.Header().Next(), parent.Header().Hash(), types.GenPayload(rng))
		}
		return types.NewBlock(producer, types.LaneRangeOpt(prev, producer).Next(), types.GenBlockHeaderHash(rng), types.GenPayload(rng))
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
	var headers []*types.BlockHeader
	var blockList []*types.Block
	for lane := range committee.Lanes().All() {
		if bs := blocks[lane]; len(bs) > 0 {
			laneQCs[lane] = TestLaneQC(keys, bs[len(bs)-1].Header())
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
func TestBlockMap(blocks []*types.Block) map[uint64]*types.Block {
	result := make(map[uint64]*types.Block, len(blocks))
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
