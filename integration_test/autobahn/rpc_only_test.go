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
	validatorEVMRPCURL  = "http://localhost:8545" // sei-node-0 (host-published)
	rpcOnlyInternalURL  = "http://localhost:8545" // inside sei-rpc-node
	rpcOnlyReceiptPoll  = 500 * time.Millisecond
	rpcOnlyReceiptLimit = 60 * time.Second
)

// setupRPCOnlyNode boots an autobahn rpc-only sidecar alongside the validator
// cluster. Backgrounded via cmd.Start() because `make run-rpc-node` uses
// `docker run --rm` (foreground until the container exits); the actual
// docker container detaches from this process once it starts.
//
// AUTOBAHN=true triggers step1_configure_init.sh's autobahn.json generation
// and writes autobahn-role="rpc-only" into the rpc node's config.toml.
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

// teardownRPCOnlyNode tears down the rpc-only sidecar.
func teardownRPCOnlyNode() {
	fmt.Println("=== Stopping rpc-only sidecar ===")
	_ = runMake(nil, "kill-rpc-node")
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
		rpcOnlyInternalURL).Output()
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
	resp, err := http.Post(validatorEVMRPCURL, "application/json", bytes.NewReader(body))
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
	emptyHash := crypto.Keccak256(nil)
	sig, err := crypto.Sign(emptyHash, priv)
	if err != nil {
		t.Fatalf("crypto.Sign: %v", err)
	}
	// sig layout: [R(32) | S(32) | V(1)]. V is the recovery byte (0 or 1).
	r := hex.EncodeToString(sig[:32])
	s := hex.EncodeToString(sig[32:64])
	v := hex.EncodeToString([]byte{sig[64]})
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

// testRPCOnlyForwarding verifies that an Autobahn rpc-only sidecar accepts a
// signed EVM transaction, forwards it to the shard owner over HTTP, and the
// tx lands in a block on the cluster.
//
// Why this proves the rpc-only milestone:
//   - The rpc-only container has no consensus state, no producer, no block
//     execution loop. EvmProxy is its only meaningful surface.
//   - Submitting via the rpc-only's 8545 exercises send.go's proxy branch:
//     parse tx → recover sender → committee.EvmShard → dial shard-owner →
//     return the validator's hash response.
//   - Polling the validator's eth_getTransactionReceipt confirms the tx
//     actually landed (not just that the proxy hop happened with an error).
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

	// 2. Associate admin if its EVM-side balance is still 0. Skipping when
	//    balance is already > 0 keeps the test idempotent across test re-runs.
	if balanceAt(t, addr).Sign() == 0 {
		associateAdmin(t, adminPrivHex)
	}

	// 3. Wait for admin's EVM balance to materialize.
	deadline := time.Now().Add(30 * time.Second)
	var bal *big.Int
	for time.Now().Before(deadline) {
		bal = balanceAt(t, addr)
		if bal.Sign() > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	if bal.Sign() == 0 {
		t.Fatalf("admin balance never appeared at %s", addr.Hex())
	}
	t.Logf("admin balance: %s wei", bal)

	// 4. Build, sign, and submit a 0-value self-transfer via the rpc-only.
	chainID := chainIDFromRPC(t)
	nonce := nonceAt(t, addr)
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

	submit, err := evmRPCInContainer(rpcOnlyContainer, "eth_sendRawTransaction", []any{rawHex})
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

	// 5. Poll the validator for the receipt — proves the tx actually landed
	//    in a block (not just that the proxy hop returned a hash).
	deadline = time.Now().Add(rpcOnlyReceiptLimit)
	for time.Now().Before(deadline) {
		r, err := evmRPCOnHost("eth_getTransactionReceipt", []any{expected.Hex()})
		if err == nil && r.Error == nil && len(r.Result) > 0 && string(r.Result) != "null" {
			var receipt struct {
				Status      string `json:"status"`
				BlockNumber string `json:"blockNumber"`
			}
			if err := json.Unmarshal(r.Result, &receipt); err != nil {
				t.Fatalf("decode receipt: %v (raw=%s)", err, r.Result)
			}
			t.Logf("tx %s landed in block %s (status=%s)",
				expected.Hex(), receipt.BlockNumber, receipt.Status)
			if receipt.Status != "0x1" {
				t.Fatalf("tx reverted (status=%s)", receipt.Status)
			}
			return
		}
		time.Sleep(rpcOnlyReceiptPoll)
	}
	t.Fatalf("receipt never landed on validator for tx %s", expected.Hex())
}

// balanceAt fetches the EVM balance at addr via the validator RPC.
func balanceAt(t *testing.T, addr common.Address) *big.Int {
	t.Helper()
	resp, err := evmRPCOnHost("eth_getBalance", []any{addr.Hex(), "latest"})
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

func chainIDFromRPC(t *testing.T) *big.Int {
	t.Helper()
	resp, err := evmRPCOnHost("eth_chainId", []any{})
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

func nonceAt(t *testing.T, addr common.Address) uint64 {
	t.Helper()
	resp, err := evmRPCOnHost("eth_getTransactionCount", []any{addr.Hex(), "pending"})
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
