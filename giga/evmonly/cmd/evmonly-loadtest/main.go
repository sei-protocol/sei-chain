package main

import (
	"fmt"
	"os"
	"runtime/debug"
)

// defaultGCPercent leaves Go's own default in place. Raising it trades memory for throughput, but
// how much of either depends on the host, so the value belongs to whoever knows the machine.
const defaultGCPercent = 0

// applyGCPercent raises the GC target, leaving an explicitly configured GOGC alone so the
// environment stays authoritative.
//
// Nothing here bounds the heap that results. On a host with a memory limit — a container, a pod —
// pair a raised target with GOMEMLIMIT, or the collector will run late enough to be killed.
func applyGCPercent(percent int) {
	if percent <= 0 {
		return
	}
	if _, set := os.LookupEnv("GOGC"); set {
		return
	}
	debug.SetGCPercent(percent)
}

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "evmonly-loadtest: %v\n", err)
		os.Exit(2)
	}
	applyGCPercent(cfg.gcPercent)
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "evmonly-loadtest: %v\n", err)
		os.Exit(1)
	}
}
