package types

import (
	"slices"
	"testing"

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
				ep = NewEpoch(ep.EpochIndex()+1, ep.RoadRange(), committee)
			case "road":
				roads := ep.RoadRange()
				roads.First++
				ep = NewEpoch(ep.EpochIndex(), roads, committee)
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
