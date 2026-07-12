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
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func wsURL() string {
	if u := os.Getenv("SEI_EVM_WS_URL"); u != "" {
		return u
	}
	return "ws://127.0.0.1:8546"
}

func triggerHead(t *testing.T) {
	t.Helper()

	container := os.Getenv("SEI_EVM_WS_TX_CONTAINER")
	if container == "" {
		container = "sei-node-0"
	}
	password := os.Getenv("SEI_EVM_WS_TX_PASSWORD")
	if password == "" {
		password = "12345678"
	}
	from := os.Getenv("SEI_EVM_WS_TX_FROM")
	if from == "" {
		from = "admin"
	}
	recipient := os.Getenv("SEI_EVM_WS_TX_RECIPIENT")
	if recipient == "" {
		recipient = "0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52"
	}
	evmRPCURL := os.Getenv("SEI_EVM_WS_TX_EVM_RPC_URL")
	if evmRPCURL == "" {
		evmRPCURL = "http://localhost:8545"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		"docker", "exec",
		"--env", fmt.Sprintf("SEI_EVM_WS_PASSWORD=%s", password),
		container,
		"/bin/bash", "-c",
		`export PATH=$PATH:/root/go/bin && printf "%s\n" "$SEI_EVM_WS_PASSWORD" | "$@"`,
		"bash",
		"seid", "tx", "evm", "send", recipient, "1",
		"--from", from,
		"--chain-id", "sei",
		"--evm-rpc", evmRPCURL,
		"-b", "sync",
		"-y",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trigger head tx: %v\n%s", err, out)
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
