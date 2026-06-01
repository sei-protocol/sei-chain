//go:build autobahn_integration

package autobahn

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	rpcOnlyContainer    = "sei-rpc-node"
	rpcOnlyBootTimeout  = 5 * time.Minute
	rpcOnlyBootPoll     = 5 * time.Second
	rpcOnlyReceiptPoll  = 500 * time.Millisecond
	rpcOnlyReceiptLimit = 60 * time.Second

	// evmRPCURLOnContainerLocalhost is the EVM RPC address inside a
	// docker container — used with `docker exec ... curl` to reach the
	// rpc-only sidecar's own EVM RPC (it isn't host-published).
	evmRPCURLOnContainerLocalhost = "http://localhost:8545"
	// validatorEVMRPCURLOnHost is sei-node-0's EVM RPC, host-published
	// at 8545 via docker-compose. Used from the test host directly.
	validatorEVMRPCURLOnHost = "http://localhost:8545"
)

// setupRPCOnlyNode boots an autobahn rpc-only sidecar alongside the validator
// cluster. Backgrounded via cmd.Start() because `make run-rpc-node` uses
// `docker run --rm` (foreground until the container exits); the actual
// docker container detaches from this process once it starts.
//
// AUTOBAHN=true triggers the rpc-node's step1 autobahn.json generation. The
// rpc-only role itself comes from mode = "full" in docker/rpcnode/config/
// config.toml (see IsAutobahnRPCOnly in sei-tendermint config).
func setupRPCOnlyNode() error {
	fmt.Println("=== Starting rpc-only sidecar ===")
	_ = runMake(nil, "kill-rpc-node") // best-effort cleanup

	cmd := exec.Command("make", "run-rpc-node")
	cmd.Env = append(os.Environ(), "AUTOBAHN=true", "CLUSTER_SIZE=4")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start make run-rpc-node: %w", err)
	}
	// Reap the process when it eventually exits (e.g. on container kill); not
	// blocking on Wait here since the container runs for the duration of the
	// test suite.
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(rpcOnlyBootTimeout)
	for time.Now().Before(deadline) {
		if rpcOnlyRunning() && rpcOnlyEVMReady() {
			fmt.Println("rpc-only sidecar is ready")
			return nil
		}
		time.Sleep(rpcOnlyBootPoll)
	}
	return fmt.Errorf("rpc-only sidecar didn't come up within %s", rpcOnlyBootTimeout)
}

func rpcOnlyRunning() bool {
	out, err := exec.Command("docker", "ps",
		"--filter", "name="+rpcOnlyContainer,
		"--filter", "status=running",
		"--format", "{{.Names}}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == rpcOnlyContainer
}

func rpcOnlyEVMReady() bool {
	r, err := evmRPCInContainer(rpcOnlyContainer, "eth_chainId", []any{})
	return err == nil && r.Error == nil && len(r.Result) > 0
}

type evmRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *evmRPCError    `json:"error,omitempty"`
}

type evmRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// evmRPCInContainer POSTs a JSON-RPC call to the given container's
// localhost:8545. The rpc-only container's 8545 isn't host-published; this
// is the only way to talk to it without changing the run target.
func evmRPCInContainer(container, method string, params any) (*evmRPCResponse, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	out, err := exec.Command("docker", "exec", container,
		"curl", "-sf", "-X", "POST",
		"-H", "content-type: application/json",
		"--data", string(body),
		evmRPCURLOnContainerLocalhost).Output()
	if err != nil {
		return nil, fmt.Errorf("docker exec curl: %v", err)
	}
	var r evmRPCResponse
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("decode (body=%s): %w", out, err)
	}
	return &r, nil
}

// evmRPCOnHost POSTs a JSON-RPC call to a validator's host-published 8545.
func evmRPCOnHost(method string, params any) (*evmRPCResponse, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(validatorEVMRPCURLOnHost, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r evmRPCResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode (body=%s): %w", raw, err)
	}
	return &r, nil
}

// extractAdminPrivKey reads admin's secp256k1 privkey from sei-node-0's
// local keyring. This is the only cosmos-side touch in the test — purely a
// file decryption, no chain interaction.
func extractAdminPrivKey(t *testing.T) string {
	t.Helper()
	out := dockerExec(t, "sei-node-0",
		`(echo y; echo 12345678) | seid keys export admin --unsafe --unarmored-hex 2>/dev/null`)
	return strings.TrimSpace(out)
}

