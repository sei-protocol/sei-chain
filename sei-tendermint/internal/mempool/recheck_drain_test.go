package mempool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/code"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// evmNonceApp models a Sei-like EVM antehandler for mempool tests:
//   - tracks the next-expected nonce per sender (the "mined" nonce frontier)
//   - returns nonce metadata used by mempool-side pending evaluation
//
// Test format: "evm=<sender>=<nonce>=<priority>".
type evmNonceApp struct {
	abci.Application

	mu        sync.Mutex
	nextNonce map[common.Address]uint64
	balance   map[common.Address]int
}

func newEVMNonceApp() *evmNonceApp {
	return &evmNonceApp{
		nextNonce: map[common.Address]uint64{},
		balance:   map[common.Address]int{},
	}
}

// markMined bumps the sender's next-expected nonce by 1, simulating that the
// previous next-expected nonce just landed in a block.
func (a *evmNonceApp) markMined(sender common.Address) {
	a.mu.Lock()
	a.nextNonce[sender]++
	a.mu.Unlock()
}

func (a *evmNonceApp) setNonce(sender common.Address, nonce uint64) {
	a.mu.Lock()
	a.nextNonce[sender] = nonce
	a.mu.Unlock()
}

func (a *evmNonceApp) setBalance(sender common.Address, balance int) {
	a.mu.Lock()
	a.balance[sender] = balance
	a.mu.Unlock()
}

func (a *evmNonceApp) balanceOf(sender common.Address) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if balance, ok := a.balance[sender]; ok {
		return balance
	}
	return 0
}

func (a *evmNonceApp) parseTx(tx []byte) (sender string, nonce uint64, priority int64, requiredBalance uint256.Int, ok bool) {
	parts := bytes.Split(tx, []byte("="))
	if len(parts) != 4 && len(parts) != 5 || string(parts[0]) != "evm" {
		return "", 0, 0, uint256.Int{}, false
	}
	n, err := strconv.ParseUint(string(parts[2]), 10, 64)
	if err != nil {
		return "", 0, 0, uint256.Int{}, false
	}
	p, err := strconv.ParseInt(string(parts[3]), 10, 64)
	if err != nil {
		return "", 0, 0, uint256.Int{}, false
	}
	requiredBalance = uint256.Int{}
	if len(parts) == 5 {
		b, err := strconv.ParseInt(string(parts[4]), 10, 64)
		if err != nil {
			return "", 0, 0, uint256.Int{}, false
		}
		if b < 0 {
			return "", 0, 0, uint256.Int{}, false
		}
		requiredBalance = *uint256.NewInt(uint64(b))
	}
	return string(parts[1]), n, p, requiredBalance, true
}

func (a *evmNonceApp) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	sender, nonce, priority, requiredBalance, ok := a.parseTx(req.Tx)
	if !ok {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 1}}
	}
	senderAddr := common.HexToAddress(sender)

	a.mu.Lock()
	expected := a.nextNonce[senderAddr]
	a.mu.Unlock()

	if nonce < expected {
		// Already mined. Reject.
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 2}}
	}

	res := &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         code.CodeTypeOK,
			Priority:     priority,
			GasWanted:    DefaultGasWanted,
			GasEstimated: DefaultGasEstimated,
		},
		EVMHash:            common.Hash(sha256.Sum256(req.Tx)),
		EVMNonce:           nonce,
		EVMSenderAddress:   senderAddr,
		SeiSenderAddress:   sdk.AccAddress(senderAddr.Bytes()),
		IsEVM:              true,
		EVMRequiredBalance: requiredBalance,
	}
	return res
}

func (a *evmNonceApp) GetTxPriorityHint(context.Context, *abci.RequestGetTxPriorityHintV2) (*abci.ResponseGetTxPriorityHint, error) {
	return &abci.ResponseGetTxPriorityHint{Priority: 1}, nil
}

func (a *evmNonceApp) EvmNonce(addr common.Address) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nextNonce[addr]
}

func (a *evmNonceApp) EvmBalance(addr common.Address, _ []byte) uint256.Int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if balance, ok := a.balance[addr]; ok {
		return *uint256.NewInt(uint64(balance))
	}
	return uint256.Int{}
}

