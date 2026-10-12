//go:build yaml_integration

package runner_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os/exec"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
	"github.com/sei-protocol/sei-chain/integration_test/runner"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

const (
	autobahnStartupContainer = "sei-node-0"
	autobahnStartupRPC       = "http://127.0.0.1:8545"
	autobahnStartupTimeout   = 2 * time.Minute
	autobahnStartupPoll      = 1 * time.Second
	// Far from the Autobahn load-test sender range (1..4000). The two
	// suites use separate clusters in CI; this only matters if someone
	// points both at one leftover cluster.
	autobahnStartupSender = uint64(2_000_000)
)

type evmRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// TestAutobahnStartup is the startup gate for AUTOBAHN=true clusters. Autobahn
// serves the EVM JSON-RPC only, so the Tendermint RPC queries TestStartup makes
// have nothing to answer them. With empty blocks off, a committed transfer is
// what proves the producer is sealing.
func TestAutobahnStartup(t *testing.T) {
	runner.RunFile(t, "../startup/startup_autobahn_test.yaml")
	t.Run("Test a submitted transaction should confirm", assertAutobahnCommittedTx)
}

func assertAutobahnCommittedTx(t *testing.T) {
	tx, raw := buildStartupTransfer(t)
	sent, err := evmRPCInContainer(autobahnStartupContainer, "eth_sendRawTransaction", []any{hexutil.Encode(raw)})
	if err != nil {
		t.Fatalf("send startup transfer: %v", err)
	}
	if sent.Error != nil {
		t.Fatalf("send startup transfer: rpc %d %s", sent.Error.Code, sent.Error.Message)
	}

	deadline := time.Now().Add(autobahnStartupTimeout)
	for time.Now().Before(deadline) {
		receipt, err := evmRPCInContainer(autobahnStartupContainer, "eth_getTransactionReceipt", []any{tx.Hash()})
		if err == nil && receipt.Error == nil && len(receipt.Result) > 0 && string(receipt.Result) != "null" {
			t.Logf("startup transfer %s confirmed", tx.Hash())
			return
		}
		time.Sleep(autobahnStartupPoll)
	}
	t.Fatalf("startup transfer %s was not confirmed within %s", tx.Hash(), autobahnStartupTimeout)
}

// buildStartupTransfer returns a signed 1 wei self-transfer from the startup
// sender at its next nonce, together with its raw encoding.
func buildStartupTransfer(t *testing.T) (*ethtypes.Transaction, []byte) {
	t.Helper()
	key, err := scenarios.DeterministicPrivateKey(autobahnStartupSender)
	if err != nil {
		t.Fatalf("derive startup sender key: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	signer := ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID))
	tx, err := ethtypes.SignNewTx(key, signer, &ethtypes.LegacyTx{
		Nonce:    committedNonce(t, sender),
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21_000,
		To:       &sender,
		Value:    big.NewInt(1),
	})
	if err != nil {
		t.Fatalf("sign startup transfer: %v", err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("encode startup transfer: %v", err)
	}
	return tx, raw
}

// committedNonce returns the nonce the chain expects next from sender.
func committedNonce(t *testing.T, sender common.Address) uint64 {
	t.Helper()
	resp, err := evmRPCInContainer(autobahnStartupContainer, "eth_getTransactionCount", []any{sender, "latest"})
	if err != nil {
		t.Fatalf("read startup sender nonce: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("read startup sender nonce: rpc %d %s", resp.Error.Code, resp.Error.Message)
	}
	var nonce hexutil.Uint64
	if err := json.Unmarshal(resp.Result, &nonce); err != nil {
		t.Fatalf("decode startup sender nonce %s: %v", resp.Result, err)
	}
	return uint64(nonce)
}

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
		autobahnStartupRPC).Output()
	if err != nil {
		return nil, fmt.Errorf("docker exec curl: %v", err)
	}
	var r evmRPCResponse
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("decode (body=%s): %w", out, err)
	}
	return &r, nil
}