// associateAdmin calls sei_associate via curl on a validator. Same JSON-RPC
// method that `seid tx evm associate-address` wraps under the hood; the
// chain records the cosmos↔EVM mapping so admin's cosmos balance becomes
// visible at its derived EVM address. Idempotent: returns nil if admin is
// already associated.
func associateAdmin(t *testing.T, privHex string) {
	t.Helper()
	priv, err := crypto.HexToECDSA(privHex)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	emptyHash := crypto.Keccak256Hash([]byte{})
	sig, err := crypto.Sign(emptyHash[:], priv)
	if err != nil {
		t.Fatalf("crypto.Sign: %v", err)
	}
	// sig layout: [R(32) | S(32) | V(1)]. V is the recovery byte (0 or 1).
	// Encode R/S/V via big.Int.Bytes() so leading zeros are trimmed —
	// matches the wire format `seid tx evm associate-address` uses (see
	// x/evm/client/cli/tx.go). The chain verifies the signature against
	// the exact byte representation it received, so encoding mismatch
	// (e.g. V=0 → "" vs "00") makes CheckTx reject the tx.
	r := hex.EncodeToString(new(big.Int).SetBytes(sig[:32]).Bytes())
	s := hex.EncodeToString(new(big.Int).SetBytes(sig[32:64]).Bytes())
	v := hex.EncodeToString(big.NewInt(int64(sig[64])).Bytes())
	resp, err := evmRPCOnHost("sei_associate", []any{map[string]string{"v": v, "r": r, "s": s}})
	if err != nil {
		t.Fatalf("sei_associate: %v", err)
	}
	if resp.Error != nil {
		msg := strings.ToLower(resp.Error.Message)
		if !strings.Contains(msg, "already associated") && !strings.Contains(msg, "tx already exists") {
			t.Fatalf("sei_associate error: %s", resp.Error.Message)
		}
	}
}

// testRPCOnlyForwarding drives the whole client-facing flow through the
// rpc-only sidecar's EVM RPC: balance / chain-id / nonce reads, the
// eth_sendRawTransaction submit, and the receipt poll all hit the rpc-only.
// This exercises both the write path (proxy to the shard owner) and the
// read path (block-sync subscriber feeds runExecute → local data.State →
// eth_getTransactionReceipt). The only validator-side call is sei_associate
// (a write that creates a cosmos tx — the rpc-only doesn't proxy it
// today, only eth_sendRawTransaction).
//
// If block sync stalls on the rpc-only, the read polls below time out:
// LastBlockHeight stays at 0, the EVM gate never fires, and locally-served
// reads never reflect the chain even though the tx landed on the cluster.
func testRPCOnlyForwarding(t *testing.T) {
	assertAutobahnEnabled(t)

	// 1. Extract admin's privkey from the validator container's keyring.
	adminPrivHex := extractAdminPrivKey(t)
	priv, err := crypto.HexToECDSA(adminPrivHex)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey)
	t.Logf("admin EVM address: %s", addr.Hex())

	// 2. Associate admin if its EVM-side balance (as seen by the rpc-only)
	//    is still 0. Skipping when balance > 0 keeps the test idempotent
	//    across re-runs. sei_associate goes to the validator because the
	//    rpc-only's HTTP proxy only handles eth_sendRawTransaction; the
	//    rest of the test reads exclusively from the rpc-only.
	if balanceOnRPCOnly(t, addr).Sign() == 0 {
		associateAdmin(t, adminPrivHex)
	}

	// 3. Wait for admin's EVM balance to materialize on the rpc-only —
	//    requires the rpc-only's block-sync subscriber to pull blocks
	//    through the associate height and runExecute to commit them
	//    locally. 60s is generous; in practice this lands in under 10s
	//    once the cluster has been producing blocks.
	deadline := time.Now().Add(60 * time.Second)
	var bal *big.Int
	for time.Now().Before(deadline) {
		bal = balanceOnRPCOnly(t, addr)
		if bal.Sign() > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	if bal.Sign() == 0 {
		t.Fatalf("admin balance never appeared on rpc-only at %s", addr.Hex())
	}
	t.Logf("admin balance (rpc-only view): %s wei", bal)

	// 4. Build, sign, and submit a 0-value self-transfer via the rpc-only.
	//    chainID and nonce are read from the rpc-only too — same chain
	//    state the user-facing operator would see.
	chainID := chainIDOnRPCOnly(t)
	nonce := nonceOnRPCOnly(t, addr)
	gasPrice := new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000)) // 100 gwei
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      100_000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     nil,
	})
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(chainID), priv)
	if err != nil {
		t.Fatalf("SignTx: %v", err)
	}
	raw, err := signedTx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	expected := signedTx.Hash()
	rawHex := "0x" + hex.EncodeToString(raw)

	submit, err := evmRPCOnRPCOnly("eth_sendRawTransaction", []any{rawHex})
	if err != nil {
		t.Fatalf("rpc-only eth_sendRawTransaction: %v", err)
	}
	if submit.Error != nil {
		t.Fatalf("rpc-only rejected the tx: %s", submit.Error.Message)
	}
	var returnedHash string
	if err := json.Unmarshal(submit.Result, &returnedHash); err != nil {
		t.Fatalf("decode hash: %v (raw=%s)", err, submit.Result)
	}
	if !strings.EqualFold(returnedHash, expected.Hex()) {
		t.Fatalf("rpc-only returned hash %s, expected %s", returnedHash, expected.Hex())
	}

	// 5. Poll the rpc-only's own EVM RPC for the receipt. The receipt can
	//    only appear here if the rpc-only's block-sync subscriber pulled
	//    the finalized block from a committee member and runExecute pushed
	//    it through the local EVM ledger.
	waitForReceipt(t, expected.Hex(), evmRPCOnRPCOnly, "rpc-only")
}