// TestTxMempool_DescendingNonceDrain exercises the producer-style flow:
// submit N EVM nonces from a single sender in descending order (worst case
// for the gap-pending pool — every tx except the last is ahead of expected
// at CheckTx time), then drain by repeatedly PopTxs-ing and Update-ing.
//
// Regression for the recheck=true eviction loop: with recheck=true, the
// mempool-side pending classification for the rest of the per-sender
// evmQueue causes handleRecheckResult to evict + async re-CheckTx every
// non-head tx, dumping them back into pendingTxs. The mempool's priority
// pool collapses to 1 each Update cycle and the chain only mines 1 per
// block, vs draining all N in roughly one PopTxs/Update cycle here.
func TestTxMempool_DescendingNonceDrain(t *testing.T) {
	sender := common.HexToAddress("0x00000000000000000000000000000000000000cc")
	const N = 100

	ctx := t.Context()
	app := newEVMNonceApp()
	cfg := TestConfig()
	cfg.CacheSize = 5000
	txmp := setup(cfg, proxy.New(app, proxy.NewMetrics()), NopTxConstraintsFetcher)

	// Submit nonces N-1, N-2, ..., 1, 0. Every tx except the last enters
	// pendingTxs because its nonce is ahead of the sender's expected nonce
	// (0) at CheckTx time. The last tx (nonce 0) matches expected and lands
	// in the priority index as the evmQueue head.
	for n := N - 1; n >= 0; n-- {
		tx := []byte(fmt.Sprintf("evm=%s=%d=1", sender.Hex(), n))
		_, err := txmp.CheckTx(ctx, tx)
		require.NoError(t, err)
	}

	require.Equal(t, N, txmp.Size(), "mempool should hold all submitted txs")

	// Drain: repeatedly reap a batch, "mine" each tx (advance nextNonce), then
	// Update the mempool. With recheck=false the producer reaps ALL N txs in at
	// most a couple of blocks: the first PopTxs grabs the head, Update promotes
	// the rest of the per-sender evmQueue, the second PopTxs reaps everything.
	// We bound the loop tightly so a regression to recheck=true (which evicts
	// the queue tail and forces 1-tx-per-block forever) fails fast instead of
	// silently passing.
	const maxBlocks = 5
	totalMined := 0
	for height := int64(1); txmp.Size() > 0 && height <= maxBlocks; height++ {
		txs, _ := txmp.ReapTxs(ReapLimits{
			MaxTxs: utils.Some(uint64(N)),
		}, true)
		require.NotEmpty(t, txs, "PopTxs returned no txs at height %d (mempool stalled)", height)

		txResults := make([]*abci.ExecTxResult, len(txs))
		for i := range txs {
			app.markMined(sender)
			txResults[i] = &abci.ExecTxResult{Code: code.CodeTypeOK}
		}
		totalMined += len(txs)

		// recheck=false matches the post-fix Autobahn path and CometBFT's default.
		require.NoError(t, txmp.Update(ctx, height, txs, txResults, utils.OrPanic1(NopTxConstraintsFetcher()), false))
	}

	require.Equal(t, N, totalMined, "all N txs should have mined within %d blocks", maxBlocks)
	require.Zero(t, txmp.Size(), "mempool should fully drain within %d blocks", maxBlocks)
	require.Equal(t, uint64(N), app.nextNonce[sender], "all N nonces should have been mined")
}

func TestTxMempool_EvmNextPendingNonceIncludesPendingTransactions(t *testing.T) {
	ctx := t.Context()
	sender := common.HexToAddress("0x00000000000000000000000000000000000000aa")

	app := newEVMNonceApp()
	app.setNonce(sender, 5)
	cfg := TestConfig()
	cfg.CacheSize = 5000
	txmp := setup(cfg, proxy.New(app, proxy.NewMetrics()), NopTxConstraintsFetcher)

	for _, nonce := range []uint64{7, 5, 6} {
		tx := []byte(fmt.Sprintf("evm=%s=%d=1", sender.Hex(), nonce))
		_, err := txmp.CheckTx(ctx, tx)
		require.NoError(t, err)
	}

	require.Equal(t, 3, txmp.NumTxsNotPending())
	require.Equal(t, 0, txmp.PendingSize())
	require.Equal(t, uint64(8), txmp.EvmNextPendingNonce(sender))
}

func TestTxMempool_EvmNextPendingNonceReplacesSameNonceByPriority(t *testing.T) {
	ctx := t.Context()
	sender := common.HexToAddress("0x00000000000000000000000000000000000000bb")

	app := newEVMNonceApp()
	app.setNonce(sender, 5)
	cfg := TestConfig()
	cfg.CacheSize = 5000
	txmp := setup(cfg, proxy.New(app, proxy.NewMetrics()), NopTxConstraintsFetcher)

	lowPriorityTx := []byte(fmt.Sprintf("evm=%s=%d=%d", sender.Hex(), 6, 1))
	highPriorityTx := []byte(fmt.Sprintf("evm=%s=%d=%d", sender.Hex(), 6, 2))

	_, err := txmp.CheckTx(ctx, lowPriorityTx)
	require.NoError(t, err)
	_, err = txmp.CheckTx(ctx, highPriorityTx)
	require.NoError(t, err)

	require.Equal(t, 1, txmp.PendingSize())
	require.Equal(t, 1, txmp.Size())
	_, ok := txmp.txStore.ByHash(types.Tx(lowPriorityTx).Hash())
	require.False(t, ok)
	_, ok = txmp.txStore.ByHash(types.Tx(highPriorityTx).Hash())
	require.True(t, ok)
	require.Equal(t, uint64(5), txmp.EvmNextPendingNonce(sender))
}
