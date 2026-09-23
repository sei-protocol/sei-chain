//go:build autobahn_integration

// Package autobahn contains integration tests for Autobahn EVM-only consensus.
//
// Requires a running Autobahn Docker cluster. Run via:
//
//	make autobahn-integration-test
//
// Or directly (cluster must already be up):
//
//	go test -tags autobahn_integration -v ./integration_test/autobahn/...
package autobahn

import (
	"bufio"
	"context"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

const (
	clusterBootTimeout  = 5 * time.Minute
	clusterBootPoll     = 5 * time.Second
	autobahnSettleDelay = 30 * time.Second

	evmOnlyLoadTxs     = 4_000
	evmOnlyLoadTimeout = 3 * time.Minute
	evmOnlyMetricsURL  = "http://127.0.0.1:26660/metrics"
	evmOnlyNextBlock   = "tendermint_internal_autobahn_data_next_block"
	evmOnlyTxLatency   = "tendermint_internal_autobahn_data_latency_count"
)

// clusterSize is set once at TestAutobahn start from the number of running
// sei-node-* containers.
var clusterSize int

func listRunningNodes(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("docker", "ps",
		"--filter", "name=sei-node-",
		"--filter", "status=running",
		"--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("docker ps: %v", err)
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}

func assertAutobahnEnabled(t *testing.T) {
	t.Helper()
	names := listRunningNodes(t)
	if len(names) == 0 {
		t.Fatalf("no running sei-node-* containers")
	}
	for _, name := range names {
		cmd := exec.Command("docker", "exec", name, "sh", "-c",
			"grep -q 'GigaRouter initialized' build/generated/logs/seid-*.log")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("autobahn not enabled on %s (no 'GigaRouter initialized' in container log): %v\n%s",
				name, err, out)
		}
	}
}

func assertEVMOnlyEnabled(t *testing.T) {
	t.Helper()
	for _, name := range listRunningNodes(t) {
		cmd := exec.Command("docker", "exec", name, "sh", "-c",
			"grep -q 'Autobahn EVM-only execution enabled with disk-backed Giga storage' build/generated/logs/seid-*.log")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("EVM-only execution not enabled on %s: %v\n%s", name, err, out)
		}
	}
}

func TestMain(m *testing.M) {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find repo root: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintf(os.Stderr, "chdir to %s: %v\n", root, err)
		os.Exit(1)
	}
	if err := setupCluster(); err != nil {
		fmt.Fprintf(os.Stderr, "cluster setup failed: %v\n", err)
		teardownCluster()
		os.Exit(1)
	}
	code := m.Run()
	teardownCluster()
	os.Exit(code)
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func runMake(env []string, target string) error {
	cmd := exec.Command("make", target)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func setupCluster() error {
	fmt.Println("=== Starting Autobahn Integration Tests ===")
	_ = runMake(nil, "docker-cluster-stop")
	if err := os.RemoveAll("build/generated"); err != nil {
		return fmt.Errorf("rm -rf build/generated: %w", err)
	}
	if err := runMake([]string{"AUTOBAHN=true", "DOCKER_DETACH=true"}, "docker-cluster-start"); err != nil {
		return fmt.Errorf("docker-cluster-start: %w", err)
	}
	expected, err := countSeiContainers()
	if err != nil {
		return fmt.Errorf("count cluster containers: %w", err)
	}
	if expected == 0 {
		return fmt.Errorf("no sei-node-* containers found after docker-cluster-start")
	}
	fmt.Printf("Waiting for %d nodes to be ready...\n", expected)
	deadline := time.Now().Add(clusterBootTimeout)
	for time.Now().Before(deadline) {
		if n := countLaunchComplete("build/generated/launch.complete"); n >= expected {
			fmt.Printf("All %d nodes are ready\n", expected)
			fmt.Printf("Waiting %s for autobahn connections to establish...\n", autobahnSettleDelay)
			time.Sleep(autobahnSettleDelay)
			return nil
		}
		time.Sleep(clusterBootPoll)
	}
	return fmt.Errorf("cluster failed to start within %s", clusterBootTimeout)
}

func countSeiContainers() (int, error) {
	out, err := exec.Command("docker", "ps", "-a",
		"--filter", "name=sei-node-",
		"--format", "{{.Names}}").Output()
	if err != nil {
		return 0, err
	}
	return len(strings.Fields(strings.TrimSpace(string(out)))), nil
}

func teardownCluster() {
	fmt.Println("=== Stopping cluster ===")
	_ = runMake(nil, "docker-cluster-stop")
}

func countLaunchComplete(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.TrimSpace(s.Text()) != "" {
			n++
		}
	}
	return n
}

