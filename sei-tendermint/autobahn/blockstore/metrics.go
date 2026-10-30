package blockstore

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "autobahn_blockstore"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Lowest SuperBlock road index retained by BlockDB.
	pruneWatermark prometheus.GaugeIntVec
}
