//go:build frozen_rpc_integration

// Package frozenrpcrouter verifies the Docker frozen-node routing topology.
package frozenrpcrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	defaultRouterURL   = "http://127.0.0.1:8553"
	defaultFrozen10URL = "http://127.0.0.1:8547"
	defaultFrozen20URL = "http://127.0.0.1:8549"
	defaultLiveURL     = "http://127.0.0.1:8545"
	routeHeader        = "Sei-RPC-Route"
)

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func TestFrozenRPCRouterReachesEveryNode(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	routerURL := envOrDefault("FROZEN_RPC_ROUTER_URL", defaultRouterURL)
	frozen10URL := envOrDefault("FROZEN_RPC_NODE_10_URL", defaultFrozen10URL)
	frozen20URL := envOrDefault("FROZEN_RPC_NODE_20_URL", defaultFrozen20URL)
	liveURL := envOrDefault("FROZEN_RPC_LIVE_NODE_URL", defaultLiveURL)

	waitForHead(t, client, frozen10URL, func(height uint64) bool { return height == 9 }, "frozen node at height 10")
	waitForHead(t, client, frozen20URL, func(height uint64) bool { return height == 19 }, "frozen node at height 20")
	waitForHead(t, client, liveURL, func(height uint64) bool { return height > 20 }, "live node past height 20")
	waitForHead(t, client, routerURL, func(height uint64) bool { return height > 20 }, "router live-node forwarding")

	for _, testCase := range []struct {
		name      string
		height    uint64
		wantRoute string
	}{
		{name: "height 9 reaches first frozen node", height: 9, wantRoute: "frozen:10"},
		{name: "height 15 reaches second frozen node", height: 15, wantRoute: "frozen:20"},
		{name: "height 20 reaches live node", height: 20, wantRoute: "live"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response, route, err := callRPC(t.Context(), client, routerURL, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", testCase.height), false})
			if err != nil {
				t.Fatal(err)
			}
			if response.Error != nil {
				t.Fatalf("RPC returned error %d: %s", response.Error.Code, response.Error.Message)
			}
			if route != testCase.wantRoute {
				t.Fatalf("route header = %q, want %q", route, testCase.wantRoute)
			}
			var block struct {
				Number string `json:"number"`
			}
			if err := json.Unmarshal(response.Result, &block); err != nil {
				t.Fatalf("decode block response: %v", err)
			}
			wantNumber := fmt.Sprintf("0x%x", testCase.height)
			if block.Number != wantNumber {
				t.Fatalf("block number = %q, want %q", block.Number, wantNumber)
			}
		})
	}

	t.Run("range crossing frozen nodes is rejected", func(t *testing.T) {
		response, _, err := callRPC(t.Context(), client, routerURL, "eth_getLogs", []any{map[string]any{
			"fromBlock": "0x9",
			"toBlock":   "0xa",
		}})
		if err != nil {
			t.Fatal(err)
		}
		if response.Error == nil || response.Error.Code != -32000 {
			t.Fatalf("RPC error = %+v, want code -32000", response.Error)
		}
	})
}

func waitForHead(t *testing.T, client *http.Client, endpoint string, ready func(uint64) bool, description string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	var lastHeight uint64
	var lastErr error
	for ctx.Err() == nil {
		response, _, err := callRPC(ctx, client, endpoint, "eth_blockNumber", []any{})
		if err == nil && response.Error == nil {
			var encodedHeight string
			err = json.Unmarshal(response.Result, &encodedHeight)
			if err == nil {
				lastHeight, err = strconv.ParseUint(strings.TrimPrefix(encodedHeight, "0x"), 16, 64)
			}
			if err == nil && ready(lastHeight) {
				return
			}
		} else if err == nil {
			err = fmt.Errorf("RPC error %d: %s", response.Error.Code, response.Error.Message)
		}
		lastErr = err
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	t.Fatalf("timed out waiting for %s at %s (last height %d, last error %v)", description, endpoint, lastHeight, lastErr)
}

func callRPC(ctx context.Context, client *http.Client, endpoint, method string, params any) (rpcResponse, string, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return rpcResponse{}, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return rpcResponse{}, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return rpcResponse{}, "", err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return rpcResponse{}, "", err
	}
	if response.StatusCode != http.StatusOK {
		return rpcResponse{}, "", fmt.Errorf("HTTP status %d: %s", response.StatusCode, body)
	}
	var result rpcResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return rpcResponse{}, "", fmt.Errorf("decode RPC response %q: %w", body, err)
	}
	return result, response.Header.Get(routeHeader), nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
