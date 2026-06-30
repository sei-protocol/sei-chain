package types

import (
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestNewCommittee_FiltersOutZeroWeightValidators(t *testing.T) {
	rng := utils.TestRng()
	zeroWeightKey := GenPublicKey(rng)
	nonZeroWeightKey := GenPublicKey(rng)

	committee, err := NewCommittee(map[PublicKey]uint64{
		zeroWeightKey:    0,
		nonZeroWeightKey: 7,
	})
	if err != nil {
		t.Fatalf("NewCommittee(): %v", err)
	}

	if committee.HasReplica(zeroWeightKey) {
		t.Fatal("HasReplica() = true for zero-weight validator, want false")
	}
	if got := committee.Replicas().Len(); got != 1 {
		t.Fatalf("Replicas().Len() = %v, want 1", got)
	}
	if got := committee.Replicas().At(0); got != nonZeroWeightKey {
		t.Fatalf("Replicas().At(0) = %v, want %v", got, nonZeroWeightKey)
	}
	if got := committee.Weight(nonZeroWeightKey); got != 7 {
		t.Fatalf("Weight() = %v, want 7", got)
	}
}

func TestNewCommittee_RejectsZeroTotalWeight(t *testing.T) {
	rng := utils.TestRng()

	_, err := NewCommittee(map[PublicKey]uint64{
		GenPublicKey(rng): 0,
		GenPublicKey(rng): 0,
	})
	if err == nil {
		t.Fatal("NewCommittee() succeeded, want error")
	}
}

func TestNewCommittee_RejectsWeightOverflow(t *testing.T) {
	rng := utils.TestRng()

	_, err := NewCommittee(map[PublicKey]uint64{
		GenPublicKey(rng): math.MaxUint64,
		GenPublicKey(rng): 1,
	})
	if err == nil {
		t.Fatal("NewCommittee() succeeded, want error")
	}
}

func TestNewCommittee_RejectsTooManyValidators(t *testing.T) {
	rng := utils.TestRng()

	weights := map[PublicKey]uint64{}
	for range MaxValidators + 1 {
		weights[GenPublicKey(rng)] = 1
	}

	_, err := NewCommittee(weights)
	if err == nil {
		t.Fatal("NewCommittee() succeeded, want error")
	}
}

func makeEpoch(rng utils.Rng) (*Epoch, []SecretKey) {
	keys := []SecretKey{
		TestSecretKey("heavy"),
		TestSecretKey("light1"),
		TestSecretKey("light2"),
	}
	committee := utils.OrPanic1(NewCommittee(map[PublicKey]uint64{
		keys[0].Public(): 5,
		keys[1].Public(): 1,
		keys[2].Public(): 1,
	}))
	return GenEpochWithCommittee(rng, committee), keys
}

func TestLaneQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	vote := NewLaneVote(NewBlock(keys[0].Public(), 0, GenBlockHeaderHash(rng), GenPayload(rng)).Header())

	heavyOnly := NewLaneQC([]*Signed[*LaneVote]{
		Sign(keys[0], vote),
	})
	require.NoError(t, heavyOnly.Verify(ep.Committee()))
	lightMajority := NewLaneQC([]*Signed[*LaneVote]{
		Sign(keys[1], vote),
		Sign(keys[2], vote),
	})
	require.Error(t, lightMajority.Verify(ep.Committee()))
}

func TestPrepareQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	vote := NewPrepareVote(ProposalAt(ep, View{}))

	heavyOnly := NewPrepareQC([]*Signed[*PrepareVote]{
		Sign(keys[0], vote),
	})
	require.NoError(t, heavyOnly.Verify(ep))
	lightMajority := NewPrepareQC([]*Signed[*PrepareVote]{
		Sign(keys[1], vote),
		Sign(keys[2], vote),
	})
	require.Error(t, lightMajority.Verify(ep))
}

func TestCommitQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	vote := NewCommitVote(ProposalAt(ep, View{}))

	heavyOnly := NewCommitQC([]*Signed[*CommitVote]{
		Sign(keys[0], vote),
	})
	require.NoError(t, heavyOnly.Verify(ep))
	lightMajority := NewCommitQC([]*Signed[*CommitVote]{
		Sign(keys[1], vote),
		Sign(keys[2], vote),
	})
	require.Error(t, lightMajority.Verify(ep))
}

func TestAppQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	vote := NewAppVote(NewAppProposal(0, 0, GenAppHash(rng), 0))

	heavyOnly := NewAppQC([]*Signed[*AppVote]{
		Sign(keys[0], vote),
	})
	require.NoError(t, heavyOnly.Verify(ep.Committee()))

	lightMajority := NewAppQC([]*Signed[*AppVote]{
		Sign(keys[1], vote),
		Sign(keys[2], vote),
	})
	require.Error(t, lightMajority.Verify(ep.Committee()))
}

func TestTimeoutQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	view := View{}

	heavyOnly := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[0], view, utils.None[*PrepareQC](), ep.EpochIndex()),
	})
	if err := heavyOnly.Verify(ep, utils.None[*CommitQC]()); err != nil {
		t.Fatalf("heavyOnly.Verify(): %v", err)
	}

	lightMajority := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[1], view, utils.None[*PrepareQC](), ep.EpochIndex()),
		NewFullTimeoutVote(keys[2], view, utils.None[*PrepareQC](), ep.EpochIndex()),
	})
	if err := lightMajority.Verify(ep, utils.None[*CommitQC]()); err == nil {
		t.Fatal("lightMajority.Verify() succeeded, want error")
	}
}

func TestNewCommittee_RejectsEmptyWeights(t *testing.T) {
	_, err := NewCommittee(map[PublicKey]uint64{})
	if err == nil {
		t.Fatal("NewCommittee() succeeded with empty weights, want error")
	}
}
