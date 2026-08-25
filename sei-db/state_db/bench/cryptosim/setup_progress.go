package cryptosim

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// maxSetupEstimate is the largest time remaining that setupProgress will state outright. Beyond
// it the line reports beyond-the-ceiling instead.
//
// The ceiling exists because the estimate is a division by an observed rate: early in a phase, or
// after a stall, that rate approaches zero and the quotient exceeds the ~292 years time.Duration
// can hold, which silently wraps to a negative duration.
const maxSetupEstimate = 99 * 24 * time.Hour

// setupProgress renders the console progress line for one phase of benchmark setup.
type setupProgress struct {
	// The plural noun for the thing being created, e.g. "accounts".
	noun string

	// The count the phase began at.
	startCount int64

	// The count the phase finishes at.
	target int64

	// The time the phase began.
	startTime time.Time

	// The width of the widest line rendered so far.
	maxLineWidth int
}

// newSetupProgress starts tracking a setup phase that runs from startCount to target.
//
// startCount is where this run picked up rather than zero: setup resumes against an existing data
// directory, and the rate has to be measured over the work this run does, not over a count it
// inherited. Passing zero for a resumed phase reports a rate several times too high.
func newSetupProgress(noun string, startCount int64, target int64) *setupProgress {
	return &setupProgress{
		noun:       noun,
		startCount: startCount,
		target:     target,
		startTime:  time.Now(),
	}
}

// line returns the progress line for current, padded so that writing it over the previous line
// erases that line completely.
func (p *setupProgress) line(current int64) string {
	text := p.render(current, time.Since(p.startTime))

	if pad := p.maxLineWidth - utf8.RuneCountInString(text); pad > 0 {
		text += strings.Repeat(" ", pad)
	}
	if width := utf8.RuneCountInString(text); width > p.maxLineWidth {
		p.maxLineWidth = width
	}
	return text
}

// render returns the unpadded progress line for current, given how long the phase has been
// running. It states the time remaining only once there is completed work to extrapolate from.
func (p *setupProgress) render(current int64, elapsed time.Duration) string {
	counts := fmt.Sprintf("Created %s of %s %s",
		int64Commas(current), int64Commas(p.target), p.noun)

	created := current - p.startCount
	remaining := p.target - current
	if created <= 0 || remaining <= 0 || elapsed <= 0 {
		return counts + "."
	}

	perSecond := float64(created) / elapsed.Seconds()
	return fmt.Sprintf("%s, %s remaining (%s/sec).",
		counts, formatEstimate(float64(remaining)/perSecond), formatNumberFloat64(perSecond, 2))
}

// formatEstimate renders a number of seconds as a time remaining, reporting anything past
// maxSetupEstimate as exceeding it rather than stating a figure that overflowed.
func formatEstimate(seconds float64) string {
	if seconds >= maxSetupEstimate.Seconds() {
		return ">" + formatDuration(maxSetupEstimate, 1)
	}
	return formatDuration(time.Duration(seconds*float64(time.Second)), 1)
}
