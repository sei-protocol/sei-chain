package types

import (
	"math"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestNewCommittee_FiltersOutZeroWeightValidators(t *testing.T) {
	rng := utils.TestRng()
	firstBlock := GenGlobalBlockNumber(rng)
	genesisTimestamp := time.Now()
	zeroWeightKey := GenPublicKey(rng)
	nonZeroWeightKey := GenPublicKey(rng)

	committee, err := NewCommittee(map[PublicKey]uint64{
		zeroWeightKey:    0,
		nonZeroWeightKey: 7,
	}, firstBlock, genesisTimestamp)
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
	firstBlock := GenGlobalBlockNumber(rng)
	genesisTimestamp := time.Now()

	_, err := NewCommittee(map[PublicKey]uint64{
		GenPublicKey(rng): 0,
		GenPublicKey(rng): 0,
	}, firstBlock, genesisTimestamp)
	if err == nil {
		t.Fatal("NewCommittee() succeeded, want error")
	}
}

func TestNewCommittee_RejectsWeightOverflow(t *testing.T) {
	rng := utils.TestRng()
	firstBlock := GenGlobalBlockNumber(rng)
	genesisTimestamp := time.Now()

	_, err := NewCommittee(map[PublicKey]uint64{
		GenPublicKey(rng): math.MaxUint64,
		GenPublicKey(rng): 1,
	}, firstBlock, genesisTimestamp)
	if err == nil {
		t.Fatal("NewCommittee() succeeded, want error")
	}
}

func TestNewCommittee_RejectsTooManyValidators(t *testing.T) {
	rng := utils.TestRng()
	firstBlock := GenGlobalBlockNumber(rng)
	genesisTimestamp := time.Now()

	weights := map[PublicKey]uint64{}
	for range MaxValidators + 1 {
		weights[GenPublicKey(rng)] = 1
	}

	_, err := NewCommittee(weights, firstBlock, genesisTimestamp)
	if err == nil {
		t.Fatal("NewCommittee() succeeded, want error")
	}
}

func makeCommittee() (*Committee, []SecretKey) {
	keys := []SecretKey{
		TestSecretKey("heavy"),
		TestSecretKey("light1"),
		TestSecretKey("light2"),
	}
	return utils.OrPanic1(NewCommittee(map[PublicKey]uint64{
		keys[0].Public(): 5,
		keys[1].Public(): 1,
		keys[2].Public(): 1,
	}, 0, time.Now())), keys
}

func TestLaneQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := makeCommittee()
	vote := NewLaneVote(NewBlock(keys[0].Public(), 0, GenBlockHeaderHash(rng), GenPayload(rng)).Header())

	heavyOnly := NewLaneQC([]*Signed[*LaneVote]{
		Sign(keys[0], vote),
	})
	require.NoError(t, heavyOnly.Verify(committee))
	lightMajority := NewLaneQC([]*Signed[*LaneVote]{
		Sign(keys[1], vote),
		Sign(keys[2], vote),
	})
	require.Error(t, lightMajority.Verify(committee))
}

func TestPrepareQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := makeCommittee()
	vote := NewPrepareVote(GenProposalAt(rng, View{}))

	heavyOnly := NewPrepareQC([]*Signed[*PrepareVote]{
		Sign(keys[0], vote),
	})
	require.NoError(t, heavyOnly.Verify(committee))
	lightMajority := NewPrepareQC([]*Signed[*PrepareVote]{
		Sign(keys[1], vote),
		Sign(keys[2], vote),
	})
	require.Error(t, lightMajority.Verify(committee))
}

func TestCommitQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := makeCommittee()
	vote := NewCommitVote(GenProposalAt(rng, View{}))

	heavyOnly := NewCommitQC([]*Signed[*CommitVote]{
		Sign(keys[0], vote),
	})
	require.NoError(t, heavyOnly.Verify(committee))
	lightMajority := NewCommitQC([]*Signed[*CommitVote]{
		Sign(keys[1], vote),
		Sign(keys[2], vote),
	})
	require.Error(t, lightMajority.Verify(committee))
}

func TestAppQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := makeCommittee()
	vote := NewAppVote(NewAppProposal(0, 0, GenAppHash(rng)))

	heavyOnly := NewAppQC([]*Signed[*AppVote]{
		Sign(keys[0], vote),
	})
	require.NoError(t, heavyOnly.Verify(committee))

	lightMajority := NewAppQC([]*Signed[*AppVote]{
		Sign(keys[1], vote),
		Sign(keys[2], vote),
	})
	require.Error(t, lightMajority.Verify(committee))
}

func TestTimeoutQCVerifyChecksWeight(t *testing.T) {
	committee, keys := makeCommittee()
	view := View{}

	heavyOnly := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[0], view, utils.None[*PrepareQC]()),
	})
	if err := heavyOnly.Verify(committee, utils.None[*CommitQC]()); err != nil {
		t.Fatalf("heavyOnly.Verify(): %v", err)
	}

	lightMajority := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[1], view, utils.None[*PrepareQC]()),
		NewFullTimeoutVote(keys[2], view, utils.None[*PrepareQC]()),
	})
	if err := lightMajority.Verify(committee, utils.None[*CommitQC]()); err == nil {
		t.Fatal("lightMajority.Verify() succeeded, want error")
	}
}
