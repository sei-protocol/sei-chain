package utils

import (
	"bytes"
	"log/slog"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// The logged line holds the message, then the caller's arguments, then each phase under its name in the order the
// phases ran, the last one ended by Log().
func TestLoggingPhaseTimerLogsPhasesInOrder(t *testing.T) {
	var out bytes.Buffer
	timer := NewLoggingPhaseTimer(slog.New(slog.NewTextHandler(&out, nil)))
	timer.SetPhase("first")
	timer.SetPhase("second")
	timer.SetPhase("third")
	timer.Log("Did the steps", "height", 7)

	require.Regexp(t,
		regexp.MustCompile(`msg="Did the steps" height=7 first=\S+ second=\S+ third=\S+\n$`), out.String())
}
