package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof" //nolint:gosec // the profiling endpoint is the point; it is opt-in via its address
	"runtime"
	"time"
)

// StartPprofServer serves the net/http/pprof endpoints on addr, and turns on the mutex and block
// profiles at the given sample rates, 0 leaving a profile off. It returns the address it bound, or ""
// when addr is empty, and shuts down when ctx is cancelled.
func StartPprofServer(
	ctx context.Context,
	addr string,
	mutexProfileFraction int,
	blockProfileRate int,
) (string, error) {
	if addr == "" {
		return "", nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listen on pprof address %q: %w", addr, err)
	}

	if mutexProfileFraction > 0 {
		runtime.SetMutexProfileFraction(mutexProfileFraction)
	}
	if blockProfileRate > 0 {
		runtime.SetBlockProfileRate(blockProfileRate)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// No write timeout: a CPU profile or trace holds its response open for the whole collection.
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		_ = srv.Serve(listener)
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	return listener.Addr().String(), nil
}
