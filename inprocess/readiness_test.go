//go:build inprocess

package inprocess

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestWaitEVMServingSurfacesServeErr proves a reported EVM listener-start failure
// short-circuits waitEVMServing with the actual error, rather than polling the
// unreachable endpoint until the ctx deadline and returning a generic timeout.
// The EVM URL points at a closed loopback port so the poll never succeeds; the
// pre-seeded serveErr channel stands in for app.reportEVMServeErr's divert.
func TestWaitEVMServingSurfacesServeErr(t *testing.T) {
	serveErr := make(chan error, 2) // matches the node's HTTP+WS buffer
	bindErr := errors.New("listen tcp 0.0.0.0:8545: bind: address already in use")
	serveErr <- bindErr

	// Generous ctx: if the short-circuit failed we'd block on the poll loop until
	// this fires, so the test would catch a regression as a timeout-shaped error.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hc := &http.Client{Timeout: time.Second}
	start := time.Now()
	err := waitEVMServing(ctx, hc, "http://127.0.0.1:1", serveErr)
	if err == nil {
		t.Fatal("waitEVMServing returned nil; want the reported serve error")
	}
	if !errors.Is(err, bindErr) {
		t.Fatalf("error does not wrap the reported serve error: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "not serving before deadline") {
		t.Fatalf("got a generic timeout, want the real serve error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("short-circuit took %v; expected near-immediate", elapsed)
	}

	// Non-destructive contract: the error is re-sent, so a later ServeErr()-style
	// read still observes it.
	select {
	case got := <-serveErr:
		if !errors.Is(got, bindErr) {
			t.Fatalf("re-sent error = %v, want %v", got, bindErr)
		}
	default:
		t.Fatal("serveErr drained: readiness consumption was destructive")
	}
}
