package conn

import "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "p2p_conn"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Successful old mux ping/pong roundtrip time in seconds, including local send queuing and scheduling.
	pingRTT prometheus.HistogramVec `metrics_name:"ping_rtt" metrics_buckets:"exp(0.0001, 1.5, 30)"`
}
