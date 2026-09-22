package consensus

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type voteSet[V any] struct {
	weight uint64
	votes  map[types.PublicKey]V
}

type voteEntry[V any, B comparable] struct {
	vote   V
	view   types.View
	bucket B
}

type voteAggregator[V any, B comparable] struct {
	byKey    map[types.PublicKey]voteEntry[V, B]
	byBucket map[B]*voteSet[V]
}

func newVoteAggregator[V any, B comparable]() *voteAggregator[V, B] {
	return &voteAggregator[V, B]{
		byKey:    map[types.PublicKey]voteEntry[V, B]{},
		byBucket: map[B]*voteSet[V]{},
	}
}

func (a *voteAggregator[V, B]) pushVote(
	c *types.Committee,
	key types.PublicKey,
	view types.View,
	bucket B,
	vote V,
	quorum uint64,
) utils.Option[[]V] {
	// Check if the key has already voted.
	if old, ok := a.byKey[key]; ok {
		if !old.view.Less(view) {
			return utils.None[[]V]() // Ignore older or equal votes.
		}
		// Prune the old vote.
		oldSet := a.byBucket[old.bucket]
		oldSet.weight -= c.Weight(key)
		delete(oldSet.votes, key)
		if len(oldSet.votes) == 0 {
			delete(a.byBucket, old.bucket)
		}
	}

	// Insert the new vote.
	a.byKey[key] = voteEntry[V, B]{vote: vote, view: view, bucket: bucket}
	if _, ok := a.byBucket[bucket]; !ok {
		a.byBucket[bucket] = &voteSet[V]{votes: map[types.PublicKey]V{}}
	}
	set := a.byBucket[bucket]
	set.weight += c.Weight(key)
	set.votes[key] = vote

	// Check if we have enough votes for a QC.
	if set.weight < quorum {
		return utils.None[[]V]()
	}

	votes := make([]V, 0, len(set.votes))
	for _, vote := range set.votes {
		votes = append(votes, vote)
	}
	return utils.Some(votes)
}
