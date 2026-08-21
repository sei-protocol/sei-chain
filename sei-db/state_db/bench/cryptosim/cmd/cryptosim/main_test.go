package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/cryptosim"
	"github.com/stretchr/testify/require"
)

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

	client := &http.Client{Timeout: 10 * time.Second}
	for _, path := range []string{
		"/debug/pprof/",
		"/debug/pprof/goroutine?debug=1",
		"/debug/pprof/heap",
		"/debug/pprof/mutex",
		"/debug/pprof/block",
		"/debug/pprof/cmdline",
	} {
		url := fmt.Sprintf("http://%s%s", addr, path)
		response, err := client.Get(url)
		require.NoError(t, err, "GET %s", path)

		body, err := io.ReadAll(response.Body)
		require.NoError(t, response.Body.Close())
		require.NoError(t, err, "read %s", path)
		require.Equal(t, http.StatusOK, response.StatusCode, "GET %s", path)
		require.NotEmpty(t, body, "GET %s returned an empty body", path)
	}
}

// Asserts that an empty PprofAddr starts nothing, which is how a run opts out.
func TestPprofServerDisabled(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.PprofAddr = ""

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startPprofServer(ctx, config)
	require.NoError(t, err)
	require.Empty(t, addr)
}

// Asserts that a port already in use is reported rather than swallowed.
func TestPprofServerReportsBindFailure(t *testing.T) {
	config := cryptosim.DefaultCryptoSimConfig()
	config.PprofAddr = "127.0.0.1:0"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := startPprofServer(ctx, config)
	require.NoError(t, err)

	config.PprofAddr = addr
	_, err = startPprofServer(ctx, config)
	require.Error(t, err)
}
