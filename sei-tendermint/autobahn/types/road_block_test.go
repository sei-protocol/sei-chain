package types

import (
	"encoding/hex"
	"slices"
	"time"

	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestRoadBlockVerify(t *testing.T) {
	for _, variant := range []string{"valid", "none", "missing", "extra", "reordered", "wrong", "payload", "QC signatures", "QC headers", "epoch", "road"} {
		t.Run(variant, func(t *testing.T) {
			rng := utils.TestRng()
			committee, keys := GenCommittee(rng, 3)
			ep := GenEpochWithCommittee(rng, committee)
			proposal, blocks := ProposalAtBlocks(ep, View{Height: ep.RoadRange().First}, 3)
			signingKeys := keys
			if variant == "QC signatures" {
				_, signingKeys = GenCommittee(rng, 3)
			}
			var votes []*Signed[*CommitVote]
			prepareQC := BuildPrepareQC(keys, proposal)
			for _, key := range signingKeys {
				votes = append(votes, Sign(key, NewCommitVote(prepareQC)))
			}
			headers := make([]*LaneBlockHeader, len(blocks))
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
				blocks[0] = GenLaneBlock(rng)
			case "payload":
				wire := LaneBlockConv.Encode(blocks[0])
				wire.Payload = PayloadConv.Encode(GenPayload(rng))
				var err error
				blocks[0], err = LaneBlockConv.Decode(wire)
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

func TestRoadBlockHeaderHashIndependentOfView(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 3)
	ep := GenEpochWithCommittee(rng, committee)
	proposal, blocks := ProposalAtBlocks(ep, View{Height: ep.RoadRange().First}, 2)
	wire := ProposalConv.Encode(proposal)
	wire.ViewNumber = new(uint64(proposal.View().Number.Next()))
	reproposal, err := ProposalConv.Decode(wire)
	require.NoError(t, err)
	require.Equal(t, proposal.Header().Hash(), reproposal.Header().Hash())
	require.NotEqual(t, NewHashed(proposal).Hash(), NewHashed(reproposal).Hash())

	key := GenSecretKey(rng)
	qc := NewCommitQC([]*Signed[*CommitVote]{Sign(key, NewCommitVote(BuildPrepareQC([]SecretKey{key}, proposal)))})
	reproposalQC := NewCommitQC([]*Signed[*CommitVote]{Sign(key, NewCommitVote(BuildPrepareQC([]SecretKey{key}, reproposal)))})
	require.Equal(t, proposal.Header().Hash(), qc.Header().Hash())
	require.Equal(t, qc.Header().Hash(), reproposalQC.Header().Hash())
	headers := make([]*LaneBlockHeader, len(blocks))
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
		{EpochIndex: 7, Height: 29, Number: 13},
		{EpochIndex: 7, Height: 30, Number: 13},
		{EpochIndex: 8, Height: 29, Number: 13},
	} {
		proposal, err := ProposalConv.Decode(&pb.Proposal{
			ViewNumber: new(uint64(view.Number)),
			Header: &pb.RoadBlockHeader{
				EpochIndex: new(uint64(view.EpochIndex)),
				Height:     new(uint64(view.Height)),
				Timestamp:  TimeConv.Encode(time.Time{}),
			},
		})
		require.NoError(t, err)
		header := proposal.Header()
		require.Equal(t, view.EpochIndex, header.EpochIndex())
		require.Equal(t, view.Height, header.Height())
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
	var blocks []*LaneBlock
	var headers []*LaneBlockHeader
	laneQCs := map[LaneID]*LaneQC{}
	for laneIndex := range committee.Lanes().Len() {
		lane := committee.Lanes().At(laneIndex)
		var parent LaneBlockHeaderHash
		for offset := range 2 - laneIndex {
			i := len(blocks)
			payload, err := (PayloadBuilder{CreatedAt: time.Time{}, TotalGasWanted: uint64(i + 1), Txs: [][]byte{{byte(i), 0}, {byte(i), 1}}}).Build()
			require.NoError(t, err)
			block := NewLaneBlock(lane, LaneHeight(offset), parent, payload)
			blocks = append(blocks, block)
			headers = append(headers, block.Header())
			parent = block.Header().Hash()
		}
		votes := make([]*Signed[*LaneVote], len(keys))
		for i, key := range keys {
			votes[i] = Sign(key, NewLaneVote(ep.EpochIndex(), headers[len(headers)-1]))
		}
		laneQCs[lane] = NewLaneQC(votes)
	}
	qc := BuildCommitQC(ep, keys, utils.None[*CommitQC](), laneQCs)
	fullQC := NewFullCommitQC(qc, headers)
	require.NoError(t, fullQC.Verify(ep))
	block := NewRoadBlock(fullQC, blocks).SuperBlock()
	require.Equal(t, qc.Height(), block.Height)
	require.Equal(t, qc.Header().Timestamp(), block.Timestamp)
	require.Equal(t, uint64(6), block.GasWanted)
	require.Equal(t, [][]byte{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {2, 0}, {2, 1}}, block.Txs)
}

func TestSuperBlockNamesTheParentOfItsRoadBlock(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 3)
	ep := GenEpochWithCommittee(rng, committee)
	for _, parent := range []utils.Option[RoadBlockHeaderHash]{utils.None[RoadBlockHeaderHash](), utils.Some(GenRoadBlockHeaderHash(rng))} {
		proposal, blocks := proposalAtBlocks(ep, View{Height: ep.RoadRange().First}, 1, parent)
		prepareQC := BuildPrepareQC(keys, proposal)
		var votes []*Signed[*CommitVote]
		for _, key := range keys {
			votes = append(votes, Sign(key, NewCommitVote(prepareQC)))
		}
		qc := NewFullCommitQC(NewCommitQC(votes), []*LaneBlockHeader{blocks[0].Header()})
		require.Equal(t, parent, NewRoadBlock(qc, blocks).SuperBlock().ParentHash)
	}
}

func TestLaneRangeVerifyRejectsHashOnEmptyRange(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 4)
	lane := committee.Lanes().At(0)

	require.Error(t, (&LaneRange{lane: lane, first: 0, next: 0, lastHash: GenLaneBlockHeaderHash(rng)}).Verify(committee))
	require.NoError(t, (&LaneRange{lane: lane, first: 0, next: 0}).Verify(committee))
	// Empty ranges have a zero hash at every height.
	require.Error(t, (&LaneRange{
		lane: lane, first: 1, next: 1, lastHash: GenLaneBlockHeaderHash(rng),
	}).Verify(committee))
}

func TestRoadBlockHeaderParentHashRoundTrips(t *testing.T) {
	rng := utils.TestRng()
	for _, parentHash := range []utils.Option[RoadBlockHeaderHash]{
		utils.None[RoadBlockHeaderHash](),
		utils.Some(GenRoadBlockHeaderHash(rng)),
	} {
		h := newRoadBlockHeader(GenEpochIndex(rng), RoadHeight(rng.Uint64()), parentHash, utils.GenTimestamp(rng), utils.GenSlice(rng, GenLaneRange))
		got, err := RoadBlockHeaderConv.Decode(RoadBlockHeaderConv.Encode(h))
		require.NoError(t, err)
		require.Equal(t, parentHash, got.ParentHash())
		require.Equal(t, h.Hash(), got.Hash())
	}
}

func TestRoadBlockHeaderVerifyParent(t *testing.T) {
	rng := utils.TestRng()
	parent := newRoadBlockHeader(GenEpochIndex(rng), 1, utils.None[RoadBlockHeaderHash](), utils.GenTimestamp(rng), nil)
	other := newRoadBlockHeader(GenEpochIndex(rng), 2, utils.None[RoadBlockHeaderHash](), utils.GenTimestamp(rng), nil)
	headerWith := func(parentHash utils.Option[RoadBlockHeaderHash]) *RoadBlockHeader {
		return newRoadBlockHeader(GenEpochIndex(rng), 3, parentHash, utils.GenTimestamp(rng), nil)
	}
	for name, tc := range map[string]struct {
		header *RoadBlockHeader
		parent utils.Option[*RoadBlockHeader]
		ok     bool
	}{
		"no parent expected, none named": {headerWith(utils.None[RoadBlockHeaderHash]()), utils.None[*RoadBlockHeader](), true},
		"parent expected, same named":    {headerWith(utils.Some(parent.Hash())), utils.Some(parent), true},
		"parent expected, other named":   {headerWith(utils.Some(other.Hash())), utils.Some(parent), false},
		"parent expected, none named":    {headerWith(utils.None[RoadBlockHeaderHash]()), utils.Some(parent), false},
		"no parent expected, one named":  {headerWith(utils.Some(parent.Hash())), utils.None[*RoadBlockHeader](), false},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.header.VerifyParent(tc.parent)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrParentMismatch)
			}
		})
	}
}

