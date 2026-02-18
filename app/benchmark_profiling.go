//go:build benchmark

package app

import "runtime"

func init() {
	// Enable block profiling: record blocking events lasting 1us or longer.
	// Lower values capture more events but add overhead that can skew TPS.
	// This lets /debug/pprof/block show time spent waiting on channels and mutexes.
	runtime.SetBlockProfileRate(1000)

	// Enable mutex contention profiling: sample 1 in 5 contention events.
	// Full capture (fraction=1) adds measurable overhead; 1/5 is a good balance.
	// This lets /debug/pprof/mutex show where goroutines contend on locks.
	runtime.SetMutexProfileFraction(5)
}
