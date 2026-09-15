package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/otlptranslator"
	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const (
	meterName = "autobahn"
	timerName = "autobahn_main_loop"

	// PhaseConsensus is time spent waiting for the next committed Autobahn block.
	PhaseConsensus = "consensus"
	// PhaseExecution is time spent executing that block's transactions.
	PhaseExecution = "execution"
	// PhaseStorage is time spent persisting receipts, state, and the app commit.
	PhaseStorage = "storage"
)

var (
	setupOnce sync.Once
	setupErr  error

	loopOnce sync.Once
	loop     *seidbmetrics.PhaseTimer
	loopMu   sync.Mutex
)

// SetupPrometheus installs a Prometheus MeterProvider on the default registerer.
func SetupPrometheus() error {
	setupOnce.Do(func() {
		exporter, err := otelprometheus.New(
			otelprometheus.WithRegisterer(prometheus.DefaultRegisterer),
			otelprometheus.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
		)
		if err != nil {
			setupErr = err
			return
		}
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)))
	})
	return setupErr
}

// MainLoop is the phase timer for Autobahn's execute loop.
func MainLoop() *seidbmetrics.PhaseTimer {
	loopOnce.Do(func() {
		loop = seidbmetrics.NewPhaseTimerFactory(otel.Meter(meterName), timerName).
			RecordLatencies().
			Build()
	})
	return loop
}

// SetPhase records a transition on Autobahn's execute-loop timer.
func SetPhase(phase string) {
	loopMu.Lock()
	defer loopMu.Unlock()
	MainLoop().SetPhase(phase)
}
