package keeper

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
)

// BankNewAccount* must stay byte-identical to the same consts in
// sei-cosmos/x/bank/keeper and utils/metrics so the three Int64Counter
// declarations merge into one series.
const (
	BankNewAccountMeter       = "seicosmos_x_bank_keeper"
	BankNewAccountName        = "bank_new_account"
	BankNewAccountDescription = "Number of new accounts created during bank transfers"
	BankNewAccountUnit        = "{count}"
)

// bankMetrics.newAccount mirrors sei-cosmos/x/bank/keeper/metrics.go's
// instrument of the same name/scope so the two dual-emit paths merge into a
// single bank_new_account series.
var (
	meter = otel.Meter(BankNewAccountMeter)

	bankMetrics = struct {
		newAccount metric.Int64Counter
	}{
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

// recordNewAccounts dual-emits the legacy new-account counter and its OTel
// counterpart (bank_new_account). Runs from consensus-critical send paths, so
// a telemetry fault here must not panic into the caller.
func recordNewAccounts(ctx context.Context, count int64) {
	if count <= 0 {
		return
	}
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	// TODO(PLT-353): remove once bank_new_account verified
	func() {
		defer func() {
			if e := recover(); e != nil {
				fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
			}
		}()
		telemetry.IncrCounter(float32(count), "new", "account")
	}()
	bankMetrics.newAccount.Add(ctx, count)
}
