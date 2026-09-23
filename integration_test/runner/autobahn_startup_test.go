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

	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
	"github.com/sei-protocol/sei-chain/integration_test/runner"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

const (
	autobahnStartupContainer = "sei-node-0"
	autobahnStartupRPC       = "http://127.0.0.1:8545"
	autobahnStartupTimeout   = 2 * time.Minute
	autobahnStartupPoll      = 1 * time.Second
	// Past the Autobahn load-test sender range so this gate does not collide
	// with TestAutobahn when both run on the same cluster.
	autobahnStartupSender = uint64(2_000_000)
)

type evmRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type startupState struct{}

func (startupState) SetBalance(common.Address, *big.Int)               {}
func (startupState) SetCode(common.Address, []byte)                    {}
func (startupState) SetState(common.Address, common.Hash, common.Hash) {}

// TestAutobahnStartup is the startup gate for AUTOBAHN=true clusters. Autobahn
// serves the EVM JSON-RPC only, so the Tendermint RPC queries TestStartup makes
// have nothing to answer them. With empty blocks off, a committed transfer is
// what proves the producer is sealing.
func TestAutobahnStartup(t *testing.T) {
	runner.RunFile(t, "../startup/startup_autobahn_test.yaml")
	t.Run("Test a submitted transaction should confirm", assertAutobahnCommittedTx)
}

func assertAutobahnCommittedTx(t *testing.T) {
	cfg := scenarios.Config{
		TxsPerBlock:   1,
		ChainID:       new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID),
		GasPrice:      big.NewInt(1_000_000_000),
		SenderBalance: new(big.Int).Lsh(big.NewInt(1), 200),
		TransferValue: big.NewInt(1),
		TxGasLimit:    21_000,
		SameSender:    true,
	}
	workload, err := scenarios.NewTransferWorkload(cfg, startupState{})
	if err != nil {
		t.Fatalf("create startup transfer: %v", err)
	}
	block, err := workload.BuildBlock(t.Context(), autobahnStartupSender)
	if err != nil {
		t.Fatalf("build startup transfer: %v", err)
	}
	raw := block.Txs[0]
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		t.Fatalf("decode startup transfer: %v", err)
	}

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
