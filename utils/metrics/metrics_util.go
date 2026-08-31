package metrics

import (
	"context"
	"errors"
	"fmt"
	"math/big"
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

// RecordBankNewAccount dual-emits the legacy new-account counter and its OTel
// counterpart (bank_new_account). Call from defer when creating an account.
// Runs during precompile execution, so a telemetry fault here must not panic
// into a consensus-critical path.
func RecordBankNewAccount(ctx context.Context) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintf(os.Stderr, "telemetry panic: %v\n%s", e, debug.Stack())
		}
	}()
	// TODO(PLT-353): remove once bank_new_account verified
	telemetry.IncrCounter(1, "new", "account")
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

// sei_evm_zero_storage_pruned_keys
func IncrEvmZeroStoragePrunedKeys(count uint64) {
	SafeTelemetryIncrCounter(
		float32(count),
		"sei", "evm", "zero", "storage", "pruned", "keys",
	)
}

// sei_evm_zero_storage_processed_keys
func IncrEvmZeroStorageProcessedKeys(count uint64) {
	SafeTelemetryIncrCounter(
		float32(count),
		"sei", "evm", "zero", "storage", "processed", "keys",
	)
}

// sei_evm_zero_storage_pruned_bytes
func IncrEvmZeroStoragePrunedBytes(bytes uint64) {
	SafeTelemetryIncrCounter(
		float32(bytes),
		"sei", "evm", "zero", "storage", "pruned", "bytes",
	)
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

func IncrementNonceMismatch(tooHigh bool) {
	cause := "too_low"
	if tooHigh {
		cause = "too_high"
	}
	SafeTelemetryIncrCounterWithLabels(
		[]string{"sei", "nonce", "mismatch"},
		1,
		[]metrics.Label{
			telemetry.NewLabel("cause", cause),
		},
	)
}

func AddHistogramMetric(key []string, value float32) {
	metrics.AddSample(key, value)
}

// Gauge for gas price paid for transactions
// Metric Name:
//
// sei_evm_effective_gas_price
func HistogramEvmEffectiveGasPrice(gasPrice *big.Int) {
	AddHistogramMetric(
		[]string{"sei", "evm", "effective", "gas", "price"},
		float32(gasPrice.Uint64()),
	)
}

// Gauge for block base fee
// Metric Name:
//
// sei_evm_block_base_fee
func GaugeEvmBlockBaseFee(baseFee *big.Int, blockHeight int64) {
	metrics.SetGauge(
		[]string{"sei", "evm", "block", "base", "fee"},
		float32(baseFee.Uint64()),
	)
}
