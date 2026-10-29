package types

import (
	"encoding/hex"
	"slices"
	"time"

	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestRoadBlockVerify(t *testing.T) {
	for _, variant := range []string{"valid", "none", "missing", "extra", "reordered", "wrong", "payload", "QC signatures", "QC headers", "epoch", "road"} {
		t.Run(variant, func(t *testing.T) {
			rng := utils.TestRng()
			committee, keys := GenCommittee(rng, 3)
			ep := GenEpochWithCommittee(rng, committee)
			proposal, blocks := ProposalAtBlocks(ep, View{Index: ep.RoadRange().First}, 3)
			signingKeys := keys
			if variant == "QC signatures" {
				_, signingKeys = GenCommittee(rng, 3)
			}
			var votes []*Signed[*CommitVote]
			prepareQC := BuildPrepareQC(keys, proposal)
			for _, key := range signingKeys {
				votes = append(votes, Sign(key, NewCommitVote(prepareQC)))
			}
			headers := make([]*BlockHeader, len(blocks))
			for i, block := range blocks {
				headers[i] = block.Header()
			}
			commitQC := NewCommitQC(votes)
			switch variant {
			case "none":
				blocks = nil
			case "missing":
				blocks = slices.Delete(blocks, 1, 2)
			case "extra":
				blocks = append(blocks, blocks[0])
			case "reordered":
				blocks[0], blocks[1] = blocks[1], blocks[0]
			case "wrong":
				blocks[0] = GenBlock(rng)
			case "payload":
				wire := BlockConv.Encode(blocks[0])
				wire.Payload = PayloadConv.Encode(GenPayload(rng))
				var err error
				blocks[0], err = BlockConv.Decode(wire)
				require.NoError(t, err)
			case "QC headers":
				slices.Reverse(headers)
				slices.Reverse(blocks)
			case "epoch":
				ep = NewEpoch(ep.EpochIndex()+1, ep.RoadRange(), ep.FirstTimestamp(), committee)
			case "road":
				roads := ep.RoadRange()
				roads.First++
				ep = NewEpoch(ep.EpochIndex(), roads, ep.FirstTimestamp(), committee)
			}
			qc := NewFullCommitQC(commitQC, headers)
			err := NewRoadBlock(qc, blocks).Verify(ep)
			switch variant {
			case "valid":
				require.NoError(t, err)
			case "none", "missing", "extra", "reordered", "wrong", "payload":
				require.ErrorIs(t, err, ErrQCBlocksMismatch)
			default:
				require.Error(t, err)
			}
		})
	}
}

func TestRoadBlockHeaderHashIndependentOfView(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 3)
	ep := GenEpochWithCommittee(rng, committee)
	proposal, blocks := ProposalAtBlocks(ep, View{Index: ep.RoadRange().First}, 2)
	wire := ProposalConv.Encode(proposal)
	wire.ViewNumber = utils.Alloc(uint64(proposal.View().Number.Next()))
	reproposal, err := ProposalConv.Decode(wire)
	require.NoError(t, err)
	require.Equal(t, proposal.Header().Hash(), reproposal.Header().Hash())
	require.NotEqual(t, NewHashed(proposal).Hash(), NewHashed(reproposal).Hash())

	key := GenSecretKey(rng)
	qc := NewCommitQC([]*Signed[*CommitVote]{Sign(key, NewCommitVote(BuildPrepareQC([]SecretKey{key}, proposal)))})
	reproposalQC := NewCommitQC([]*Signed[*CommitVote]{Sign(key, NewCommitVote(BuildPrepareQC([]SecretKey{key}, reproposal)))})
	require.Equal(t, proposal.Header().Hash(), qc.Header().Hash())
	require.Equal(t, qc.Header().Hash(), reproposalQC.Header().Hash())
	headers := make([]*BlockHeader, len(blocks))
	for i, block := range blocks {
		headers[i] = block.Header()
	}
	roadBlock := NewRoadBlock(NewFullCommitQC(qc, headers), blocks)
	require.Equal(t, proposal.Header(), roadBlock.Header())
	require.Equal(t, roadBlock.Header().Hash(), roadBlock.SuperBlock().Hash)
}

