package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("oracle_keeper")

	missTypeAttribute    = attribute.String("type", "miss")
	abstainTypeAttribute = attribute.String("type", "abstain")
	successTypeAttribute = attribute.String("type", "success")

	oracleKeeperMetrics = struct {
		votePenaltyCount      metric.Int64Gauge
		validatorSlashedTotal metric.Int64Counter
	}{
		votePenaltyCount: must(meter.Int64Gauge(
			"oracle_vote_penalty_count",
			metric.WithDescription("Oracle vote penalty counts by validator and type (miss, abstain, success)"),
			metric.WithUnit("{count}"),
		)),
		validatorSlashedTotal: must(meter.Int64Counter(
			"oracle_validator_slashed",
			metric.WithDescription("Number of validators slashed by oracle"),
			metric.WithUnit("{count}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
