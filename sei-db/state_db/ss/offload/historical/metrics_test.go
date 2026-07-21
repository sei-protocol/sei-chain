package historical

import "testing"

func TestFallbackMetricsNilSafe(t *testing.T) {
	var m *fallbackMetrics
	m.recordRead("get", fallbackOutcomeCacheHit)
}

func TestFallbackMetricsRecordNoPanic(t *testing.T) {
	m := newFallbackMetrics()
	for _, outcome := range []string{
		fallbackOutcomeCacheHit,
		fallbackOutcomeBackendHit,
		fallbackOutcomeBackendMiss,
		fallbackOutcomeError,
	} {
		m.recordRead("get", outcome)
		m.recordRead("has", outcome)
	}
}
