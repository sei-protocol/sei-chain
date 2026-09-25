package metrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestObserveTimeoutAndCommit(t *testing.T) {
	rng := utils.TestRng()
	a := types.GenSecretKey(rng).Public()
	b := types.GenSecretKey(rng).Public()

	timeoutsA := Timeouts(a)
	timeoutsB := Timeouts(b)
	commitsA := Commits(a)

	ObserveTimeout(a)
	ObserveTimeout(a)
	ObserveTimeout(b)
	ObserveCommit(a)

	require.Equal(t, timeoutsA+2, Timeouts(a))
	require.Equal(t, timeoutsB+1, Timeouts(b))
	require.Equal(t, commitsA+1, Commits(a))

	votesProposal := TimeoutVotes(a, PhaseNoProposal)
	votesCommit := TimeoutVotes(a, PhaseNoCommit)
	ObserveTimeoutVote(a, PhaseNoProposal)
	ObserveTimeoutVote(a, PhaseNoCommit)
	require.Equal(t, votesProposal+1, TimeoutVotes(a, PhaseNoProposal))
	require.Equal(t, votesCommit+1, TimeoutVotes(a, PhaseNoCommit))

	prepare := VotesIngested(VotePrepare)
	ObserveVoteIngested(VotePrepare)
	ObserveVoteIngested(VotePrepare)
	require.Equal(t, prepare+2, VotesIngested(VotePrepare))
}

func TestSetView(t *testing.T) {
	SetView(types.View{Index: 4, Number: 2})
	require.Equal(t, int64(2), gaugeValue(Global.viewNumberAt()))
}

func gaugeValue(g *prometheus.GaugeInt) int64 {
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		return 0
	}
	return int64(m.GetGauge().GetValue())
}
