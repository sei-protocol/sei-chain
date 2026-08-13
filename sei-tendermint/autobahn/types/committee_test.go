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
	if !committee.HasLane(committee.Lane(nonZeroWeightKey).OrPanic("member")) {
		t.Fatal("HasLane(nonZero@e0) = false, want true")
	}
	if got := committee.Lanes().Len(); got != 1 {
		t.Fatalf("Lanes().Len() = %v, want 1", got)
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
	lane := ep.Committee().Lane(keys[0].Public()).OrPanic("keys[0]")
	vote := NewLaneVote(NewBlock(lane, 0, GenBlockHeaderHash(rng), GenPayload(rng)).Header())

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
	vote := NewPrepareVote(ProposalAt(ep, View{EpochIndex: ep.EpochIndex(), Index: ep.RoadRange().First}))

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

func TestPrepareQCVerifyChecksEpochBinding(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	sign := func(p *Proposal) *PrepareQC {
		return NewPrepareQC([]*Signed[*PrepareVote]{Sign(keys[0], NewPrepareVote(p))})
	}

	require.NoError(t, sign(ProposalAt(ep, View{Index: ep.RoadRange().First})).Verify(ep))

	wrongEpoch := newProposal(View{Index: ep.RoadRange().First, EpochIndex: ep.EpochIndex() + 1}, time.Time{}, nil, ep.FirstBlock())
	require.Error(t, sign(wrongEpoch).Verify(ep))

	outOfRoads := newProposal(View{Index: ep.RoadRange().Next, EpochIndex: ep.EpochIndex()}, time.Time{}, nil, ep.FirstBlock())
	require.Error(t, sign(outOfRoads).Verify(ep))
}

func TestCommitQCVerifyChecksEpochBinding(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	sign := func(p *Proposal) *CommitQC {
		return NewCommitQC([]*Signed[*CommitVote]{Sign(keys[0], NewCommitVote(p))})
	}

	require.NoError(t, sign(ProposalAt(ep, View{Index: ep.RoadRange().First})).Verify(ep))

	wrongEpoch := newProposal(View{Index: ep.RoadRange().First, EpochIndex: ep.EpochIndex() + 1}, time.Time{}, nil, ep.FirstBlock())
	require.Error(t, sign(wrongEpoch).Verify(ep))

	outOfRoads := newProposal(View{Index: ep.RoadRange().Next, EpochIndex: ep.EpochIndex()}, time.Time{}, nil, ep.FirstBlock())
	require.Error(t, sign(outOfRoads).Verify(ep))
}

func TestCommitQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	vote := NewCommitVote(ProposalAt(ep, View{EpochIndex: ep.EpochIndex(), Index: ep.RoadRange().First}))

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
	vote := NewAppVote(NewAppProposal(ProposalAt(ep, View{EpochIndex: ep.EpochIndex(), Index: ep.RoadRange().First}), GenAppHash(rng)))

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

func TestTimeoutQCVerifyChecksEpochBinding(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	prev := utils.Some(CommitQCAt(ep, keys))
	view := View{EpochIndex: ep.EpochIndex(), Index: ep.RoadRange().First + 1}

	correct := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[0], view, utils.None[*PrepareQC]()),
	})
	require.NoError(t, correct.Verify(ep, prev))

	wrongEpoch := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[0], View{EpochIndex: ep.EpochIndex() + 1, Index: view.Index}, utils.None[*PrepareQC]()),
	})
	require.Error(t, wrongEpoch.Verify(ep, prev))
}

func TestTimeoutQCVerifyChecksWeight(t *testing.T) {
	rng := utils.TestRng()
	ep, keys := makeEpoch(rng)
	prev := utils.Some(CommitQCAt(ep, keys))
	view := View{EpochIndex: ep.EpochIndex(), Index: ep.RoadRange().First + 1}

	heavyOnly := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[0], view, utils.None[*PrepareQC]()),
	})
	require.NoError(t, heavyOnly.Verify(ep, prev))

	lightMajority := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[1], view, utils.None[*PrepareQC]()),
		NewFullTimeoutVote(keys[2], view, utils.None[*PrepareQC]()),
	})
	require.Error(t, lightMajority.Verify(ep, prev))
}

func TestNewCommittee_RejectsEmptyWeights(t *testing.T) {
	_, err := NewCommittee(map[PublicKey]uint64{})
	if err == nil {
		t.Fatal("NewCommittee() succeeded with empty weights, want error")
	}
}

func TestDeriveNext_StayLeaveRejoin(t *testing.T) {
	rng := utils.TestRng()
	a := GenSecretKey(rng).Public()
	b := GenSecretKey(rng).Public()
	c := GenSecretKey(rng).Public()
	d := GenSecretKey(rng).Public()

	requireLanesSorted := func(t *testing.T, committee *Committee) {
		t.Helper()
		lanes := committee.Lanes()
		for i := 1; i < lanes.Len(); i++ {
			require.Less(t, lanes.At(i-1).Compare(lanes.At(i)), 0)
		}
	}

	// Epoch 0: A,B,D join.
	c0, err := NewCommittee(map[PublicKey]uint64{a: 1, b: 1, d: 1})
	require.NoError(t, err)
	require.Equal(t, LaneID{Validator: a, Joined: 0}, c0.Lane(a).OrPanic("a"))
	require.Equal(t, LaneID{Validator: b, Joined: 0}, c0.Lane(b).OrPanic("b"))
	require.Equal(t, LaneID{Validator: d, Joined: 0}, c0.Lane(d).OrPanic("d"))
	require.False(t, c0.HasLane(LaneID{Validator: c, Joined: 0}))
	requireLanesSorted(t, c0)

	// Epoch 1: A, B, D remain; Joined stays 0.
	c1, err := c0.DeriveNext(map[PublicKey]uint64{a: 1, b: 1, d: 1}, 1)
	require.NoError(t, err)
	require.Equal(t, LaneID{Validator: a, Joined: 0}, c1.Lane(a).OrPanic("a"))
	require.Equal(t, LaneID{Validator: b, Joined: 0}, c1.Lane(b).OrPanic("b"))
	require.Equal(t, LaneID{Validator: d, Joined: 0}, c1.Lane(d).OrPanic("d"))
	requireLanesSorted(t, c1)

	// Epoch 2: B and D leave; C joins with Joined=2; A remains.
	c2, err := c1.DeriveNext(map[PublicKey]uint64{a: 1, c: 1}, 2)
	require.NoError(t, err)
	require.Equal(t, LaneID{Validator: a, Joined: 0}, c2.Lane(a).OrPanic("a"))
	require.Equal(t, LaneID{Validator: c, Joined: 2}, c2.Lane(c).OrPanic("c"))
	require.False(t, c2.HasLane(LaneID{Validator: b, Joined: 0}))
	require.False(t, c2.HasLane(LaneID{Validator: d, Joined: 0}))
	require.False(t, c2.HasReplica(b))
	requireLanesSorted(t, c2)

	// Epoch 3: D rejoins with Joined=3; C and A remain.
	c3, err := c2.DeriveNext(map[PublicKey]uint64{a: 1, c: 1, d: 1}, 3)
	require.NoError(t, err)
	require.Equal(t, LaneID{Validator: a, Joined: 0}, c3.Lane(a).OrPanic("a"))
	require.Equal(t, LaneID{Validator: c, Joined: 2}, c3.Lane(c).OrPanic("c"))
	require.Equal(t, LaneID{Validator: d, Joined: 3}, c3.Lane(d).OrPanic("d"))
	require.False(t, c3.HasLane(LaneID{Validator: d, Joined: 0}))
	requireLanesSorted(t, c3)
}
