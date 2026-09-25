package metrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestSetAnchorRoad(t *testing.T) {
	Get().AnchorRoad.Set(7)
	var m dto.Metric
	require.NoError(t, Get().AnchorRoad.Write(&m))
	require.Equal(t, float64(7), m.GetGauge().GetValue())
}
