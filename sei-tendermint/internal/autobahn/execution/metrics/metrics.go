package metrics

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_autobahn_execution"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// gas used by executed blocks
	gasUsed prometheus.CounterIntVec
}

// GasUsed counts the gas used by the blocks the executor commits.
var GasUsed = Global.gasUsedAt()
