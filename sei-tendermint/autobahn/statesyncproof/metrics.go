package statesyncproof

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "autobahn_statesyncproof"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Counts state sync proof export sweeps that failed and will be retried.
	exportFailures prometheus.CounterIntVec
}
