package keeper

import (
	"context"
	"time"

	"github.com/armon/go-metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	contractLabel   = "contract"
	metricTypeLabel = "type"
)

var (
	meter = otel.Meter("wasm_keeper")

	// contractLatencyBuckets (seconds) for contract instantiate/execute/migrate/sudo and IBC relay latency.
	contractLatencyBuckets = metric.WithExplicitBucketBoundaries(
		0.025, 0.05, 0.1, 0.25, 0.5, 0.75, 1.0, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0, 10.0,
	)

	// queryLatencyBuckets (seconds) for smart/raw query latency, which typically runs in the
	// microsecond-to-low-millisecond range and needs finer resolution than contractLatencyBuckets.
	queryLatencyBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	wasmKeeperMetrics = struct {
		contractInstantiateDuration metric.Float64Histogram
		contractExecuteDuration     metric.Float64Histogram
		contractMigrateDuration     metric.Float64Histogram
		contractSudoDuration        metric.Float64Histogram
		contractQuerySmartDuration  metric.Float64Histogram
		contractQueryRawDuration    metric.Float64Histogram
		contractQuerySmartGasUsed   metric.Int64Histogram
	}{
		contractInstantiateDuration: must(meter.Float64Histogram(
			"wasm_contract_instantiate_duration",
			metric.WithDescription("Duration of wasm contract instantiate operations"),
			metric.WithUnit("s"),
			contractLatencyBuckets,
		)),
		contractExecuteDuration: must(meter.Float64Histogram(
			"wasm_contract_execute_duration",
			metric.WithDescription("Duration of wasm contract execute operations"),
			metric.WithUnit("s"),
			contractLatencyBuckets,
		)),
		contractMigrateDuration: must(meter.Float64Histogram(
			"wasm_contract_migrate_duration",
			metric.WithDescription("Duration of wasm contract migrate operations"),
			metric.WithUnit("s"),
			contractLatencyBuckets,
		)),
		contractSudoDuration: must(meter.Float64Histogram(
			"wasm_contract_sudo_duration",
			metric.WithDescription("Duration of wasm contract sudo operations"),
			metric.WithUnit("s"),
			contractLatencyBuckets,
		)),
		contractQuerySmartDuration: must(meter.Float64Histogram(
			"wasm_contract_query_smart_duration",
			metric.WithDescription("Duration of wasm contract smart query operations"),
			metric.WithUnit("s"),
			queryLatencyBuckets,
		)),
		contractQueryRawDuration: must(meter.Float64Histogram(
			"wasm_contract_query_raw_duration",
			metric.WithDescription("Duration of wasm contract raw query operations"),
			metric.WithUnit("s"),
			queryLatencyBuckets,
		)),
		contractQuerySmartGasUsed: must(meter.Int64Histogram(
			"wasm_contract_query_smart_gas_used",
			metric.WithDescription("Gas used by wasm contract smart query invocations"),
			metric.WithUnit("{gas}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

func recordContractInstantiateDuration(ctx context.Context, start time.Time) {
	wasmKeeperMetrics.contractInstantiateDuration.Record(ctx, time.Since(start).Seconds())
	// TODO(PLT-910): remove once wasm_contract_instantiate_duration verified
	telemetry.MeasureSince(start, "wasm", contractLabel, "instantiate")
}

func recordContractExecuteDuration(ctx context.Context, start time.Time) {
	wasmKeeperMetrics.contractExecuteDuration.Record(ctx, time.Since(start).Seconds())
	// TODO(PLT-910): remove once wasm_contract_execute_duration verified
	telemetry.MeasureSince(start, "wasm", contractLabel, "execute")
}

func recordContractMigrateDuration(ctx context.Context, start time.Time) {
	wasmKeeperMetrics.contractMigrateDuration.Record(ctx, time.Since(start).Seconds())
	// TODO(PLT-910): remove once wasm_contract_migrate_duration verified
	telemetry.MeasureSince(start, "wasm", contractLabel, "migrate")
}

func recordContractSudoDuration(ctx context.Context, start time.Time) {
	wasmKeeperMetrics.contractSudoDuration.Record(ctx, time.Since(start).Seconds())
	// TODO(PLT-910): remove once wasm_contract_sudo_duration verified
	telemetry.MeasureSince(start, "wasm", contractLabel, "sudo")
}

func recordContractQuerySmartDuration(ctx context.Context, start time.Time) {
	wasmKeeperMetrics.contractQuerySmartDuration.Record(ctx, time.Since(start).Seconds())
	// TODO(PLT-910): remove once wasm_contract_query_smart_duration verified
	telemetry.MeasureSince(start, "wasm", contractLabel, "query-smart")
}

func recordContractQueryRawDuration(ctx context.Context, start time.Time) {
	wasmKeeperMetrics.contractQueryRawDuration.Record(ctx, time.Since(start).Seconds())
	// TODO(PLT-910): remove once wasm_contract_query_raw_duration verified
	telemetry.MeasureSince(start, "wasm", contractLabel, "query-raw")
}

func recordContractQuerySmartInvocation(contractAddress string) {
	// No OTel counter here: it would be redundant with wasm_contract_query_smart_duration's
	// count, which is recorded unconditionally on every QuerySmart call just like this one.
	// TODO(PLT-910): remove once wasm_contract_query_smart_duration verified
	telemetry.IncrCounterWithLabels(
		[]string{"wasm", contractLabel, "query-smart", "invocation"},
		1,
		[]metrics.Label{telemetry.NewLabel("contract_address", contractAddress)},
	)
}

func recordContractQuerySmartGasUsed(ctx context.Context, contractAddress string, gasUsed uint64) {
	// contract_address omitted on the OTel histogram: unlike the legacy Prometheus sink, the
	// OTel SDK has no series expiration, so a per-contract label here would retain one series
	// per distinct contract address queried for the process lifetime.
	wasmKeeperMetrics.contractQuerySmartGasUsed.Record(ctx, int64(gasUsed)) //nolint:gosec
	// TODO(PLT-910): remove once wasm_contract_query_smart_gas_used verified
	telemetry.SetGaugeWithLabels(
		[]string{"wasm", contractLabel, "query-smart", "gas-used"},
		float32(gasUsed),
		[]metrics.Label{telemetry.NewLabel("contract_address", contractAddress)},
	)
}

const (
	labelPinned = "pinned"
	labelMemory = "memory"
	labelFs     = "fs"
)

// metricSource source of wasmvm metrics
type metricSource interface {
	GetMetrics() (*wasmvmtypes.Metrics, error)
}

var _ prometheus.Collector = (*WasmVMMetricsCollector)(nil)

// WasmVMMetricsCollector custom metrics collector to be used with Prometheus
type WasmVMMetricsCollector struct {
	source             metricSource
	CacheHitsDescr     *prometheus.Desc
	CacheMissesDescr   *prometheus.Desc
	CacheElementsDescr *prometheus.Desc
	CacheSizeDescr     *prometheus.Desc
}

// NewWasmVMMetricsCollector constructor
func NewWasmVMMetricsCollector(s metricSource) *WasmVMMetricsCollector {
	return &WasmVMMetricsCollector{
		source:             s,
		CacheHitsDescr:     prometheus.NewDesc("wasmvm_cache_hits_total", "Total number of cache hits", []string{metricTypeLabel}, nil),
		CacheMissesDescr:   prometheus.NewDesc("wasmvm_cache_misses_total", "Total number of cache misses", nil, nil),
		CacheElementsDescr: prometheus.NewDesc("wasmvm_cache_elements_total", "Total number of elements in the cache", []string{metricTypeLabel}, nil),
		CacheSizeDescr:     prometheus.NewDesc("wasmvm_cache_size_bytes", "Total number of elements in the cache", []string{metricTypeLabel}, nil),
	}
}

// Register registers all metrics
func (p *WasmVMMetricsCollector) Register(r prometheus.Registerer) {
	r.MustRegister(p)
}

// Describe sends the super-set of all possible descriptors of metrics
func (p *WasmVMMetricsCollector) Describe(descs chan<- *prometheus.Desc) {
	descs <- p.CacheHitsDescr
	descs <- p.CacheMissesDescr
	descs <- p.CacheElementsDescr
	descs <- p.CacheSizeDescr
}

// Collect is called by the Prometheus registry when collecting metrics.
func (p *WasmVMMetricsCollector) Collect(c chan<- prometheus.Metric) {
	m, err := p.source.GetMetrics()
	if err != nil {
		return
	}
	c <- prometheus.MustNewConstMetric(p.CacheHitsDescr, prometheus.CounterValue, float64(m.HitsPinnedMemoryCache), labelPinned)
	c <- prometheus.MustNewConstMetric(p.CacheHitsDescr, prometheus.CounterValue, float64(m.HitsMemoryCache), labelMemory)
	c <- prometheus.MustNewConstMetric(p.CacheHitsDescr, prometheus.CounterValue, float64(m.HitsFsCache), labelFs)
	c <- prometheus.MustNewConstMetric(p.CacheMissesDescr, prometheus.CounterValue, float64(m.Misses))
	c <- prometheus.MustNewConstMetric(p.CacheElementsDescr, prometheus.GaugeValue, float64(m.ElementsPinnedMemoryCache), labelPinned)
	c <- prometheus.MustNewConstMetric(p.CacheElementsDescr, prometheus.GaugeValue, float64(m.ElementsMemoryCache), labelMemory)
	c <- prometheus.MustNewConstMetric(p.CacheSizeDescr, prometheus.GaugeValue, float64(m.SizeMemoryCache), labelMemory)
	c <- prometheus.MustNewConstMetric(p.CacheSizeDescr, prometheus.GaugeValue, float64(m.SizePinnedMemoryCache), labelPinned)
	// Node about fs metrics:
	// The number of elements and the size of elements in the file system cache cannot easily be obtained.
	// We had to either scan the whole directory of potentially thousands of files or track the values when files are added or removed.
	// Such a tracking would need to be on disk such that the values are not cleared when the node is restarted.
}
