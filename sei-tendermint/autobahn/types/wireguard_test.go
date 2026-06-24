package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func maxValidatorCommittee(t *testing.T) (*Committee, []SecretKey) {
	t.Helper()
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, MaxValidators)
	require.Len(t, keys, MaxValidators)
	return committee, keys
}

func secretKeyFor(keys []SecretKey, want PublicKey) SecretKey {
	for _, key := range keys {
		if key.Public() == want {
			return key
		}
	}
	panic("missing key")
}

func TestPayloadWireguardAcceptsSingleMaxSizedTx(t *testing.T) {
	payload, err := PayloadBuilder{
		CreatedAt:         time.Unix(1, 2),
		TotalGasWanted:    1,
		TotalGasEstimated: 1,
		Txs:               [][]byte{make([]byte, int(MaxTxsBytesPerBlock))},
	}.Build()
	require.NoError(t, err)

	raw := protoutils.Marshal(PayloadConv.Encode(payload))
	decoded, err := protoutils.Unmarshal[*pb.Payload](raw)
	require.NoError(t, err)
	require.Len(t, decoded.Txs, 1)
	require.Len(t, decoded.Txs[0], int(MaxTxsBytesPerBlock))
}

func TestPayloadWireguardAcceptsMaxCountRegularTxs(t *testing.T) {
	regularTxSize := int(MaxTxsBytesPerBlock / MaxTxsPerBlock)
	txs := make([][]byte, int(MaxTxsPerBlock))
	for i := range txs {
		txs[i] = make([]byte, regularTxSize)
	}
	payload, err := PayloadBuilder{
		CreatedAt:         time.Unix(1, 2),
		TotalGasWanted:    1,
		TotalGasEstimated: 1,
		Txs:               txs,
	}.Build()
	require.NoError(t, err)

	raw := protoutils.Marshal(PayloadConv.Encode(payload))
	decoded, err := protoutils.Unmarshal[*pb.Payload](raw)
	require.NoError(t, err)
	require.Len(t, decoded.Txs, int(MaxTxsPerBlock))
	for _, tx := range decoded.Txs {
		require.Len(t, tx, regularTxSize)
	}
}

func TestPayloadWireguardRejectsTooManyTxs(t *testing.T) {
	txs := make([][]byte, int(MaxTxsPerBlock)+1)
	raw := protoutils.Marshal(&pb.Payload{
		CreatedAt:         TimeConv.Encode(time.Unix(1, 2)),
		TotalGasWanted:    utils.Alloc(uint64(1)),
		TotalGasEstimated: utils.Alloc(uint64(1)),
		Txs:               txs,
	})
	_, err := protoutils.Unmarshal[*pb.Payload](raw)
	require.Error(t, err)
}

func TestLaneQCWireguardAcceptsMaxValidators(t *testing.T) {
	committee, keys := maxValidatorCommittee(t)
	rng := utils.TestRng()
	lane := committee.Leader(View{})
	vote := NewLaneVote(NewBlock(lane, 0, GenBlockHeaderHash(rng), GenPayload(rng)).Header())
	votes := make([]*Signed[*LaneVote], len(keys))
	for i, key := range keys {
		votes[i] = Sign(key, vote)
	}
	qc := NewLaneQC(votes)

	raw := protoutils.Marshal(LaneQCConv.Encode(qc))
	decoded, err := protoutils.Unmarshal[*pb.LaneQC](raw)
	require.NoError(t, err)
	require.Len(t, decoded.Sigs, MaxValidators)
}

func TestPrepareQCWireguardAcceptsMaxValidators(t *testing.T) {
	_, keys := maxValidatorCommittee(t)
	rng := utils.TestRng()
	vote := NewPrepareVote(GenProposalAt(rng, View{}))
	votes := make([]*Signed[*PrepareVote], len(keys))
	for i, key := range keys {
		votes[i] = Sign(key, vote)
	}
	qc := NewPrepareQC(votes)

	raw := protoutils.Marshal(PrepareQCConv.Encode(qc))
	decoded, err := protoutils.Unmarshal[*pb.PrepareQC](raw)
	require.NoError(t, err)
	require.Len(t, decoded.Sigs, MaxValidators)
}

