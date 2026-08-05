package metrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The two instruments a factory named "test_timer" registers.
var phaseInstruments = []string{
	"test_timer_phase_duration_seconds_total",
	"test_timer_phase_latency_seconds",
}

// collectPhaseAttrs drives a timer through two phases and returns the attribute sets recorded on
// the named instrument, keyed by the instrument name.
func collectPhaseAttrs(t *testing.T, staticAttrs ...attribute.KeyValue) map[string][]attribute.Set {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	timer := NewPhaseTimer(provider.Meter("test"), "test_timer", staticAttrs...)

	// The first SetPhase only opens a phase; the second closes it and records. Reset closes the
	// second phase, so both phases produce measurements.
	timer.SetPhase("first")
	timer.SetPhase("second")
	timer.Reset()

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))

	attrsByInstrument := make(map[string][]attribute.Set)
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[float64]:
				for _, point := range data.DataPoints {
					attrsByInstrument[m.Name] = append(attrsByInstrument[m.Name], point.Attributes)
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					attrsByInstrument[m.Name] = append(attrsByInstrument[m.Name], point.Attributes)
				}
			default:
				t.Fatalf("unexpected data type %T for instrument %s", data, m.Name)
			}
		}
	}
	return attrsByInstrument
}

// phases returns the value of the "phase" attribute in each set, sorted.
func phases(t *testing.T, sets []attribute.Set) []string {
	t.Helper()

	found := make([]string, 0, len(sets))
	for _, set := range sets {
		value, ok := set.Value("phase")
		require.True(t, ok, "every measurement must carry a phase attribute, got %v", set.ToSlice())
		found = append(found, value.AsString())
	}
	return found
}

func TestPhaseTimerRecordsPhaseAttributeOnly(t *testing.T) {
	attrsByInstrument := collectPhaseAttrs(t)

	require.Len(t, attrsByInstrument, 2, "expected both phase instruments, got %v", attrsByInstrument)
	for _, name := range phaseInstruments {
		sets := attrsByInstrument[name]
		require.ElementsMatch(t, []string{"first", "second"}, phases(t, sets), "instrument %s", name)
		for _, set := range sets {
			require.Equal(t, 1, set.Len(),
				"instrument %s must carry only phase, got %v", name, set.ToSlice())
		}
	}
}

// A hyphen in a static attribute value is legal: attribute values become Prometheus label values,
// not part of the metric name, so no escaping is applied and no series collide.
func TestPhaseTimerRecordsStaticAttributes(t *testing.T) {
	attrsByInstrument := collectPhaseAttrs(t,
		attribute.String("cache", "state-cache"),
		attribute.Int("shard", 3),
	)

	require.Len(t, attrsByInstrument, 2, "expected both phase instruments, got %v", attrsByInstrument)
	for _, name := range phaseInstruments {
		sets := attrsByInstrument[name]
		require.ElementsMatch(t, []string{"first", "second"}, phases(t, sets), "instrument %s", name)
		for _, set := range sets {
			cache, ok := set.Value("cache")
			require.True(t, ok, "instrument %s lost the cache attribute: %v", name, set.ToSlice())
			require.Equal(t, "state-cache", cache.AsString())

			shard, ok := set.Value("shard")
			require.True(t, ok, "instrument %s lost the shard attribute: %v", name, set.ToSlice())
			require.Equal(t, int64(3), shard.AsInt64())
		}
	}
}

// The factory must not alias the caller's variadic slice: mutating it after construction must not
// change what the built timers record.
func TestPhaseTimerFactoryCopiesStaticAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	staticAttrs := []attribute.KeyValue{attribute.String("cache", "state")}
	factory := NewPhaseTimerFactory(provider.Meter("test"), "copy_timer", staticAttrs...)
	staticAttrs[0] = attribute.String("cache", "mutated")

	timer := factory.Build()
	timer.SetPhase("first")
	timer.Reset()

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))

	seen := 0
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[float64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				cache, found := point.Attributes.Value("cache")
				require.True(t, found)
				require.Equal(t, "state", cache.AsString())
				seen++
			}
		}
	}
	require.Positive(t, seen, "no counter measurements were recorded")
}
