// Package ws_test exercises WebSocket JSON-RPC subscriptions against a
// live Sei EVM RPC. The test is consensus-mode agnostic: it dials the
// EVM WS port and asserts that eth_subscribe("newHeads") and
// eth_subscribe("logs") deliver notifications. It runs under both
// standard CometBFT clusters and Autobahn clusters — the newHeads
// producer hook differs between them (legacy event bus vs in-process
// notifier), but the externally observable behaviour must be identical.
//
// Env:
//   - SEI_EVM_WS_RUN_INTEGRATION=1 to run (set by integration scripts/CI).
//     Otherwise the test skips so `go test ./...` stays cheap.
//   - SEI_EVM_WS_URL overrides the default ws://127.0.0.1:8546.
//   - SEI_EVM_WS_EMITTER_ADDRESS + SEI_EVM_WS_EMITTER_BLOCK: address +
//     deploy block of the emitter contract whose constructor emits one
//     LOG1; required by TestEthSubscribeLogs. The integration script
//     deploys the contract and exports both vars; missing vars cause the
//     logs test to skip rather than fail.
package ws_test

import (
	"os"
	"strings"
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

	// Wait for at least one head notification. At Sei's block cadence
	// this should arrive within a few seconds; allow generous slack.
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

func TestEthSubscribeLogs(t *testing.T) {
	if os.Getenv("SEI_EVM_WS_RUN_INTEGRATION") != "1" {
		t.Skip("EVM WS integration tests skipped (set SEI_EVM_WS_RUN_INTEGRATION=1 to run)")
	}
	emitterAddr := os.Getenv("SEI_EVM_WS_EMITTER_ADDRESS")
	emitterBlock := os.Getenv("SEI_EVM_WS_EMITTER_BLOCK")
	if emitterAddr == "" || emitterBlock == "" {
		t.Skip("emitter contract address/block not set (script-side deploy failed?); skipping eth_subscribe(logs) integration test")
	}

	url := wsURL()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	defer conn.Close()

	// Subscribe to logs in the emitter's deploy block. The constructor
	// emits one LOG1 with topic 0x4242…4242 from the new contract's
	// address, so the deploy block receipt contains exactly one matching
	// log.
	if err = conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_subscribe",
		"params": []interface{}{
			"logs",
			map[string]interface{}{
				"fromBlock": emitterBlock,
				"toBlock":   emitterBlock,
				"address":   emitterAddr,
			},
		},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

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

	// Server polls historical block ranges on the first iteration with no
	// leading sleep, so the queued log for the deploy block should arrive
	// well within the deadline.
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
		t.Fatalf("read log notification: %v", err)
	}
	if note.Method != "eth_subscription" {
		t.Fatalf("expected eth_subscription, got %q", note.Method)
	}
	if note.Params.Subscription != ack.Result {
		t.Fatalf("subscription id mismatch: got %q want %q",
			note.Params.Subscription, ack.Result)
	}
	log := note.Params.Result
	gotAddr, _ := log["address"].(string)
	if !strings.EqualFold(gotAddr, emitterAddr) {
		t.Fatalf("log address: got %q want %q", gotAddr, emitterAddr)
	}
	if gotBlock, _ := log["blockNumber"].(string); !strings.EqualFold(gotBlock, emitterBlock) {
		t.Fatalf("log blockNumber: got %q want %q", gotBlock, emitterBlock)
	}
	topics, ok := log["topics"].([]interface{})
	if !ok || len(topics) != 1 {
		t.Fatalf("expected exactly 1 topic, got %+v", log["topics"])
	}
	const wantTopic = "0x4242424242424242424242424242424242424242424242424242424242424242"
	if gotTopic, _ := topics[0].(string); !strings.EqualFold(gotTopic, wantTopic) {
		t.Fatalf("log topic: got %q want %q", gotTopic, wantTopic)
	}
	t.Logf("received log: addr=%v block=%v topic=%v", log["address"], log["blockNumber"], topics[0])
}
