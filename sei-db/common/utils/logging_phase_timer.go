package utils

import (
	"log/slog"
	"time"
)

// LoggingPhaseTimer measures how long each phase of a sequence takes, and logs every phase's duration on one line.
// Not safe for concurrent use.
type LoggingPhaseTimer struct {
	logger *slog.Logger

	// The phase in progress, empty before the first SetPhase().
	phase string

	// When the phase in progress started.
	phaseStart time.Time

	// Each ended phase's name and duration, as logger key-value pairs.
	durations []any
}

// NewLoggingPhaseTimer returns a timer that logs to logger.
func NewLoggingPhaseTimer(logger *slog.Logger) *LoggingPhaseTimer {
	return &LoggingPhaseTimer{logger: logger}
}

// SetPhase records a transition to a new phase, ending the current one.
func (t *LoggingPhaseTimer) SetPhase(
	// Becomes a log key, so it must be non-empty and unique within the timer.
	phase string,
) {
	now := time.Now()
	t.endPhase(now)
	t.phase = phase
	t.phaseStart = now
}

// Log ends the current phase, then logs msg at info level with args, followed by each phase's duration in the order
// the phases ran.
func (t *LoggingPhaseTimer) Log(msg string, args ...any) {
	t.endPhase(time.Now())
	t.phase = ""
	t.logger.Info(msg, append(args, t.durations...)...)
}

// endPhase records the duration of the phase in progress, if any.
func (t *LoggingPhaseTimer) endPhase(now time.Time) {
	if t.phase == "" {
		return
	}
	t.durations = append(t.durations, t.phase, now.Sub(t.phaseStart).Truncate(time.Millisecond))
}