func TestAutobahn(t *testing.T) {
	names := listRunningNodes(t)
	if len(names) == 0 {
		t.Fatalf("no running sei-node-* containers")
	}
	clusterSize = len(names)
	t.Run("EVMOnlyLoad", testEVMOnlyLoad)
}

type evmOnlyLoadState struct{}

func (evmOnlyLoadState) SetBalance(common.Address, *big.Int)               {}
func (evmOnlyLoadState) SetCode(common.Address, []byte)                    {}
func (evmOnlyLoadState) SetState(common.Address, common.Hash, common.Hash) {}

func testEVMOnlyLoad(t *testing.T) {
	assertAutobahnEnabled(t)
	assertEVMOnlyEnabled(t)
	assertTendermintRPCDisabled(t)
	if clusterSize != 4 {
		t.Fatalf("EVM-only Docker load test requires four validators, got %d", clusterSize)
	}

	workload, err := scenarios.NewTransferWorkload(scenarios.Config{
		TxsPerBlock:   evmOnlyLoadTxs,
		ChainID:       new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID),
		GasPrice:      big.NewInt(1_000_000_000),
		SenderBalance: new(big.Int).Lsh(big.NewInt(1), 200),
		TransferValue: big.NewInt(1),
		TxGasLimit:    21_000,
	}, evmOnlyLoadState{})
	if err != nil {
		t.Fatalf("create EVM-only transfer workload: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), evmOnlyLoadTimeout)
	defer cancel()
	block, err := workload.BuildBlock(ctx, 1)
	if err != nil {
		t.Fatalf("build EVM-only transfer workload: %v", err)
	}

	clients := make([]*ethrpc.Client, clusterSize)
	for i := range clients {
		client, err := ethrpc.DialContext(ctx, fmt.Sprintf("http://localhost:%d", 8545+2*i))
		if err != nil {
			t.Fatalf("create node %d EVM RPC client: %v", i, err)
		}
		clients[i] = client
		t.Cleanup(client.Close)
	}
	started := time.Now()
	group, groupCtx := errgroup.WithContext(ctx)
	for nodeIndex, client := range clients {
		nodeIndex, client := nodeIndex, client
		group.Go(func() error {
			for txIndex := nodeIndex; txIndex < len(block.Txs); txIndex += len(clients) {
				var hash common.Hash
				if err := client.CallContext(groupCtx, &hash, "eth_sendRawTransaction", hexutil.Bytes(block.Txs[txIndex])); err != nil {
					return fmt.Errorf("send raw transaction %d through node %d: %w", txIndex, nodeIndex, err)
				}
				if want := crypto.Keccak256Hash(block.Txs[txIndex]); hash != want {
					return fmt.Errorf("send raw transaction %d through node %d: hash %s, want %s", txIndex, nodeIndex, hash, want)
				}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		t.Fatal(err)
	}

	lastHeight, included := waitForEVMOnlyTxs(t, ctx, listRunningNodes(t), len(block.Txs))
	assertEVMOnlyReceipts(t, ctx, clients, block.Txs)
	assertEVMOnlyBalances(t, ctx, clients, block.Txs)
	assertEVMOnlyTransactionCount(t, ctx, clients, block.Txs)
	assertEVMOnlyChainID(t, ctx, clients)
	assertEVMOnlyBlockNumber(t, ctx, clients, lastHeight)
	elapsed := time.Since(started)
	t.Logf("Autobahn finalized %d raw EVM transfers through %d validators in %s (%.0f tx/s)",
		included, clusterSize, elapsed.Round(time.Millisecond), float64(included)/elapsed.Seconds())
	t.Logf("all validators executed through at least height %d", lastHeight)
}

func assertEVMOnlyBalances(t *testing.T, ctx context.Context, clients []*ethrpc.Client, txs [][]byte) {
	t.Helper()
	want := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 200), big.NewInt(1))
	for nodeIndex, client := range clients {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(txs[nodeIndex]); err != nil {
			t.Fatalf("decode EVM-only transaction %d: %v", nodeIndex, err)
		}
		var got hexutil.Big
		if err := client.CallContext(ctx, &got, "eth_getBalance", tx.To(), "latest"); err != nil {
			t.Fatalf("read EVM-only balance %s from node %d: %v", tx.To(), nodeIndex, err)
		}
		if got.ToInt().Cmp(want) != 0 {
			t.Fatalf("node %d returned balance %s for %s, want %s", nodeIndex, got.ToInt(), tx.To(), want)
		}
	}
}

// assertEVMOnlyTransactionCount checks eth_getTransactionCount against every
// node: each transfer's sender starts at nonce 0 and sends exactly one
// transaction, so its committed nonce should now be 1.
func assertEVMOnlyTransactionCount(t *testing.T, ctx context.Context, clients []*ethrpc.Client, txs [][]byte) {
	t.Helper()
	signer := ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID))
	for nodeIndex, client := range clients {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(txs[nodeIndex]); err != nil {
			t.Fatalf("decode EVM-only transaction %d: %v", nodeIndex, err)
		}
		sender, err := ethtypes.Sender(signer, tx)
		if err != nil {
			t.Fatalf("recover EVM-only sender for transaction %d: %v", nodeIndex, err)
		}
		var got hexutil.Uint64
		if err := client.CallContext(ctx, &got, "eth_getTransactionCount", sender, "latest"); err != nil {
			t.Fatalf("read EVM-only transaction count %s from node %d: %v", sender, nodeIndex, err)
		}
		if got != 1 {
			t.Fatalf("node %d returned transaction count %d for %s, want 1", nodeIndex, got, sender)
		}
	}
}

