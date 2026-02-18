//go:build benchmark

package node

import (
	"net/http"

	"github.com/felixge/fgprof"
)

func init() {
	// Register fgprof handler on DefaultServeMux alongside net/http/pprof.
	// fgprof captures both on-CPU and off-CPU (I/O, blocking) time, unlike
	// the standard CPU profiler which only sees on-CPU time.
	http.DefaultServeMux.Handle("/debug/fgprof", fgprof.Handler())
}
