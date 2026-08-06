package keeper

import (
	"context"
	"runtime/debug"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
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

// recordNewAccounts dual-emits the legacy new-account counter and its OTel
// counterpart (bank_new_account). Runs from consensus-critical send paths, so
// a telemetry fault here must not panic into the caller.
func recordNewAccounts(ctx context.Context, count int64) {
	if count == 0 {
		return
	}
	defer func() {
		if e := recover(); e != nil {
			debug.PrintStack()
		}
	}()
	// TODO(PLT-353): remove once bank_new_account verified
	telemetry.IncrCounter(float32(count), "new", "account")
	bankMetrics.newAccount.Add(ctx, count)
}