// assertEVMOnlyChainID checks eth_chainId against every node.
func assertEVMOnlyChainID(t *testing.T, ctx context.Context, clients []*ethrpc.Client) {
	t.Helper()
	wantChainID := new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID)
	for nodeIndex, client := range clients {
		var chainID hexutil.Big
		if err := client.CallContext(ctx, &chainID, "eth_chainId"); err != nil {
			t.Fatalf("read EVM-only chain ID from node %d: %v", nodeIndex, err)
		}
		if (*big.Int)(&chainID).Cmp(wantChainID) != 0 {
			t.Fatalf("node %d returned chain ID %s, want %s", nodeIndex, (*big.Int)(&chainID), wantChainID)
		}
	}
}

// assertEVMOnlyBlockNumber checks eth_blockNumber against every node once the
// load run has finalized through minHeight.
func assertEVMOnlyBlockNumber(t *testing.T, ctx context.Context, clients []*ethrpc.Client, minHeight int64) {
	t.Helper()
	for nodeIndex, client := range clients {
		var height hexutil.Uint64
		if err := client.CallContext(ctx, &height, "eth_blockNumber"); err != nil {
			t.Fatalf("read EVM-only block number from node %d: %v", nodeIndex, err)
		}
		if int64(height) < minHeight {
			t.Fatalf("node %d returned block number %d, want at least %d", nodeIndex, height, minHeight)
		}
	}
}

