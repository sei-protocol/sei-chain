package consensus

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// timeoutEnv is a timeoutVotes aggregator over a 4-replica equal-weight committee.
type timeoutEnv struct {
	tv     *timeoutVotes
	keys   []types.SecretKey
	ep     *types.Epoch
	view   types.View
	quorum []types.SecretKey
}

func newTimeoutEnv(rng utils.Rng) timeoutEnv {
	keys := utils.GenSliceN(rng, 4, types.GenSecretKey)
	weights := make(map[types.PublicKey]uint64, len(keys))
	for _, k := range keys {
		weights[k.Public()] = 1
	}
	c := utils.OrPanic1(types.NewCommittee(weights))
	ep := types.NewEpoch(0, types.OpenRoadRange(), time.Time{}, c, 0)
	view := types.View{Index: 0, Number: 0, EpochIndex: ep.EpochIndex()}
	quorum := types.TestKeysWithWeight(c, keys, c.TimeoutQuorum())
	return timeoutEnv{
		tv:     newTimeoutVotes(),
		keys:   keys,
		ep:     ep,
		view:   view,
		quorum: quorum,
	}
}

func TestTimeoutVotes_BelowQuorumDoesNotFormQC(t *testing.T) {
	e := newTimeoutEnv(utils.TestRng())
	c := e.ep.Committee()

	e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(e.keys[0], e.view, utils.None[*types.PrepareQC]()))
	require.False(t, e.tv.qc.Load().IsPresent())
}

func TestTimeoutVotes_QuorumFormsQC(t *testing.T) {
	rng := utils.TestRng()
	e := newTimeoutEnv(rng)
	c := e.ep.Committee()
	pqc := makePrepareQC(e.keys, types.GenProposalForEpoch(rng, e.ep, e.view))

	for _, k := range e.quorum {
		e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(k, e.view, utils.Some(pqc)))
	}
	got, ok := e.tv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, e.view, got.View())
	require.NoError(t, got.Verify(e.ep, utils.None[*types.CommitQC]()))
	require.True(t, got.LatestPrepareQC().IsPresent())
}

func TestTimeoutVotes_SameViewAfterQCDoesNotChangeView(t *testing.T) {
	e := newTimeoutEnv(utils.TestRng())
	c := e.ep.Committee()

	for _, k := range e.quorum {
		e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(k, e.view, utils.None[*types.PrepareQC]()))
	}
	require.True(t, e.tv.qc.Load().IsPresent())
	e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(e.keys[3], e.view, utils.None[*types.PrepareQC]()))
	got, ok := e.tv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, e.view, got.View())
}

func TestTimeoutVotes_IgnoresStaleVoteFromSameKey(t *testing.T) {
	e := newTimeoutEnv(utils.TestRng())
	c := e.ep.Committee()
	view1 := e.view.Next()

	for _, k := range e.quorum {
		e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(k, view1, utils.None[*types.PrepareQC]()))
	}
	got, ok := e.tv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, view1, got.View())

	e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(e.quorum[0], e.view, utils.None[*types.PrepareQC]()))
	got, ok = e.tv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, view1, got.View())
}

func TestTimeoutVotes_ReplacesOlderVoteAndAdvancesQC(t *testing.T) {
	e := newTimeoutEnv(utils.TestRng())
	c := e.ep.Committee()
	view1 := e.view.Next()

	for _, k := range e.quorum {
		e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(k, e.view, utils.None[*types.PrepareQC]()))
	}
	got, ok := e.tv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, e.view, got.View())

	e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(e.quorum[0], view1, utils.None[*types.PrepareQC]()))
	got, ok = e.tv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, e.view, got.View())

	for _, k := range e.quorum[1:] {
		e.tv.pushVerifiedVote(c, types.NewFullTimeoutVote(k, view1, utils.None[*types.PrepareQC]()))
	}
	got, ok = e.tv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, view1, got.View())
}
