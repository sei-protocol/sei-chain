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

	m := Get()
	addr := func(k types.PublicKey) string { return k.ED25519().Address().String() }
	timeoutsA := counterValue(m.Timeouts.WithLabelValues(addr(a)))
	timeoutsB := counterValue(m.Timeouts.WithLabelValues(addr(b)))
	commitsA := counterValue(m.Commits.WithLabelValues(addr(a)))

	m.Timeouts.WithLabelValues(addr(a)).Add(1)
	m.Timeouts.WithLabelValues(addr(a)).Add(1)
	m.Timeouts.WithLabelValues(addr(b)).Add(1)
	m.Commits.WithLabelValues(addr(a)).Add(1)

	require.Equal(t, timeoutsA+2, counterValue(m.Timeouts.WithLabelValues(addr(a))))
	require.Equal(t, timeoutsB+1, counterValue(m.Timeouts.WithLabelValues(addr(b))))
	require.Equal(t, commitsA+1, counterValue(m.Commits.WithLabelValues(addr(a))))

	votesProposal := counterValue(m.TimeoutVotes.WithLabelValues(addr(a), PhaseNoProposal))
	votesCommit := counterValue(m.TimeoutVotes.WithLabelValues(addr(a), PhaseNoCommit))
	m.TimeoutVotes.WithLabelValues(addr(a), PhaseNoProposal).Add(1)
	m.TimeoutVotes.WithLabelValues(addr(a), PhaseNoCommit).Add(1)
	require.Equal(t, votesProposal+1, counterValue(m.TimeoutVotes.WithLabelValues(addr(a), PhaseNoProposal)))
	require.Equal(t, votesCommit+1, counterValue(m.TimeoutVotes.WithLabelValues(addr(a), PhaseNoCommit)))

	prepare := counterValue(m.VotesIngested.WithLabelValues(VotePrepare))
	m.VotesIngested.WithLabelValues(VotePrepare).Add(1)
	m.VotesIngested.WithLabelValues(VotePrepare).Add(1)
	require.Equal(t, prepare+2, counterValue(m.VotesIngested.WithLabelValues(VotePrepare)))
}

func counterValue(c *prometheus.CounterInt) int64 {
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		return 0
	}
	return int64(m.GetCounter().GetValue())
}

func TestSetView(t *testing.T) {
	Get().ViewNumber.Set(2)
	require.Equal(t, int64(2), gaugeValue(Get().ViewNumber))
}

func gaugeValue(g *prometheus.GaugeInt) int64 {
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		return 0
	}
	return int64(m.GetGauge().GetValue())
}
