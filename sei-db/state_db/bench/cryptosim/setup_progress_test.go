package cryptosim

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSetupProgressRender(t *testing.T) {
	tests := []struct {
		name       string
		startCount int64
		target     int64
		current    int64
		elapsed    time.Duration
		want       string
	}{
		{
			name:       "fresh run extrapolates from work done",
			startCount: 0,
			target:     1000,
			current:    250,
			elapsed:    10 * time.Second,
			// 250 in 10s is 25/sec, so the remaining 750 take 30s.
			want: "Created 250 of 1,000 accounts, 30.0s remaining (25.00/sec).",
		},
		{
			name:       "resumed run measures only this run's work",
			startCount: 600,
			target:     1000,
			current:    800,
			elapsed:    10 * time.Second,
			// 200 created in 10s is 20/sec, so the remaining 200 take 10s. Counting the 600 it
			// inherited would report 80/sec and 2.5s.
			want: "Created 800 of 1,000 accounts, 10.0s remaining (20.00/sec).",
		},
		{
			name:       "no estimate before any work is done",
			startCount: 600,
			target:     1000,
			current:    600,
			elapsed:    10 * time.Second,
			want:       "Created 600 of 1,000 accounts.",
		},
		{
			name:       "no estimate before any time has passed",
			startCount: 0,
			target:     1000,
			current:    250,
			elapsed:    0,
			want:       "Created 250 of 1,000 accounts.",
		},
		{
			name:       "no estimate once the target is reached",
			startCount: 0,
			target:     1000,
			current:    1000,
			elapsed:    10 * time.Second,
			want:       "Created 1,000 of 1,000 accounts.",
		},
		{
			name:       "overshooting the target reports no estimate",
			startCount: 0,
			target:     1000,
			current:    1001,
			elapsed:    10 * time.Second,
			want:       "Created 1,001 of 1,000 accounts.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			progress := newSetupProgress("accounts", test.startCount, test.target)
			require.Equal(t, test.want, progress.render(test.current, test.elapsed))
		})
	}
}

// A rate near zero makes the quotient exceed what time.Duration holds, which wraps negative.
func TestSetupProgressRenderClampsUnreachableEstimate(t *testing.T) {
	progress := newSetupProgress("accounts", 0, 1_000_000_000_000)
	line := progress.render(1, time.Hour)

	require.Contains(t, line, ">"+formatDuration(maxSetupEstimate, 1))
	require.NotContains(t, line, "-")
}

func TestSetupProgressLinePadsOverPreviousLine(t *testing.T) {
	progress := newSetupProgress("accounts", 0, 1_000_000)
	progress.startTime = time.Now().Add(-time.Hour)

	// Reaching the target drops the estimate clause, which is the one transition that shortens
	// the line: the count and rate fields only ever grow.
	wide := progress.line(500_000)
	narrow := progress.line(1_000_000)

	require.Less(t, len([]rune(strings.TrimRight(narrow, " "))), len([]rune(wide)),
		"test is not exercising a shrinking line")
	require.Equal(t, len([]rune(wide)), len([]rune(narrow)),
		"a shorter line must be padded to erase the longer line it overwrites")
	require.True(t, strings.HasPrefix(narrow, "Created 1,000,000 of 1,000,000 accounts."))
}

func TestSetupProgressLineUsesPhaseStartTime(t *testing.T) {
	progress := newSetupProgress("accounts", 0, 1000)
	progress.startTime = time.Now().Add(-10 * time.Second)

	require.Contains(t, progress.line(500), "/sec)")
}
