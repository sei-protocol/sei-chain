package cryptosim

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// PhaseTimerFactory constructs shared Prometheus metrics and builds independent
// PhaseTimer instances. Use Build() to create a timer for each thread.
type PhaseTimerFactory struct {
	phaseDurationTotal *prometheus.CounterVec
	phaseLatency       *prometheus.HistogramVec
}

// NewPhaseTimerFactory creates a factory that registers metrics with the given
// name prefix (e.g., "main_thread" produces main_thread_phase_duration_seconds_total).
func NewPhaseTimerFactory(reg *prometheus.Registry, name string) *PhaseTimerFactory {
	phaseDurationTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_phase_duration_seconds_total", name),
		Help: "Total seconds spent in each phase",
	}, []string{"phase"})
	phaseLatency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_phase_latency_seconds", name),
		Help:    "Latency per phase (seconds); use for p99, p95, etc.",
		Buckets: prometheus.ExponentialBucketsRange(0.001, 10, 12),
	}, []string{"phase"})
	reg.MustRegister(phaseDurationTotal, phaseLatency)
	return &PhaseTimerFactory{
		phaseDurationTotal: phaseDurationTotal,
		phaseLatency:       phaseLatency,
	}
}

// NewPhaseTimer creates a factory and builds a single PhaseTimer. Convenient when
// only one timer is needed (e.g., for a single-threaded main loop).
func NewPhaseTimer(reg *prometheus.Registry, name string) *PhaseTimer {
	return NewPhaseTimerFactory(reg, name).Build()
}

// Build returns a new PhaseTimer that records to this factory's metrics.
// Each timer has independent phase state; safe for use by different threads.
func (f *PhaseTimerFactory) Build() *PhaseTimer {
	return &PhaseTimer{
		phaseDurationTotal:  f.phaseDurationTotal,
		phaseLatency:        f.phaseLatency,
		lastPhase:           "",
		lastPhaseChangeTime: time.Time{},
	}
}

// PhaseTimer records time spent in phases (e.g., "executing", "finalizing").
// Call SetPhase when transitioning to a new phase; latency is calculated from the
// previous transition. Not safe for concurrent use on a single instance.
//
// Grafana queries (substitute PREFIX with the name passed to NewPhaseTimer or NewPhaseTimerFactory):
//
// Rate, for pie chart or stacked timeseries (seconds per second):
//
//	rate(PREFIX_phase_duration_seconds_total[$__rate_interval])
//
// Average latency:
//
//	rate(PREFIX_phase_latency_seconds_sum[$__rate_interval]) /
//		rate(PREFIX_phase_latency_seconds_count[$__rate_interval])
//
// Latency percentiles (p99, p95, p50). The phase label (executing, finalizing,
// etc.) distinguishes series; add {phase="executing"} to filter:
//
//	histogram_quantile(0.99, rate(PREFIX_phase_latency_seconds_bucket[$__rate_interval]))
//	histogram_quantile(0.95, rate(PREFIX_phase_latency_seconds_bucket[$__rate_interval]))
//	histogram_quantile(0.50, rate(PREFIX_phase_latency_seconds_bucket[$__rate_interval]))
type PhaseTimer struct {
	phaseDurationTotal  *prometheus.CounterVec
	phaseLatency        *prometheus.HistogramVec
	lastPhase           string
	lastPhaseChangeTime time.Time
}

// SetPhase records a transition to a new phase.
func (p *PhaseTimer) SetPhase(phase string) {
	if p == nil || phase == "" {
		return
	}
	now := time.Now()
	if p.lastPhase != "" {
		latency := now.Sub(p.lastPhaseChangeTime)
		seconds := latency.Seconds()
		p.phaseDurationTotal.WithLabelValues(p.lastPhase).Add(seconds)
		p.phaseLatency.WithLabelValues(p.lastPhase).Observe(seconds)
	}
	p.lastPhase = phase
	p.lastPhaseChangeTime = now
}

// Reset ends the current phase (capturing its metrics) and clears the phase state.
func (p *PhaseTimer) Reset() {
	if p == nil {
		return
	}
	if p.lastPhase != "" {
		latency := time.Since(p.lastPhaseChangeTime)
		seconds := latency.Seconds()
		p.phaseDurationTotal.WithLabelValues(p.lastPhase).Add(seconds)
		p.phaseLatency.WithLabelValues(p.lastPhase).Observe(seconds)
	}
	p.lastPhase = ""
}
