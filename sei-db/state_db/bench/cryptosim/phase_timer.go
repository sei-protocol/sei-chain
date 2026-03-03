package cryptosim

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// PhaseTimerFactory constructs shared OTel metrics and builds independent
// PhaseTimer instances. Use Build() to create a timer for each thread.
type PhaseTimerFactory struct {
	phaseDurationTotal metric.Float64Counter
	phaseLatency       metric.Float64Histogram
	timerName          string
}

// NewPhaseTimerFactory creates a factory that records to the given meter with the
// specified timer name (e.g., "main_thread" or "transaction"). Metric names are
// {timerName}_phase_duration_seconds_total and {timerName}_phase_latency_seconds
// to match existing Grafana dashboards.
func NewPhaseTimerFactory(meter metric.Meter, timerName string) *PhaseTimerFactory {
	phaseDurationTotal, _ := meter.Float64Counter(
		timerName+"_phase_duration_seconds_total",
		metric.WithDescription("Total seconds spent in each phase"),
		metric.WithUnit("s"),
	)
	phaseLatency, _ := meter.Float64Histogram(
		timerName+"_phase_latency_seconds",
		metric.WithDescription("Latency per phase (seconds); use for p99, p95, etc."),
		metric.WithUnit("s"),
	)
	return &PhaseTimerFactory{
		phaseDurationTotal: phaseDurationTotal,
		phaseLatency:       phaseLatency,
		timerName:          timerName,
	}
}

// NewPhaseTimer creates a factory and builds a single PhaseTimer. Convenient when
// only one timer is needed (e.g., for a single-threaded main loop).
func NewPhaseTimer(meter metric.Meter, timerName string) *PhaseTimer {
	return NewPhaseTimerFactory(meter, timerName).Build()
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
type PhaseTimer struct {
	phaseDurationTotal  metric.Float64Counter
	phaseLatency        metric.Float64Histogram
	lastPhase           string
	lastPhaseChangeTime time.Time
}

// SetPhase records a transition to a new phase.
func (p *PhaseTimer) SetPhase(phase string) {
	if p == nil || phase == "" || p.phaseDurationTotal == nil || p.phaseLatency == nil {
		return
	}
	now := time.Now()
	ctx := context.Background()
	if p.lastPhase != "" {
		latency := now.Sub(p.lastPhaseChangeTime)
		seconds := latency.Seconds()
		phaseAttr := attribute.String("phase", p.lastPhase)
		p.phaseDurationTotal.Add(ctx, seconds, metric.WithAttributes(phaseAttr))
		p.phaseLatency.Record(ctx, seconds, metric.WithAttributes(phaseAttr))
	}
	p.lastPhase = phase
	p.lastPhaseChangeTime = now
}

// Reset ends the current phase (capturing its metrics) and clears the phase state.
func (p *PhaseTimer) Reset() {
	if p == nil || p.phaseDurationTotal == nil || p.phaseLatency == nil {
		return
	}
	if p.lastPhase != "" {
		latency := time.Since(p.lastPhaseChangeTime)
		seconds := latency.Seconds()
		phaseAttr := attribute.String("phase", p.lastPhase)
		p.phaseDurationTotal.Add(context.Background(), seconds, metric.WithAttributes(phaseAttr))
		p.phaseLatency.Record(context.Background(), seconds, metric.WithAttributes(phaseAttr))
	}
	p.lastPhase = ""
}
