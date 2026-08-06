package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_x_bank_keeper")

	bankMetrics = struct {
		sendAmount metric.Int64Gauge
		newAccount metric.Int64Counter
	}{
		sendAmount: must(meter.Int64Gauge(
			"last_send_amount",
			metric.WithDescription("Amount sent in the last MsgSend transaction by denomination"),
			metric.WithUnit("{utoken}"),
		)),
		// newAccount is mirrored by utils/metrics.bankNewAccountCounter (bank
		// precompiles) and giga/deps/xbank/keeper.bankMetrics.newAccount (the
		// Giga fork) on the same name/scope, so all three merge into one
		// bank_new_account series. Keep description/unit byte-identical
		// across all three or the OTel SDK stops deduping the instrument.
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
