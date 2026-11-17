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
	meterName        = "autobahn"
	timerName        = "autobahn_main_loop"
	storageTimerName = "autobahn_storage_tail"

	// PhaseConsensus is time between one block's commit and the next block's execution.
	PhaseConsensus = "consensus"
	// PhaseExecution is time spent executing that block's transactions.
	PhaseExecution = "execution"
	// PhaseStorage is time spent persisting receipts, state, and the app commit.
	PhaseStorage = "storage"

	// StoragePhaseAppCommit is the app's Commit call.
	StoragePhaseAppCommit = "app_commit"
)

var (
	setupOnce sync.Once
	setupErr  error

	loopOnce  sync.Once
	loop      *seidbmetrics.PhaseTimer
	loopMu    sync.Mutex
	loopPhase string

	storageOnce sync.Once
	storage     *seidbmetrics.PhaseTimer
	storageMu   sync.Mutex
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

// SetPhase records a transition on Autobahn's execute-loop timer. Leaving PhaseStorage closes the
// storage tail's current phase.
func SetPhase(phase string) {
	loopMu.Lock()
	defer loopMu.Unlock()
	if loopPhase == PhaseStorage && phase != PhaseStorage {
		storageMu.Lock()
		StorageTail().Reset()
		storageMu.Unlock()
	}
	loopPhase = phase
	MainLoop().SetPhase(phase)
}

// StorageTail is the phase timer splitting the execute loop's PhaseStorage into
// the router's steps after FinalizeBlock returns.
func StorageTail() *seidbmetrics.PhaseTimer {
	storageOnce.Do(func() {
		storage = seidbmetrics.NewPhaseTimer(otel.Meter(meterName), storageTimerName)
	})
	return storage
}

// SetStoragePhase records a transition on the storage tail timer.
func SetStoragePhase(phase string) {
	storageMu.Lock()
	defer storageMu.Unlock()
	StorageTail().SetPhase(phase)
}
