// Package ws_test exercises WebSocket JSON-RPC subscriptions against a
// live Sei EVM RPC. The test is consensus-mode agnostic: it dials the
// EVM WS port and asserts that eth_subscribe("newHeads") delivers a
// head notification. It runs under both standard CometBFT clusters and
// Autobahn clusters — the producer hook differs between them (legacy
// event bus vs in-process notifier), but the externally observable
// behaviour must be identical.
//
// Env:
//   - SEI_EVM_WS_RUN_INTEGRATION=1 to run (set by integration scripts/CI).
//     Otherwise the test skips so `go test ./...` stays cheap.
//   - SEI_EVM_WS_URL overrides the default ws://127.0.0.1:8546.
package ws_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sei-protocol/sei-chain/testutil/evmtest"
)

func wsURL() string {
	if u := os.Getenv("SEI_EVM_WS_URL"); u != "" {
		return u
	}
	return "ws://127.0.0.1:8546"
}

func triggerHead(t *testing.T) {
	t.Helper()
	// Progress-only tx: newHeads needs one real block after subscription, and
	// under allow_empty_blocks=false we must create that block explicitly.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := evmtest.SendTinyEvmTx(ctx, evmtest.ConfigFromEnv("SEI_EVM_WS_TX_")); err != nil {
		t.Fatalf("trigger head tx: %v", err)
	}
}

func TestEthSubscribeNewHeads(t *testing.T) {
	if os.Getenv("SEI_EVM_WS_RUN_INTEGRATION") != "1" {
		t.Skip("EVM WS integration tests skipped (set SEI_EVM_WS_RUN_INTEGRATION=1 to run)")
	}

	url := wsURL()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	defer conn.Close()

	if err = conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_subscribe",
		"params":  []string{"newHeads"},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	// First message must be the subscription confirmation. Bound the wait
	// so a broken handshake fails fast.
	if err = conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	var ack struct {
		Result string                 `json:"result"`
		Error  map[string]interface{} `json:"error"`
	}
	if err = conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read subscribe ack: %v", err)
	}
	if ack.Error != nil {
		t.Fatalf("subscribe error: %v", ack.Error)
	}
	if ack.Result == "" {
		t.Fatalf("subscribe returned empty subscription id")
	}
	t.Logf("subscription id: %s", ack.Result)

	// Drive a real block after subscribing so Autobahn does not depend on
	// idle block production to emit a new head notification.
	triggerHead(t)

	// Wait for the resulting head notification.
	if err = conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	var note struct {
		Method string `json:"method"`
		Params struct {
			Subscription string                 `json:"subscription"`
			Result       map[string]interface{} `json:"result"`
		} `json:"params"`
	}
	if err = conn.ReadJSON(&note); err != nil {
		t.Fatalf("read head notification: %v", err)
	}
	if note.Method != "eth_subscription" {
		t.Fatalf("expected eth_subscription, got %q", note.Method)
	}
	if note.Params.Subscription != ack.Result {
		t.Fatalf("subscription id mismatch: got %q want %q",
			note.Params.Subscription, ack.Result)
	}
	header := note.Params.Result
	for _, key := range []string{"hash", "number", "timestamp", "stateRoot", "miner"} {
		v, ok := header[key]
		if !ok {
			t.Fatalf("head notification missing key %q (got %+v)", key, header)
		}
		// All-zero values for hash, number, or timestamp would indicate
		// the producer hook didn't fire and we're seeing a default-
		// constructed header.
		if s, _ := v.(string); s == "" || s == "0x" || s == "0x0" {
			if key == "hash" || key == "number" || key == "timestamp" {
				t.Fatalf("head notification %q has zero-ish value %q", key, s)
			}
		}
	}
	t.Logf("received head: number=%v hash=%v", header["number"], header["hash"])
}
