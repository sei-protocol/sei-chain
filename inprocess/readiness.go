//go:build inprocess

package inprocess

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Readiness probes mirror the SDK's sei.WaitHeightAdvances / sei.WaitEVMServing
// (sdk/sei/readiness.go). They are duplicated rather than imported because the
// SDK module declares a newer go toolchain than sei-chain builds with (see
// doc.go); when that skew is resolved the harness should delegate to the SDK
// helpers and drop these. Kept stdlib-only and behavior-compatible so the swap
// is mechanical.

// probeInterval is the readiness poll cadence.
const probeInterval = 500 * time.Millisecond

// waitHeightAdvances blocks until tmRPC's committed height rises by >= delta
// from the first successful read — proof the chain is producing blocks, not
// merely reachable (a stalled node reports catching_up == false at a frozen
// height). ctx bounds the wait.
func waitHeightAdvances(ctx context.Context, hc *http.Client, tmRPC string, delta int64) error {
	tick := time.NewTicker(probeInterval)
	defer tick.Stop()
	var start, last int64 = -1, -1
	for {
		if h, ok := latestHeight(ctx, hc, tmRPC); ok {
			if start < 0 {
				start = h
			}
			last = h
			if h >= start+delta {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s height did not advance +%d (start=%d last=%d): %w", tmRPC, delta, start, last, ctx.Err())
		case <-tick.C:
		}
	}
}

// waitEVMServing blocks until evmRPC answers eth_blockNumber with a non-empty,
// error-free result — proof the EVM JSON-RPC listener is bound and serving — or
// until ctx fires. A rare EVM port-bind collision panics the node's serve
// goroutine (the production fail-loud path); the harness does not divert it, so
// here it surfaces only as a poll that never succeeds before the deadline.
func waitEVMServing(ctx context.Context, hc *http.Client, evmRPC string) error {
	const body = `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`
	tick := time.NewTicker(probeInterval)
	defer tick.Stop()
	for {
		if raw, ok := getJSON(ctx, hc, http.MethodPost, evmRPC, body); ok {
			var r struct {
				Result string `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error,omitempty"`
			}
			if json.Unmarshal(raw, &r) == nil && r.Error == nil && r.Result != "" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s eth_blockNumber not serving before deadline: %w", evmRPC, ctx.Err())
		case <-tick.C:
		}
	}
}

// latestHeight reads tmRPC's committed block height from /status. ok=false on an
// unreachable endpoint or unparseable body. The in-process node's HTTP /status
// returns the UNWRAPPED shape (top-level sync_info); the enveloped result.sync_info
// branch covers the standard JSON-RPC shape. Both are live — keep both (an
// enveloped-only parse hangs WaitReady against the in-process node).
func latestHeight(ctx context.Context, hc *http.Client, tmRPC string) (int64, bool) {
	body, ok := getJSON(ctx, hc, http.MethodGet, tmRPC+"/status", "")
	if !ok {
		return 0, false
	}
	var s struct {
		Result *struct {
			SyncInfo syncInfo `json:"sync_info"`
		} `json:"result,omitempty"`
		SyncInfo syncInfo `json:"sync_info"`
	}
	if json.Unmarshal(body, &s) != nil {
		return 0, false
	}
	si := s.SyncInfo
	if s.Result != nil && s.Result.SyncInfo.LatestBlockHeight != "" {
		si = s.Result.SyncInfo
	}
	h, err := strconv.ParseInt(si.LatestBlockHeight, 10, 64)
	if err != nil {
		return 0, false
	}
	return h, true
}

type syncInfo struct {
	LatestBlockHeight string `json:"latest_block_height"`
}

// getJSON performs one request and returns the body on HTTP 200, else ok=false
// (a connection error or non-200 just means "not ready yet").
func getJSON(ctx context.Context, hc *http.Client, method, url, body string) ([]byte, bool) {
	if hc == nil {
		hc = http.DefaultClient
	}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, false
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	return out, true
}
