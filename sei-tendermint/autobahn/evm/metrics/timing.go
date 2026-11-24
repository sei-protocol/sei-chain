package metrics

import "time"

// Observe records a method duration when the returned function is called.
func Observe(method, kind string) func() {
	observer := Global.MethodTimingAt(method, kind)
	start := time.Now()
	return func() { observer.Observe(time.Since(start).Seconds()) }
}
