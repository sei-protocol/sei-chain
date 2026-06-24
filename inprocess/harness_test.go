//go:build inprocess

package inprocess

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
)

// TestInProcessNetwork productionizes the N-RPC spike: it stands up N=4
// validators in one process, asserts every node serves Tendermint RPC + EVM
// JSON-RPC, and round-trips a tx (broadcast on node 0, observed on node 1's
// independent RPC) — proving real consensus + N independent RPC stacks.
//
// Run:
//
//	go test -tags inprocess -run TestInProcessNetwork -v -timeout 300s ./inprocess/
func TestInProcessNetwork(t *testing.T) {
	const n = 4
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	net, err := Start(ctx, Options{
		Validators:    n,
		TimeoutCommit: time.Second, // tighten the cadence for a faster test.
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer net.Close()

	if net.Len() != n {
		t.Fatalf("Len = %d, want %d", net.Len(), n)
	}

	// VERIFY 1+2: every node reaches consensus and serves EVM (WaitReady gates on
	// height-advance + eth_blockNumber per node).
	if err := net.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	for i := 0; i < n; i++ {
		nd := net.Node(i)
		t.Logf("node %s: tm=%s evm=%s ws=%s grpc=%s", nd.Name(), nd.TendermintRPC(), nd.EVMRPC(), nd.EVMWS(), nd.GRPC())
	}

	// No EVM listener reported a bind failure.
	for i := 0; i < n; i++ {
		select {
		case err := <-net.Node(i).ServeErr():
			t.Fatalf("node %s EVM serve error: %v", net.Node(i).Name(), err)
		default:
		}
	}

	// VERIFY 3: tx broadcast on node 0 is observable on node 1's independent RPC.
	assertCrossNodeTxRoundTrip(t, ctx, net)
}

// assertCrossNodeTxRoundTrip broadcasts a bank send from node 0's validator key
// via node 0's RPC, then polls node 1's RPC until the tx is queryable by hash —
// the load-bearing proof that the two nodes share consensus and each serves an
// independent RPC stack.
func assertCrossNodeTxRoundTrip(t *testing.T, ctx context.Context, net *Network) {
	t.Helper()
	n0, n1 := net.nodes[0], net.nodes[1]
	bondDenom := sdk.DefaultBondDenom

	to := sdk.AccAddress(make([]byte, 20))
	msg := banktypes.NewMsgSend(n0.addr, to, sdk.NewCoins(sdk.NewCoin(bondDenom, sdk.NewInt(1))))

	num, seq, err := n0.clientCx.AccountRetriever.GetAccountNumberSequence(n0.clientCx, n0.addr)
	if err != nil {
		t.Fatalf("fetch account for node0: %v", err)
	}
	txf := tx.Factory{}.
		WithChainID(net.opts.ChainID).WithKeybase(n0.clientCx.Keyring).
		WithTxConfig(n0.clientCx.TxConfig).WithGas(300000).
		WithFees(fmt.Sprintf("200000%s", bondDenom)).
		WithAccountRetriever(n0.clientCx.AccountRetriever).
		WithAccountNumber(num).WithSequence(seq)

	txb, err := tx.BuildUnsignedTx(txf, msg)
	if err != nil {
		t.Fatalf("build tx: %v", err)
	}
	if err := tx.Sign(txf, n0.moniker, txb, true); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	txBz, err := n0.clientCx.TxConfig.TxEncoder()(txb.GetTx())
	if err != nil {
		t.Fatalf("encode tx: %v", err)
	}

	res, err := n0.rpc.BroadcastTxSync(ctx, txBz)
	if err != nil {
		t.Fatalf("broadcast via node0: %v", err)
	}
	t.Logf("broadcast via node0: code=%d hash=%X", res.Code, res.Hash)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		q, err := n1.rpc.Tx(ctx, res.Hash, false)
		if err == nil && q != nil {
			t.Logf("PASS: tx %X broadcast on node0 found on node1 at height %d (code=%d)", res.Hash, q.Height, q.TxResult.Code)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("tx %X not observed on node1 within deadline", res.Hash)
}

// TestStartRejectsZeroValidators guards the input validation.
func TestStartRejectsZeroValidators(t *testing.T) {
	_, err := Start(context.Background(), Options{Validators: 0})
	if err == nil {
		t.Fatal("Start with 0 validators: want error, got nil")
	}
}

// TestFreshChainIDPerRun pins the per-run unique chain-id discipline: an empty
// Options.ChainID must yield a distinct id each time, so a run never collides
// with a prior run's persisted genesis. Pure-function check — no bring-up.
func TestFreshChainIDPerRun(t *testing.T) {
	a := Options{}.withDefaults().ChainID
	b := Options{}.withDefaults().ChainID
	if a == b {
		t.Fatalf("fresh chain-id not unique across runs: %q == %q", a, b)
	}
	if !strings.HasPrefix(a, chainIDPrefix) {
		t.Fatalf("chain-id %q lacks prefix %q", a, chainIDPrefix)
	}
	// An explicit ChainID is honored verbatim.
	if got := (Options{ChainID: "pinned"}).withDefaults().ChainID; got != "pinned" {
		t.Fatalf("explicit ChainID not honored: got %q", got)
	}
}
