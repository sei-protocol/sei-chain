package lakesim

import (
	"fmt"
	"time"
)

// progressReporter prints the block count and rate at a fixed interval.
type progressReporter struct {
	// The time between progress lines. 0 prints none.
	interval time.Duration

	// When the last progress line was printed, or when reporting began.
	lastTime time.Time

	// The block count in the last progress line.
	lastBlocks uint64
}

// newProgressReporter returns a reporter printing every intervalSeconds seconds, or never if it is 0.
func newProgressReporter(intervalSeconds int) *progressReporter {
	return &progressReporter{interval: time.Duration(intervalSeconds) * time.Second, lastTime: time.Now()}
}

// Report prints a progress line if the interval has passed since the last one.
func (r *progressReporter) Report(
	// The number of blocks produced so far.
	blocks uint64,
) {
	if r.interval == 0 {
		return
	}
	now := time.Now()
	elapsed := now.Sub(r.lastTime)
	if elapsed < r.interval {
		return
	}
	rate := float64(blocks-r.lastBlocks) / elapsed.Seconds()
	fmt.Printf("blocks: %d, blocks/s: %.1f\n", blocks, rate)
	r.lastTime = now
	r.lastBlocks = blocks
}
