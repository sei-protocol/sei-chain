// Package gasbench measures per-opcode EVM execution time and correlates it
// with gas cost via differential microbenchmarking. See README.md for the
// full methodology and rationale.
package gasbench

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"
)

// RunOnce executes a pre-loaded EVM program to completion through the
// tracer-free interpreter and reports the gas it consumed. It closes over
// its program (see Program.Run) so the timing loop never rebuilds
// interpreter/StateDB state per call.
//
// Contract:
//   - Deterministic: identical program yields identical gasUsed and
//     equivalent work every call.
//   - Self-contained and tracer-free: no I/O, logging, or tracer on the hot
//     path.
//   - Sufficient work: total runtime well above timer resolution
//     (target >= ~10us).
//   - Allocation-light: GC is disabled during the window (see Measure), so
//     allocations across warmup+Iterations must fit in RAM.
type RunOnce func() (gasUsed uint64, err error)

// sink defeats dead-code elimination of the gas result. Safe without
// synchronization only because BenchmarkOpcodes's subtests run sequentially
// (no b.Parallel) -- GOMAXPROCS=1 and thread-locking do not by themselves
// make the ^= below race-free, and do not protect this var if a future
// change parallelizes the subtests.
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

	// NvcswDelta/NivcswDelta are process-wide (RUSAGE_SELF) voluntary/
	// involuntary context-switch counts over the timed window (Warmup
	// excluded). See README.md "Active-benchmarking diagnostics".
	NvcswDelta  int64
	NivcswDelta int64
}

// rusageSnapshot reads the process's current voluntary/involuntary
// context-switch counters (process-wide, not thread-scoped: Go's syscall
// package exposes no portable RUSAGE_THREAD).
func rusageSnapshot() (nvcsw, nivcsw int64, err error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, 0, fmt.Errorf("gasbench: getrusage: %w", err)
	}
	return ru.Nvcsw, ru.Nivcsw, nil
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
