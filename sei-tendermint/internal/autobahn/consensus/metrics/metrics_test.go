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

	timeoutsA := counterValue(Global.timeoutsAt(leaderLabel(a)))
	timeoutsB := counterValue(Global.timeoutsAt(leaderLabel(b)))
	commitsA := counterValue(Global.commitsAt(leaderLabel(a)))

	ObserveTimeout(a)
	ObserveTimeout(a)
	ObserveTimeout(b)
	ObserveCommit(a)

	require.Equal(t, timeoutsA+2, counterValue(Global.timeoutsAt(leaderLabel(a))))
	require.Equal(t, timeoutsB+1, counterValue(Global.timeoutsAt(leaderLabel(b))))
	require.Equal(t, commitsA+1, counterValue(Global.commitsAt(leaderLabel(a))))

	votesProposal := counterValue(Global.timeoutVotesAt(leaderLabel(a), PhaseNoProposal.label))
	votesCommit := counterValue(Global.timeoutVotesAt(leaderLabel(a), PhaseNoCommit.label))
	ObserveTimeoutVote(a, PhaseNoProposal)
	ObserveTimeoutVote(a, PhaseNoCommit)
	require.Equal(t, votesProposal+1, counterValue(Global.timeoutVotesAt(leaderLabel(a), PhaseNoProposal.label)))
	require.Equal(t, votesCommit+1, counterValue(Global.timeoutVotesAt(leaderLabel(a), PhaseNoCommit.label)))

	prepare := counterValue(Global.votesIngestedAt(VotePrepare))
	ObserveVoteIngested(VotePrepare)
	ObserveVoteIngested(VotePrepare)
	require.Equal(t, prepare+2, counterValue(Global.votesIngestedAt(VotePrepare)))
}

func counterValue(c *prometheus.CounterInt) int64 {
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		return 0
	}
	return int64(m.GetCounter().GetValue())
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
