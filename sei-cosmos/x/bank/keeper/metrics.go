package keeper

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// BankNewAccount* must stay byte-identical to the same consts in
// utils/metrics and giga/deps/xbank/keeper so the three Int64Counter
// declarations merge into one series.
const (
	BankNewAccountMeter       = "seicosmos_x_bank_keeper"
	BankNewAccountName        = "bank_new_account"
	BankNewAccountDescription = "Number of new accounts created during bank transfers"
	BankNewAccountUnit        = "{count}"
)

var (
	meter = otel.Meter(BankNewAccountMeter)

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
		// bank_new_account series.
		newAccount: must(meter.Int64Counter(
			BankNewAccountName,
			metric.WithDescription(BankNewAccountDescription),
			metric.WithUnit(BankNewAccountUnit),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
