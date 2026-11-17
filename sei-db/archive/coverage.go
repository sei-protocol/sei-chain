package archive

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
)

const (
	// coverageRefreshInterval is how often the reader reloads which blocks are committed.
	coverageRefreshInterval = time.Second
	// coverageQueryTimeout bounds one coverage load. The first load reads every commit row.
	coverageQueryTimeout = time.Minute
)

// coverageQuery returns, as runs of consecutive blocks, the committed blocks after {after}.
var coverageQuery = fmt.Sprintf(`SELECT min(start_block) AS first, max(end_block) AS last
FROM (
    SELECT start_block, end_block,
           sum(start_block > prev_end + 1) OVER (ORDER BY end_block ROWS UNBOUNDED PRECEDING) AS run
    FROM (
        SELECT start_block, end_block,
               lagInFrame(end_block, 1, 0) OVER (ORDER BY end_block ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) AS prev_end
        FROM %s
        WHERE end_block > {after:UInt64}))
GROUP BY run
ORDER BY first`, tableCommits)

// blockRange is the blocks first through last, inclusive.
type blockRange struct {
	first, last uint64
}

// committedRanges is the archive's committed blocks as sorted, non-overlapping ranges.
type committedRanges []blockRange

// contains reports whether block is committed.
func (c committedRanges) contains(block uint64) bool {
	i := sort.Search(len(c), func(i int) bool { return c[i].last >= block })
	return i < len(c) && c[i].first <= block
}

// bounds returns the first and last committed blocks, and false when none is.
func (c committedRanges) bounds() (first, last uint64, ok bool) {
	if len(c) == 0 {
		return 0, 0, false
	}
	return c[0].first, c[len(c)-1].last, true
}

// merge returns c extended by runs, which are sorted and begin after c's last block. It leaves c
// unchanged.
func (c committedRanges) merge(runs []blockRange) committedRanges {
	out := slices.Clone(c)
	for _, run := range runs {
		if n := len(out); n > 0 && run.first <= out[n-1].last+1 {
			out[n-1].last = max(out[n-1].last, run.last)
			continue
		}
		out = append(out, run)
	}
	return out
}

// coverage is the committed ranges a reader serves, which it reloads in the background.
type coverage struct {
	// ranges is nil until the first load succeeds. A load stores a new slice rather than changing
	// the one readers hold.
	ranges atomic.Pointer[committedRanges]
	// refreshMu makes each refresh start from the ranges the previous one stored.
	refreshMu sync.Mutex
}

// load returns the committed ranges, and false before the first load.
func (c *coverage) load() (committedRanges, bool) {
	ranges := c.ranges.Load()
	if ranges == nil {
		return nil, false
	}
	return *ranges, true
}

// refreshCoverageLoop refreshes the coverage now and every coverageRefreshInterval until ctx ends,
// logging when refreshes start or stop failing.
func (r *Reader) refreshCoverageLoop(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(coverageRefreshInterval)
	defer ticker.Stop()
	failing := false
	for {
		err := r.refreshCoverage(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil && !failing {
			logger.Error("archive coverage refresh failing; archive reads fail until it recovers", "err", err)
		} else if err == nil && failing {
			logger.Info("archive coverage refresh recovered")
		}
		failing = err != nil
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// refreshCoverage adds the blocks committed after the last known committed block to the coverage.
func (r *Reader) refreshCoverage(ctx context.Context) error {
	r.coverage.refreshMu.Lock()
	defer r.coverage.refreshMu.Unlock()
	current, _ := r.coverage.load()
	var after uint64
	if _, last, ok := current.bounds(); ok {
		// A commit below last, which only a backfill of a gap writes, is seen after a restart.
		after = last
	}
	var first, last proto.ColUInt64
	var runs []blockRange
	err := r.run(ctx, "coverage", queryLimits{timeout: coverageQueryTimeout}, ch.Query{
		Body:       coverageQuery,
		Parameters: ch.Parameters(map[string]any{"after": after}),
		Result:     proto.Results{{Name: "first", Data: &first}, {Name: "last", Data: &last}},
		OnResult: func(context.Context, proto.Block) error {
			for i := range first {
				runs = append(runs, blockRange{first: first[i], last: last[i]})
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	merged := current.merge(runs)
	r.coverage.ranges.Store(&merged)
	return nil
}
