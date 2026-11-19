package avail

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// gathered reads one series from the default registry. A missing label set is 0.
func gathered(t *testing.T, name string, labels map[string]string) int64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			if len(got) != len(labels) {
				continue
			}
			match := true
			for k, v := range labels {
				if got[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			if c := m.GetCounter(); c != nil {
				return int64(c.GetValue())
			}
			return int64(m.GetGauge().GetValue())
		}
		return 0
	}
	return 0
}