func TestRoadBlockHeaderHashCanonicalLaneRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 3)
	ep := GenEpochWithCommittee(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposal, err := NewProposal(leaderKey(committee, keys, vs.View()), vs, vs.NextTimestamp(), nil)
	require.NoError(t, err)
	header := proposal.Header()
	wire := RoadBlockHeaderConv.Encode(header)
	require.Len(t, wire.LaneRanges, 3)
	slices.Reverse(wire.LaneRanges)
	reordered, err := RoadBlockHeaderConv.Decode(wire)
	require.NoError(t, err)
	require.Equal(t, header.Hash(), reordered.Hash())

	wire.Timestamp = TimeConv.Encode(header.Timestamp().Add(time.Microsecond))
	changedTime, err := RoadBlockHeaderConv.Decode(wire)
	require.NoError(t, err)
	require.NotEqual(t, header.Hash(), changedTime.Hash())

	wire = RoadBlockHeaderConv.Encode(header)
	wire.LaneRanges[0].LastHash = slices.Clone(wire.LaneRanges[0].LastHash)
	wire.LaneRanges[0].LastHash[0] ^= 1
	changedRange, err := RoadBlockHeaderConv.Decode(wire)
	require.NoError(t, err)
	require.NotEqual(t, header.Hash(), changedRange.Hash())
}

func TestRoadBlockHeaderHashBytes(t *testing.T) {
	rng := utils.TestRng()
	hash := GenProposal(rng).Header().Hash()
	parsed, err := ParseRoadBlockHeaderHash(hash.Bytes())
	require.NoError(t, err)
	require.Equal(t, hash, parsed)
	require.Equal(t, hex.EncodeToString(hash.Bytes()), hash.String())
	for _, size := range []int{0, 31, 33} {
		_, err := ParseRoadBlockHeaderHash(make([]byte, size))
		require.Error(t, err)
	}
}

func TestRoadBlockHeaderIdentity(t *testing.T) {
	rng := utils.TestRng()
	key := GenSecretKey(rng)
	hashes := map[RoadBlockHeaderHash]struct{}{}
	for _, view := range []View{
		{},
		{EpochIndex: 7, Index: 29, Number: 13},
		{EpochIndex: 7, Index: 30, Number: 13},
		{EpochIndex: 8, Index: 29, Number: 13},
	} {
		proposal, err := ProposalConv.Decode(&pb.Proposal{
			ViewNumber: utils.Alloc(uint64(view.Number)),
			Header: &pb.RoadBlockHeader{
				EpochIndex: utils.Alloc(uint64(view.EpochIndex)),
				Index:      utils.Alloc(uint64(view.Index)),
				Timestamp:  TimeConv.Encode(time.Time{}),
			},
		})
		require.NoError(t, err)
		header := proposal.Header()
		require.Equal(t, view.EpochIndex, header.EpochIndex())
		require.Equal(t, view.Index, header.Index())
		require.Equal(t, view, proposal.View())
		require.Equal(t, view, NewPrepareVote(proposal).View())
		require.Equal(t, view, NewCommitVote(BuildPrepareQC([]SecretKey{key}, proposal)).View())
		_, seen := hashes[header.Hash()]
		require.False(t, seen)
		hashes[header.Hash()] = struct{}{}
	}
}

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

func TestLaneRangeVerifyRejectsHashAtHeightZero(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 4)
	lane := committee.Lanes().At(0)

	require.Error(t, (&LaneRange{lane: lane, first: 0, next: 0, lastHash: GenBlockHeaderHash(rng)}).Verify(committee))
	require.NoError(t, (&LaneRange{lane: lane, first: 0, next: 0}).Verify(committee))
	// Past height 0 an empty range may carry a tip. FullProposal.Verify checks that it matches.
	require.NoError(t, (&LaneRange{
		lane: lane, first: 1, next: 1, lastHash: GenBlockHeaderHash(rng),
	}).Verify(committee))
}