func assertEVMOnlyReceipts(t *testing.T, ctx context.Context, clients []*ethrpc.Client, txs [][]byte) {
	t.Helper()
	for nodeIndex, client := range clients {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(txs[nodeIndex]); err != nil {
			t.Fatalf("decode EVM-only transaction %d: %v", nodeIndex, err)
		}
		txHash := tx.Hash()
		var got *struct {
			BlockHash        common.Hash     `json:"blockHash"`
			BlockNumber      hexutil.Uint64  `json:"blockNumber"`
			GasUsed          hexutil.Uint64  `json:"gasUsed"`
			Status           hexutil.Uint64  `json:"status"`
			To               *common.Address `json:"to"`
			TransactionHash  common.Hash     `json:"transactionHash"`
			TransactionIndex hexutil.Uint64  `json:"transactionIndex"`
		}
		if err := client.CallContext(ctx, &got, "eth_getTransactionReceipt", txHash); err != nil {
			t.Fatalf("read EVM-only receipt %s from node %d: %v", txHash, nodeIndex, err)
		}
		if got == nil {
			t.Fatalf("node %d returned null for finalized EVM-only receipt %s", nodeIndex, txHash)
		}
		if got.BlockHash == (common.Hash{}) {
			t.Fatalf("node %d returned an empty block hash for receipt %s", nodeIndex, txHash)
		}
		if got.BlockNumber == 0 {
			t.Fatalf("node %d returned block zero for receipt %s", nodeIndex, txHash)
		}
		if got.GasUsed != hexutil.Uint64(21_000) {
			t.Fatalf("node %d returned gasUsed %d for receipt %s", nodeIndex, got.GasUsed, txHash)
		}
		if got.Status != hexutil.Uint64(ethtypes.ReceiptStatusSuccessful) {
			t.Fatalf("node %d returned status %d for receipt %s", nodeIndex, got.Status, txHash)
		}
		if got.To == nil || tx.To() == nil || *got.To != *tx.To() {
			t.Fatalf("node %d returned to %v for receipt %s", nodeIndex, got.To, txHash)
		}
		if got.TransactionHash != txHash {
			t.Fatalf("node %d returned transaction hash %s, want %s", nodeIndex, got.TransactionHash, txHash)
		}
	}
}

// assertTendermintRPCDisabled checks that no validator serves Tendermint RPC:
// Autobahn serves the EVM JSON-RPC only.
func assertTendermintRPCDisabled(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	for i := range clusterSize {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/status", 26657+3*i))
		if err != nil {
			continue
		}
		_ = response.Body.Close()
		t.Fatalf("node %d unexpectedly serves Tendermint RPC with HTTP status %s", i, response.Status)
	}
}

func waitForEVMOnlyTxs(t *testing.T, ctx context.Context, containers []string, target int) (int64, int) {
	t.Helper()
	for {
		if err := ctx.Err(); err != nil {
			t.Fatalf("waiting for %d EVM-only transactions on every validator: %v", target, err)
		}
		minHeight := int64(^uint64(0) >> 1)
		minExecuted := target
		allExecuted := true
		for _, container := range containers {
			height, executed, err := evmOnlyExecutionProgress(ctx, container)
			if err != nil || executed < target {
				allExecuted = false
				break
			}
			minHeight = min(minHeight, height)
			minExecuted = min(minExecuted, executed)
		}
		if allExecuted {
			return minHeight, minExecuted
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func evmOnlyExecutionProgress(ctx context.Context, container string) (int64, int, error) {
	out, err := exec.CommandContext(ctx, "docker", "exec", container, "curl", "-fsS", evmOnlyMetricsURL).Output()
	if err != nil {
		return 0, 0, err
	}
	nextBlock, ok := prometheusSample(out, evmOnlyNextBlock, `stage="execute"`)
	if !ok || nextBlock < 1 {
		return 0, 0, fmt.Errorf("missing EVM-only execution height metric")
	}
	executed, ok := prometheusSample(out, evmOnlyTxLatency, `resource="txs"`, `stage="execute"`)
	if !ok {
		return 0, 0, fmt.Errorf("missing EVM-only executed transaction metric")
	}
	return int64(nextBlock) - 1, int(executed), nil
}

func prometheusSample(metrics []byte, name string, labels ...string) (float64, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(metrics)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, name+"{") {
			continue
		}
		matches := true
		for _, label := range labels {
			if !strings.Contains(line, label) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}
