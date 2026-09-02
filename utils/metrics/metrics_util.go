package metrics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	metrics "github.com/armon/go-metrics"
	"github.com/prometheus/otlptranslator"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// BankNewAccount* must stay byte-identical to the same consts in
// sei-cosmos/x/bank/keeper and giga/deps/xbank/keeper so the three
// Int64Counter declarations merge into one series.
const (
	BankNewAccountMeter       = "seicosmos_x_bank_keeper"
	BankNewAccountName        = "bank_new_account"
	BankNewAccountDescription = "Number of new accounts created during bank transfers"
	BankNewAccountUnit        = "{count}"
)

// bankNewAccountCounter mirrors sei-cosmos/x/bank/keeper/metrics.go's
// instrument of the same name/scope (and giga/deps/xbank/keeper/metrics.go's)
// so precompile-originated and keeper-originated new-account events merge
// into a single bank_new_account series.
var bankNewAccountCounter = mustCounter(otel.Meter(BankNewAccountMeter).Int64Counter(
	BankNewAccountName,
	metric.WithDescription(BankNewAccountDescription),
	metric.WithUnit(BankNewAccountUnit),
))

func mustCounter(c metric.Int64Counter, err error) metric.Int64Counter {
	if err != nil {
		panic(err)
	}
	return c
}

func SetupOtelMetricsProvider(chainID string) error {
	if chainID == "" {
		return fmt.Errorf("chainID must not be empty")
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("chain_id", chainID),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create OTel resource: %w", err)
	}

	metricsExporter, err := prometheus.New(
		prometheus.WithNamespace("sei_chain"),
		prometheus.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
		prometheus.WithResourceAsConstantLabels(
			attribute.NewAllowKeysFilter("chain_id"),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}
	otel.SetMeterProvider(sdk.NewMeterProvider(
		sdk.WithResource(res),
		sdk.WithReader(metricsExporter),
	))
	return nil
}

// RecordBankNewAccount increments the bank_new_account counter. Call from
// defer when creating an account.
func RecordBankNewAccount(ctx context.Context) {
	// Runs during precompile execution, so a telemetry fault must not panic
	// into a consensus-critical path.
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	bankNewAccountCounter.Add(ctx, 1)
}

func SafeTelemetryIncrCounter(val float32, keys ...string) {
	defer func() {
		if e := recover(); e != nil {
			debug.PrintStack()
			return
		}
	}()
	telemetry.IncrCounter(val, keys...)
}

func SafeTelemetryIncrCounterWithLabels(keys []string, val float32, labels []metrics.Label) {
	defer func() {
		if e := recover(); e != nil {
			debug.PrintStack()
			return
		}
	}()
	telemetry.IncrCounterWithLabels(keys, val, labels)
}

func IncrementErrorMetrics(scenario string, err error) {
	if err == nil {
		return
	}
	var assocErr types.AssociationMissingErr
	if errors.As(err, &assocErr) {
		IncrementAssociationError(scenario, assocErr)
		return
	}
	// add other error types to handle as metrics
}

func IncrementAssociationError(scenario string, err types.AssociationMissingErr) {
	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "association", "error"},
		1,
		[]metrics.Label{
			telemetry.NewLabel("scenario", scenario),
			telemetry.NewLabel("type", err.AddressType()),
		},
	)
}
