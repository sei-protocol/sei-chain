package types

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestSuperBlockMergesTransactionsInHeaderOrder(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 2)
	ep := GenEpochWithCommittee(rng, committee)
	var blocks []*Block
	var headers []*BlockHeader
	laneQCs := map[LaneID]*LaneQC{}
	for laneIndex := range committee.Lanes().Len() {
		lane := committee.Lanes().At(laneIndex)
		var parent BlockHeaderHash
		for offset := range 2 - laneIndex {
			i := len(blocks)
			payload, err := (PayloadBuilder{CreatedAt: ep.FirstTimestamp(), TotalGasWanted: uint64(i + 1), Txs: [][]byte{{byte(i), 0}, {byte(i), 1}}}).Build()
			require.NoError(t, err)
			block := NewBlock(lane, BlockNumber(offset), parent, payload)
			blocks = append(blocks, block)
			headers = append(headers, block.Header())
			parent = block.Header().Hash()
		}
		votes := make([]*Signed[*LaneVote], len(keys))
		for i, key := range keys {
			votes[i] = Sign(key, NewLaneVote(headers[len(headers)-1]))
		}
		laneQCs[lane] = NewLaneQC(votes)
	}
	qc := BuildCommitQC(ep, keys, utils.None[*CommitQC](), laneQCs)
	fullQC := NewFullCommitQC(qc, headers)
	require.NoError(t, fullQC.Verify(ep))
	block := NewRoadBlock(fullQC, blocks).SuperBlock()
	require.Equal(t, qc.Index(), block.Height)
	require.Equal(t, qc.Header().Timestamp(), block.Timestamp)
	require.Equal(t, uint64(6), block.GasWanted)
	require.Equal(t, [][]byte{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {2, 0}, {2, 1}}, block.Txs)
}
