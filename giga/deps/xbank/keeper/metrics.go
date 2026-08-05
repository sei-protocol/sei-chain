package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// bankMetrics.newAccount mirrors sei-cosmos/x/bank/keeper/metrics.go's
// instrument of the same name/scope so the two dual-emit paths merge into a
// single bank_new_account series. Keep description/unit byte-identical to
// that file or the OTel SDK will stop deduping the instrument.
var (
	meter = otel.Meter("seicosmos_x_bank_keeper")

	bankMetrics = struct {
		newAccount metric.Int64Counter
	}{
		newAccount: must(meter.Int64Counter(
			"bank_new_account",
			metric.WithDescription("Number of new accounts created during bank transfers"),
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