// evmRPCOnRPCOnly POSTs a JSON-RPC call to the rpc-only sidecar's EVM RPC.
// Thin wrapper around evmRPCInContainer so the test reads exclusively from
// the rpc-only endpoint by default.
func evmRPCOnRPCOnly(method string, params any) (*evmRPCResponse, error) {
	return evmRPCInContainer(rpcOnlyContainer, method, params)
}

type evmReceiptShape struct {
	Status      string `json:"status"`
	BlockNumber string `json:"blockNumber"`
}

// waitForReceipt polls the given eth_getTransactionReceipt source until a
// non-null receipt appears or rpcOnlyReceiptLimit elapses. Fails the test
// on timeout or on a non-success status, prefixing log/fail messages with
// the source label so failures point at the right endpoint.
func waitForReceipt(
	t *testing.T,
	txHash string,
	call func(string, any) (*evmRPCResponse, error),
	source string,
) evmReceiptShape {
	t.Helper()
	deadline := time.Now().Add(rpcOnlyReceiptLimit)
	for time.Now().Before(deadline) {
		r, err := call("eth_getTransactionReceipt", []any{txHash})
		if err == nil && r.Error == nil && len(r.Result) > 0 && string(r.Result) != "null" {
			var receipt evmReceiptShape
			if err := json.Unmarshal(r.Result, &receipt); err != nil {
				t.Fatalf("%s: decode receipt: %v (raw=%s)", source, err, r.Result)
			}
			t.Logf("%s: tx %s landed in block %s (status=%s)",
				source, txHash, receipt.BlockNumber, receipt.Status)
			if receipt.Status != "0x1" {
				t.Fatalf("%s: tx reverted (status=%s)", source, receipt.Status)
			}
			return receipt
		}
		time.Sleep(rpcOnlyReceiptPoll)
	}
	t.Fatalf("%s: receipt never landed for tx %s", source, txHash)
	return evmReceiptShape{} // unreachable
}

// balanceOnRPCOnly fetches the EVM balance at addr from the rpc-only's
// own RPC — i.e. from its local data.State after runExecute has applied
// any blocks containing the relevant tx.
func balanceOnRPCOnly(t *testing.T, addr common.Address) *big.Int {
	t.Helper()
	resp, err := evmRPCOnRPCOnly("eth_getBalance", []any{addr.Hex(), "latest"})
	if err != nil {
		t.Fatalf("eth_getBalance: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_getBalance: %s", resp.Error.Message)
	}
	var s string
	if err := json.Unmarshal(resp.Result, &s); err != nil {
		t.Fatalf("decode balance: %v", err)
	}
	b, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok {
		t.Fatalf("parse balance hex %q", s)
	}
	return b
}

func chainIDOnRPCOnly(t *testing.T) *big.Int {
	t.Helper()
	resp, err := evmRPCOnRPCOnly("eth_chainId", []any{})
	if err != nil {
		t.Fatalf("eth_chainId: %v", err)
	}
	var s string
	if err := json.Unmarshal(resp.Result, &s); err != nil {
		t.Fatalf("decode chainId: %v", err)
	}
	c, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok {
		t.Fatalf("parse chainId hex %q", s)
	}
	return c
}

func nonceOnRPCOnly(t *testing.T, addr common.Address) uint64 {
	t.Helper()
	resp, err := evmRPCOnRPCOnly("eth_getTransactionCount", []any{addr.Hex(), "pending"})
	if err != nil {
		t.Fatalf("eth_getTransactionCount: %v", err)
	}
	var s string
	if err := json.Unmarshal(resp.Result, &s); err != nil {
		t.Fatalf("decode nonce: %v", err)
	}
	hexStr := strings.TrimPrefix(s, "0x")
	if hexStr == "" {
		return 0
	}
	n, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		t.Fatalf("parse nonce hex %q: %v", s, err)
	}
	return n
}
