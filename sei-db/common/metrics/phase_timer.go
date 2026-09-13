package metrics

import (
	"context"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// PhaseTimerFactory constructs shared OTel metrics and builds independent
// PhaseTimer instances. Use Build() to create a timer for each thread.
type PhaseTimerFactory struct {
	meter              metric.Meter
	phaseDurationTotal metric.Float64Counter
	phaseLatency       metric.Float64Histogram
	timerName          string
	staticAttrs        []attribute.KeyValue
}

// NewPhaseTimerFactory creates a factory that records to the given meter with the
// specified timer name (e.g., "main_thread" or "transaction"), publishing
// {timerName}_phase_duration_seconds_total. Call RecordLatencies for the histogram a
// percentile panel reads.
//
// Every measurement recorded by the built timers carries staticAttrs in addition to the
// "phase" attribute. Use staticAttrs to distinguish instances (e.g. cache="state") rather
// than encoding the instance into timerName: attribute values may contain characters that
// are illegal in a Prometheus metric name, and dashboards can filter or aggregate over a
// label but not over a name.
func NewPhaseTimerFactory(
	meter metric.Meter,
	timerName string,
	staticAttrs ...attribute.KeyValue,
) *PhaseTimerFactory {
	phaseDurationTotal, _ := meter.Float64Counter(
		timerName+"_phase_duration_seconds_total",
		metric.WithDescription("Total seconds spent in each phase"),
		metric.WithUnit("s"),
	)
	return &PhaseTimerFactory{
		meter:              meter,
		phaseDurationTotal: phaseDurationTotal,
		timerName:          timerName,
		staticAttrs:        slices.Clone(staticAttrs),
	}
}

// RecordLatencies adds {timerName}_phase_latency_seconds to the timers this factory builds, and
// returns the factory so it can be chained onto the constructor.
//
// It is off by default because it is the expensive half: a bucket is a series, so the histogram
// costs a multiple of what the duration counter does, and most timers are only ever read as the
// share-of-time counter. Turn it on where a percentile panel reads the phases.
func (f *PhaseTimerFactory) RecordLatencies() *PhaseTimerFactory {
	if f == nil || f.phaseLatency != nil {
		return f
	}
	f.phaseLatency, _ = f.meter.Float64Histogram(
		f.timerName+"_phase_latency_seconds",
		metric.WithDescription("Latency per phase (seconds); use for p99, p95, etc."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(LatencyBuckets...),
	)
	return f
}

// NewPhaseTimer creates a factory and builds a single PhaseTimer. Convenient when
// only one timer is needed (e.g., for a single-threaded main loop). staticAttrs are
// attached to every measurement, as described on NewPhaseTimerFactory.
func NewPhaseTimer(meter metric.Meter, timerName string, staticAttrs ...attribute.KeyValue) *PhaseTimer {
	return NewPhaseTimerFactory(meter, timerName, staticAttrs...).Build()
}

// Build returns a new PhaseTimer that records to this factory's metrics.
// Each timer has independent phase state; safe for use by different threads.
//
// Any attrs given are carried on every measurement in addition to the factory's own, naming the
// instance this timer belongs to when one factory serves several.
func (f *PhaseTimerFactory) Build(attrs ...attribute.KeyValue) *PhaseTimer {
	staticAttrs := f.staticAttrs
	if len(attrs) > 0 {
		staticAttrs = append(slices.Clone(f.staticAttrs), attrs...)
	}
	return &PhaseTimer{
		phaseDurationTotal:  f.phaseDurationTotal,
		phaseLatency:        f.phaseLatency,
		staticAttrs:         staticAttrs,
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
	staticAttrs         []attribute.KeyValue
	lastPhase           string
	lastPhaseChangeTime time.Time
}

// SetPhase records a transition to a new phase.
func (p *PhaseTimer) SetPhase(phase string) {
	if p == nil || phase == "" || p.phaseDurationTotal == nil {
		return
	}
	now := time.Now()
	if p.lastPhase != "" {
		p.record(now.Sub(p.lastPhaseChangeTime).Seconds())
	}
	p.lastPhase = phase
	p.lastPhaseChangeTime = now
}

// Reset ends the current phase (capturing its metrics) and clears the phase state.
func (p *PhaseTimer) Reset() {
	if p == nil || p.phaseDurationTotal == nil {
		return
	}
	if p.lastPhase != "" {
		p.record(time.Since(p.lastPhaseChangeTime).Seconds())
	}
	p.lastPhase = ""
}

// record charges seconds to the phase just ended. The histogram is only written when the factory was
// asked for one, which is what keeps a timer nobody plots a percentile of to a single series.
func (p *PhaseTimer) record(seconds float64) {
	ctx := context.Background()
	attrs := p.measurement(p.lastPhase)
	p.phaseDurationTotal.Add(ctx, seconds, attrs)
	if p.phaseLatency != nil {
		p.phaseLatency.Record(ctx, seconds, attrs)
	}
}

// measurement returns the recording option for the given phase: the timer's static
// attributes plus phase={phase}.
func (p *PhaseTimer) measurement(phase string) metric.MeasurementOption {
	phaseAttr := attribute.String("phase", phase)
	if len(p.staticAttrs) == 0 {
		return metric.WithAttributes(phaseAttr)
	}
	attrs := make([]attribute.KeyValue, 0, len(p.staticAttrs)+1)
	attrs = append(attrs, p.staticAttrs...)
	attrs = append(attrs, phaseAttr)
	return metric.WithAttributes(attrs...)
}
