package giga

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func counterValue(t *testing.T, c *prometheus.CounterInt) int64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.Write(&m))
	return int64(m.GetCounter().GetValue())
}

func TestVoteSentAndReceived(t *testing.T) {
	sent := counterValue(t, Global.votesSentAt(votePrepare))
	recv := counterValue(t, Global.votesReceivedAt(voteApp))
	Global.votesSentAt(votePrepare).Add(1)
	Global.votesReceivedAt(voteApp).Add(1)
	require.Equal(t, sent+1, counterValue(t, Global.votesSentAt(votePrepare)))
	require.Equal(t, recv+1, counterValue(t, Global.votesReceivedAt(voteApp)))
}

func TestLaneVoteSentAndReceived(t *testing.T) {
	sent := counterValue(t, Global.votesSentAt(voteLane))
	recv := counterValue(t, Global.votesReceivedAt(voteLane))
	Global.votesSentAt(voteLane).Add(1)
	Global.votesReceivedAt(voteLane).Add(1)
	require.Equal(t, sent+1, counterValue(t, Global.votesSentAt(voteLane)))
	require.Equal(t, recv+1, counterValue(t, Global.votesReceivedAt(voteLane)))
}
