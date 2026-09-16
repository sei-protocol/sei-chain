package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/bench/cryptosim"
)

// testRegistry returns a registry holding the Go collector, so that a scrape has something to report
// as it does in a real run.
func testRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	return reg
}

// get fetches a path from the server and returns its status and body.
func get(t *testing.T, addr string, path string) (int, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://%s%s", addr, path))
	require.NoError(t, err, "GET %s", path)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, response.Body.Close())
	require.NoError(t, err, "read %s", path)
	return response.StatusCode, body
}

// Asserts that the metrics and every profile the benchmark is expected to expose share one address,
// since the alternative is discovering a missing handler after a remote run has already been spent.
func TestHTTPServerServesMetricsAndProfiles(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.MetricsAddr = "127.0.0.1:0"
	config.EnablePprof = true
	config.MutexProfileFraction = 1
	config.BlockProfileRate = 1

	// The sample rates are process-wide, so leaving them on would silently slow every later test.
	t.Cleanup(func() {
		runtime.SetMutexProfileFraction(0)
		runtime.SetBlockProfileRate(0)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startHTTPServer(ctx, testRegistry(t), config)
	require.NoError(t, err)
	require.NotEmpty(t, addr)

	for _, path := range []string{
		"/metrics",
		"/debug/pprof/",
		"/debug/pprof/goroutine?debug=1",
		"/debug/pprof/heap",
		"/debug/pprof/mutex",
		"/debug/pprof/block",
		"/debug/pprof/cmdline",
	} {
		status, body := get(t, addr, path)
		require.Equal(t, http.StatusOK, status, "GET %s", path)
		require.NotEmpty(t, body, "GET %s returned an empty body", path)
	}
}

// Asserts that the profiles are genuinely opt-out and that turning them off leaves the metrics alone.
func TestHTTPServerWithoutPprof(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.MetricsAddr = "127.0.0.1:0"
	config.EnablePprof = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startHTTPServer(ctx, testRegistry(t), config)
	require.NoError(t, err)

	status, _ := get(t, addr, "/metrics")
	require.Equal(t, http.StatusOK, status, "metrics must be served with the profiles off")

	status, _ = get(t, addr, "/debug/pprof/")
	require.Equal(t, http.StatusNotFound, status, "the profiles must not be registered when off")
}

// Asserts that an empty MetricsAddr starts nothing, which is how a run serves neither.
func TestHTTPServerDisabled(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.MetricsAddr = ""
	config.EnablePprof = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startHTTPServer(ctx, testRegistry(t), config)
	require.NoError(t, err)
	require.Empty(t, addr)
}

// Asserts that a port already in use is reported rather than swallowed, which it was before the
// server bound up front.
func TestHTTPServerReportsBindFailure(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.MetricsAddr = "127.0.0.1:0"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startHTTPServer(ctx, testRegistry(t), config)
	require.NoError(t, err)

	config.MetricsAddr = addr
	_, err = startHTTPServer(ctx, testRegistry(t), config)
	require.Error(t, err)
}
