package consensus

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type voteTestEnv struct {
	keys   []types.SecretKey
	ep     *types.Epoch
	view   types.View
	quorum []types.SecretKey
}

func newVoteTestEnv(rng utils.Rng) voteTestEnv {
	keys := utils.GenSliceN(rng, 4, types.GenSecretKey)
	weights := make(map[types.PublicKey]uint64, len(keys))
	for _, k := range keys {
		weights[k.Public()] = 1
	}
	c := utils.OrPanic1(types.NewCommittee(weights))
	ep := types.NewEpoch(0, types.OpenRoadRange(), time.Time{}, c, 0)
	return voteTestEnv{
		keys:   keys,
		ep:     ep,
		view:   types.View{Index: 0, Number: 0, EpochIndex: ep.EpochIndex()},
		quorum: types.TestKeysWithWeight(c, keys, c.CommitQuorum()),
	}
}

// keyWeight is the total committee weight held by keys.
func keyWeight(c *types.Committee, keys []types.SecretKey) uint64 {
	total := uint64(0)
	for _, k := range keys {
		total += c.Weight(k.Public())
	}
	return total
}

// splitBelowQuorum divides the committee into two groups that each fall short of quorum
// but together exceed it, so a QC forming across both groups proves their votes shared a
// bucket rather than reaching quorum on their own.
func (e voteTestEnv) splitBelowQuorum(t *testing.T) (first, second []types.SecretKey) {
	c := e.ep.Committee()
	first, second = e.keys[:len(e.keys)/2], e.keys[len(e.keys)/2:]
	require.True(t, keyWeight(c, first) < c.CommitQuorum())
	require.True(t, keyWeight(c, second) < c.CommitQuorum())
	require.True(t, keyWeight(c, first)+keyWeight(c, second) >= c.CommitQuorum())
	return first, second
}

type testAggregatedVote struct {
	key    types.PublicKey
	view   types.View
	bucket int
}

type aggregationTestEnv struct {
	voteTestEnv
	votes *voteAggregator[*testAggregatedVote, int]
}

func newAggregationTestEnv(rng utils.Rng) aggregationTestEnv {
	return aggregationTestEnv{
		voteTestEnv: newVoteTestEnv(rng),
		votes:       newVoteAggregator[*testAggregatedVote, int](),
	}
}

func (e aggregationTestEnv) push(key types.SecretKey, view types.View, bucket int) utils.Option[[]*testAggregatedVote] {
	vote := &testAggregatedVote{key: key.Public(), view: view, bucket: bucket}
	return e.votes.pushVote(e.ep.Committee(), vote.key, vote.view, vote.bucket, vote, e.ep.Committee().CommitQuorum())
}

func TestVoteAggregator_BelowQuorumDoesNotEmit(t *testing.T) {
	e := newAggregationTestEnv(utils.TestRng())

	for _, k := range e.quorum[:len(e.quorum)-1] {
		require.False(t, e.push(k, e.view, 0).IsPresent())
	}
}

func TestVoteAggregator_QuorumEmitsVotes(t *testing.T) {
	e := newAggregationTestEnv(utils.TestRng())
	var got utils.Option[[]*testAggregatedVote]

	for _, k := range e.quorum {
		got = e.push(k, e.view, 0)
	}
	votes, ok := got.Get()
	require.True(t, ok)
	require.Len(t, votes, len(e.quorum))
}

func TestVoteAggregator_IgnoresEqualVoteFromSameKey(t *testing.T) {
	e := newAggregationTestEnv(utils.TestRng())

	e.push(e.quorum[0], e.view, 0)
	e.push(e.quorum[0], e.view, 1)
	var got utils.Option[[]*testAggregatedVote]
	for _, k := range e.quorum[1:] {
		got = e.push(k, e.view, 0)
	}
	votes, ok := got.Get()
	require.True(t, ok)
	require.Len(t, votes, len(e.quorum))
}

func TestVoteAggregator_IgnoresStaleVoteFromSameKey(t *testing.T) {
	e := newAggregationTestEnv(utils.TestRng())
	view1 := e.view.Next()

	e.push(e.quorum[0], view1, 1)
	e.push(e.quorum[0], e.view, 0)
	var got utils.Option[[]*testAggregatedVote]
	for _, k := range e.quorum[1:] {
		got = e.push(k, view1, 1)
	}
	votes, ok := got.Get()
	require.True(t, ok)
	require.Len(t, votes, len(e.quorum))
}

func TestVoteAggregator_ReplacesOlderVotesAndEmitsAtNewView(t *testing.T) {
	e := newAggregationTestEnv(utils.TestRng())
	view1 := e.view.Next()

	for _, k := range e.quorum {
		e.push(k, e.view, 0)
	}
	require.False(t, e.push(e.quorum[0], view1, 1).IsPresent())
	var got utils.Option[[]*testAggregatedVote]
	for _, k := range e.quorum[1:] {
		got = e.push(k, view1, 1)
	}
	votes, ok := got.Get()
	require.True(t, ok)
	require.Len(t, votes, len(e.quorum))
}
