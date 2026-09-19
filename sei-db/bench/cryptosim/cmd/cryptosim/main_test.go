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

// Asserts that every profile the benchmark is expected to expose is actually reachable, since the
// alternative is discovering a missing handler after a remote run has already been spent.
func TestPprofServerServesProfiles(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.PprofAddr = "127.0.0.1:0"
	config.MutexProfileFraction = 1
	config.BlockProfileRate = 1

	// The sample rates are process-wide, so leaving them on would silently slow every later test.
	t.Cleanup(func() {
		runtime.SetMutexProfileFraction(0)
		runtime.SetBlockProfileRate(0)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startPprofServer(ctx, config)
	require.NoError(t, err)
	require.NotEmpty(t, addr)

	for _, path := range []string{
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

// The metrics server is a separate server on a separate address, because it starts only once setup
// is done. It must serve the scrape and nothing else.
func TestMetricsServerServesOnlyMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startMetricsServer(ctx, testRegistry(t), "127.0.0.1:0")
	require.NoError(t, err)
	require.NotEmpty(t, addr)

	status, body := get(t, addr, "/metrics")
	require.Equal(t, http.StatusOK, status)
	require.NotEmpty(t, body)

	status, _ = get(t, addr, "/debug/pprof/")
	require.Equal(t, http.StatusNotFound, status, "the profiles belong to the pprof server alone")
}

// Asserts that an empty address starts nothing, which is how a run opts out of either server.
func TestServersDisabled(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.PprofAddr = ""

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startPprofServer(ctx, config)
	require.NoError(t, err)
	require.Empty(t, addr)

	addr, err = startMetricsServer(ctx, testRegistry(t), "")
	require.NoError(t, err)
	require.Empty(t, addr)
}

// Asserts that a port already in use is reported rather than swallowed, which the metrics server did
// before it bound up front.
func TestServersReportBindFailure(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.PprofAddr = "127.0.0.1:0"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	taken, err := startPprofServer(ctx, config)
	require.NoError(t, err)

	config.PprofAddr = taken
	_, err = startPprofServer(ctx, config)
	require.Error(t, err)

	_, err = startMetricsServer(ctx, testRegistry(t), taken)
	require.Error(t, err)
}