// A header with no parent omits field 6 from its canonical encoding.
func TestRoadBlockHeaderWithoutParentOmitsTheField(t *testing.T) {
	rng := utils.TestRng()
	fields := func(parentHash utils.Option[RoadBlockHeaderHash]) map[protowire.Number]bool {
		h := newRoadBlockHeader(GenEpochIndex(rng), RoadHeight(rng.Uint64()), parentHash, utils.GenTimestamp(rng), nil)
		b := hashable.MarshalCanonical(RoadBlockHeaderConv.Encode(h))
		got := map[protowire.Number]bool{}
		for len(b) > 0 {
			num, typ, n := protowire.ConsumeTag(b)
			require.GreaterOrEqual(t, n, 0)
			b = b[n:]
			n = protowire.ConsumeFieldValue(num, typ, b)
			require.GreaterOrEqual(t, n, 0)
			b = b[n:]
			got[num] = true
		}
		return got
	}
	require.False(t, fields(utils.None[RoadBlockHeaderHash]())[6])
	require.True(t, fields(utils.Some(GenRoadBlockHeaderHash(rng)))[6])
}

// A present parent hash of length 0 or 31 passes the unmarshal and fails the header decoder.
func TestRoadBlockHeaderDecodeRejectsAParentHashOfTheWrongLength(t *testing.T) {
	rng := utils.TestRng()
	h := newRoadBlockHeader(GenEpochIndex(rng), 2, utils.Some(GenRoadBlockHeaderHash(rng)), utils.GenTimestamp(rng), nil)
	for _, n := range []int{0, 31} {
		m := RoadBlockHeaderConv.Encode(h)
		m.ParentHash = utils.GenBytes(rng, n)
		decoded, err := protoutils.Unmarshal[*pb.RoadBlockHeader](protoutils.Marshal(m))
		require.NoError(t, err)
		_, err = RoadBlockHeaderConv.Decode(decoded)
		require.Error(t, err)
	}
}

// A 33 byte value fails the wireguard size check of the unmarshal, before the header decoder runs.
func TestRoadBlockHeaderUnmarshalRejectsAParentHashLongerThanTheHash(t *testing.T) {
	rng := utils.TestRng()
	h := newRoadBlockHeader(GenEpochIndex(rng), 2, utils.Some(GenRoadBlockHeaderHash(rng)), utils.GenTimestamp(rng), nil)
	m := RoadBlockHeaderConv.Encode(h)
	m.ParentHash = utils.GenBytes(rng, 33)
	_, err := protoutils.Unmarshal[*pb.RoadBlockHeader](protoutils.Marshal(m))
	require.Error(t, err)
}