func TestCommitQCWireguardAcceptsMaxValidators(t *testing.T) {
	_, keys := maxValidatorCommittee(t)
	rng := utils.TestRng()
	vote := NewCommitVote(GenProposalAt(rng, View{}))
	votes := make([]*Signed[*CommitVote], len(keys))
	for i, key := range keys {
		votes[i] = Sign(key, vote)
	}
	qc := NewCommitQC(votes)

	raw := protoutils.Marshal(CommitQCConv.Encode(qc))
	decoded, err := protoutils.Unmarshal[*pb.CommitQC](raw)
	require.NoError(t, err)
	require.Len(t, decoded.Sigs, MaxValidators)
}

func TestFullCommitQCWireguardAcceptsMaxValidatorsAndHeaders(t *testing.T) {
	committee, keys := maxValidatorCommittee(t)
	rng := utils.TestRng()
	laneRanges := make([]*LaneRange, 0, MaxValidators)
	headers := make([]*BlockHeader, 0, MaxValidators*MaxLaneRangeInProposal)
	for lane := range committee.Lanes().All() {
		parentHash := GenBlockHeaderHash(rng)
		var lastHeader *BlockHeader
		for blockNumber := range BlockNumber(MaxLaneRangeInProposal) {
			lastHeader = NewBlock(lane, blockNumber, parentHash, GenPayload(rng)).Header()
			headers = append(headers, lastHeader)
			parentHash = lastHeader.Hash()
		}
		laneRanges = append(laneRanges, NewLaneRange(lane, 0, utils.Some(lastHeader)))
	}
	proposal := newProposal(View{}, time.Unix(1, 2), laneRanges, utils.None[*AppProposal]())
	vote := NewCommitVote(proposal)
	votes := make([]*Signed[*CommitVote], len(keys))
	for i, key := range keys {
		votes[i] = Sign(key, vote)
	}
	qc := NewFullCommitQC(NewCommitQC(votes), headers)

	raw := protoutils.Marshal(FullCommitQCConv.Encode(qc))
	_, err := protoutils.Unmarshal[*pb.FullCommitQC](raw)
	require.NoError(t, err)
}

func TestAppQCWireguardAcceptsMaxValidators(t *testing.T) {
	_, keys := maxValidatorCommittee(t)
	rng := utils.TestRng()
	vote := NewAppVote(GenAppProposal(rng))
	votes := make([]*Signed[*AppVote], len(keys))
	for i, key := range keys {
		votes[i] = Sign(key, vote)
	}
	qc := NewAppQC(votes)

	raw := protoutils.Marshal(AppQCConv.Encode(qc))
	decoded, err := protoutils.Unmarshal[*pb.AppQC](raw)
	require.NoError(t, err)
	require.Len(t, decoded.Sigs, MaxValidators)
}

func TestTimeoutQCWireguardAcceptsMaxValidators(t *testing.T) {
	_, keys := maxValidatorCommittee(t)
	votes := make([]*FullTimeoutVote, len(keys))
	for i, key := range keys {
		votes[i] = NewFullTimeoutVote(key, View{}, utils.None[*PrepareQC]())
	}
	qc := NewTimeoutQC(votes)

	raw := protoutils.Marshal(TimeoutQCConv.Encode(qc))
	decoded, err := protoutils.Unmarshal[*pb.TimeoutQC](raw)
	require.NoError(t, err)
	require.Len(t, decoded.VotesV2, MaxValidators)
}

func TestFullProposalWireguardAcceptsMaxValidators(t *testing.T) {
	committee, keys := maxValidatorCommittee(t)
	rng := utils.TestRng()
	laneQCs := map[LaneID]*LaneQC{}
	for lane := range committee.Lanes().All() {
		key := secretKeyFor(keys, lane)
		vote := NewLaneVote(NewBlock(lane, 0, GenBlockHeaderHash(rng), GenPayload(rng)).Header())
		laneQCs[lane] = NewLaneQC([]*Signed[*LaneVote]{Sign(key, vote)})
	}
	proposal, err := NewProposal(
		secretKeyFor(keys, committee.Leader(View{})),
		committee,
		ViewSpec{},
		0,
		time.Time{},
		time.Unix(1, 2),
		laneQCs,
		utils.None[*AppQC](),
	)
	require.NoError(t, err)

	raw := protoutils.Marshal(FullProposalConv.Encode(proposal))
	decoded, err := protoutils.Unmarshal[*pb.FullProposal](raw)
	require.NoError(t, err)
	require.Len(t, decoded.LaneQcs, MaxValidators)
}
