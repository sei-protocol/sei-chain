// Package gasbench measures per-opcode EVM execution time and correlates it
// with gas cost via differential microbenchmarking.
//
// The harness is tracer-free by construction: nothing here attaches an EVM
// tracer, so the interpreter runs its hot path unobserved. Timing uses the
// monotonic clock (time.Now/time.Since) around a caller-supplied RunOnce.
package gasbench

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"
)

// RunOnce executes a pre-loaded EVM program to completion through the
// tracer-free interpreter and reports the gas it consumed.
//
// A RunOnce closes over its program (see Program.Run in program.go) so the
// timing loop never rebuilds interpreter/StateDB state per call. Rebuilding
// per call would swamp a nanosecond-scale opcode signal with allocation
// noise; the differential construction only holds if baseline and target pay
// identical, amortized-out setup cost.
//
// Contract the EVM-wiring side must satisfy:
//   - Deterministic: identical program yields identical gasUsed and
//     equivalent work every call; no state carried across calls that would
//     drift the timing (Program.Run resets transient storage/access list
//     per entry, which is safe only for pure-compute, non-state-touching
//     programs).
//   - Self-contained and tracer-free: no I/O, logging, or tracer on the hot
//     path; the only thing measured is interpreter execution.
//   - Sufficient work: the program runs the opcode-under-test enough times
//     that total runtime is well above timer resolution (target >= ~10us),
//     so per-call time.Now overhead and clock granularity do not dominate.
//   - Allocation-light: GC is disabled during the window, so allocations
//     across warmup+Iterations must fit in RAM. Keep the program lean or
//     lower Iterations.
type RunOnce func() (gasUsed uint64, err error)

// sink defeats dead-code elimination of the gas result. A single measurement
// goroutine (GOMAXPROCS=1, locked thread) means no synchronization is needed.
var sink uint64

// Config controls one measurement series.
type Config struct {
	Warmup     int  // discarded iterations; warm I-cache/D-cache/branch predictor and lazy init
	Iterations int  // timed iterations retained as samples
	DisableGC  bool // stop the collector for the measurement window (recommended)
	LockThread bool // pin the goroutine to its OS thread for the window (recommended)
}

// DefaultConfig is a sane starting point; tune Iterations to the noise floor.
func DefaultConfig() Config {
	return Config{Warmup: 2000, Iterations: 20000, DisableGC: true, LockThread: true}
}

// Series is the raw output of one measurement: per-iteration wall-clock samples
// plus the deterministic gas cost.
type Series struct {
	InputID  string // the measured input's identity, e.g. "ADD/baseline" (opcode + variant)
	GasUsed  uint64
	Samples  []time.Duration
	Warnings []string

	// NvcswDelta/NivcswDelta are process-wide (RUSAGE_SELF, not thread-scoped
	// -- Go exposes no portable RUSAGE_THREAD) voluntary/involuntary
	// context-switch counts observed during the timed window (Warmup
	// excluded). This is the cheapest "active benchmarking" check available
	// (Gregg): a nonzero NivcswDelta on a nominally pinned core is direct
	// confirmation the kernel scheduler preempted the measurement thread
	// mid-window -- the same tick/IRQ/neighbor mechanism a CoV reading only
	// infers indirectly. See
	// designs/gas-repricing-telemetry/research/microbenchmark-noise-isolation-tradeoffs.md.
	NvcswDelta  int64
	NivcswDelta int64
}

// rusageSnapshot reads the process's current voluntary/involuntary
// context-switch counters. Process-wide rather than thread-scoped, so on a
// process with other active goroutines the delta can include their
// scheduling activity too; for this harness's single-goroutine measurement
// loop that's a minor overcount, not a different mechanism.
func rusageSnapshot() (nvcsw, nivcsw int64, err error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, 0, fmt.Errorf("gasbench: getrusage: %w", err)
	}
	return int64(ru.Nvcsw), int64(ru.Nivcsw), nil
}

// Measure runs Warmup discarded iterations, then Iterations timed iterations
// of run(), returning per-iteration monotonic samples.
func Measure(id string, run RunOnce, cfg Config) (Series, error) {
	if run == nil {
		return Series{}, fmt.Errorf("gasbench: nil RunOnce for %q", id)
	}
	if cfg.Iterations < 1 {
		return Series{}, fmt.Errorf("gasbench: Iterations must be >= 1 for %q", id)
	}

	s := Series{InputID: id, Samples: make([]time.Duration, cfg.Iterations)}
	if n := runtime.GOMAXPROCS(0); n != 1 {
		s.Warnings = append(s.Warnings,
			fmt.Sprintf("GOMAXPROCS=%d (want 1): scheduler noise will inflate the tail", n))
	}

	if cfg.LockThread {
		// Stop the OS from migrating this goroutine across cores mid-window,
		// which would cost cold-cache and cross-NUMA restarts inside a sample.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	// Warmup, discarded. Also the first place a broken program is caught.
	var g uint64
	var err error
	for i := 0; i < cfg.Warmup; i++ {
		if g, err = run(); err != nil {
			return Series{}, fmt.Errorf("gasbench: warmup failed for %q: %w", id, err)
		}
	}
	sink ^= g

	if cfg.DisableGC {
		// A GC pause landing inside a sample is pure tail noise and is exactly
		// what corrupts p99. Collect once to start from a clean heap, stop the
		// collector for the window, and restore the prior target on exit. Cost:
		// the heap grows unbounded during the window (see RunOnce contract).
		runtime.GC()
		prev := debug.SetGCPercent(-1)
		defer debug.SetGCPercent(prev)
	}

	nvcswBefore, nivcswBefore, rerr := rusageSnapshot()
	rusageOK := rerr == nil
	if rerr != nil {
		s.Warnings = append(s.Warnings, rerr.Error())
	}

	for i := 0; i < cfg.Iterations; i++ {
		start := time.Now()
		g, err = run()
		d := time.Since(start) // monotonic; immune to wall-clock/NTP steps
		if err != nil {
			return Series{}, fmt.Errorf("gasbench: iteration %d failed for %q: %w", i, id, err)
		}
		s.Samples[i] = d
		sink ^= g
	}
	s.GasUsed = g

	if rusageOK {
		nvcswAfter, nivcswAfter, rerr := rusageSnapshot()
		if rerr != nil {
			s.Warnings = append(s.Warnings, rerr.Error())
		} else {
			s.NvcswDelta = nvcswAfter - nvcswBefore
			s.NivcswDelta = nivcswAfter - nivcswBefore
		}
	}
	return s, nil
}
