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
//   - tracks the per-sender balance used for affordability/recheck decisions
//   - returns nonce metadata used by mempool-side pending evaluation
//
// Test format is either 4-part or 5-part:
//   - "evm=<sender>=<nonce>=<priority>"                    (no balance requirement)
//   - "evm=<sender>=<nonce>=<priority>=<requiredBalance>"  (5th field is the
//     EVMRequiredBalance, i.e. the balance the tx must be able to afford)
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

	// On recheck, reject when the sender can no longer afford the tx, mirroring a
	// real EVM antehandler rejecting insufficient-funds txs. This models
	// EVMFeeCheckDecorator.BuyGas in x/evm/ante/fee.go, which today runs on both
	// CheckTx and ReCheckTx (it is NOT gated behind !IsReCheckTx) and returns
	// ErrInsufficientFunds when the sender cannot cover gas. If BuyGas is ever
	// gated behind !ctx.IsReCheckTx(), recheck would stop catching drained
	// balances and this mock rejection would pin fiction — update it in lockstep.
	// Scoped to recheck so initial admission (affordable at submit time) is
	// unaffected, which also preserves the errSameNonce admission-path test.
	if req.Type == abci.CheckTxTypeV2Recheck && requiredBalance.CmpUint64(uint64(a.balanceOf(senderAddr))) > 0 {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 3}}
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
	txmp := setup(cfg, proxy.New(app), NopTxConstraintsFetcher)

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
	txmp := setup(cfg, proxy.New(app), NopTxConstraintsFetcher)

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
	txmp := setup(cfg, proxy.New(app), NopTxConstraintsFetcher)

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

// TestTxMempool_DrainRaceSameBlockIncludesInvalidTx pins the REAL arctic-1
// (recheck_tx=false) drain-race behavior: when tx1 (nonce N) and tx2 (nonce N+1)
// from one sender are BOTH admitted affordable and neither has executed yet,
// a single Reap returns BOTH into the same block — tx2 IS INCLUDED.
//
// Mechanism (verified against tx.go): the readiness cascade in txStore.insert
// (the loop at tx.go:417-433) promotes each successive nonce to READY while the
// tx at that nonce is affordable, but it checks every tx against the SAME,
// undrained account.balance — it never subtracts what earlier nonces would
// spend. So with balance=100 and each tx requiring 100, tx1 goes ready and then
// tx2 goes ready too (100 >= 100). isReady (tx.go:449-452) is a pure nonce<
// nextNonce test, so both read as ready. inInclusionOrder puts both in the ready
// set and Reap (tx.go:631-671) walks that set, breaking only on !isReady — which
// never trips here — so both tx1 and tx2 come out in one reap.
//
// The balance recompute/demote only runs inside compact during a later
// txStore.Update, i.e. AFTER this block would already be proposed and shipped —
// too late to keep tx2 out. On chain, tx1 executes and drains the balance, then
// tx2 fails at delivery-time BuyGas (x/evm/ante/fee.go) and commits as a
// status=0 / gasUsed=0 receipt. That execution-time failure is out of mempool
// scope; this test asserts only the mempool contract: tx2 is reaped/included.
//
// Inclusion is proven by direct membership in the reaped set (not by Size),
// because a pending tx would be counted by Size but skipped by Reap — membership
// is the assertion that actually distinguishes inclusion from quarantine.
func TestTxMempool_DrainRaceSameBlockIncludesInvalidTx(t *testing.T) {
	ctx := t.Context()
	sender := common.HexToAddress("0x00000000000000000000000000000000000000df")

	app := newEVMNonceApp()
	app.setBalance(sender, 100)
	cfg := TestConfig()
	cfg.CacheSize = 5000
	// Pin the reap notify-gate off (tx.go: reap runs only when ready.count >=
	// TxNotifyThreshold). 0 is today's default, so both ready nonces reap
	// unconditionally; setting it explicitly keeps this test's premise from
	// silently breaking if that default is ever raised above 2.
	cfg.TxNotifyThreshold = 0
	txmp := setup(cfg, proxy.New(app), NopTxConstraintsFetcher)

	// Nonces 0 and 1 from one sender, each requiring the full balance of 100.
	// Both are affordable at admission, so the readiness cascade promotes both.
	// Insertion order is N then N+1 (the arctic-1 / Yasin repro order); in the
	// new mempool readiness is order-independent, so both are ready either way.
	tx1 := []byte(fmt.Sprintf("evm=%s=0=1=100", sender.Hex()))
	tx2 := []byte(fmt.Sprintf("evm=%s=1=1=100", sender.Hex()))
	_, err := txmp.CheckTx(ctx, tx1)
	require.NoError(t, err)
	_, err = txmp.CheckTx(ctx, tx2)
	require.NoError(t, err)

	// Both must be READY (none pending): the drain-race precondition.
	require.Equal(t, 2, txmp.Size())
	require.Equal(t, 0, txmp.PendingSize())
	require.Equal(t, 2, txmp.NumTxsNotPending())

	// Reap once, BEFORE any Update/compact has recomputed balances. This is the
	// proposal-time reap; both ready nonces come out into one block.
	reaped, _ := txmp.ReapTxs(ReapLimits{MaxTxs: utils.Some(uint64(10))}, true)

	reapedHashes := map[types.TxHash]struct{}{}
	for _, tx := range reaped {
		reapedHashes[tx.Hash()] = struct{}{}
	}
	_, tx1Reaped := reapedHashes[types.Tx(tx1).Hash()]
	_, tx2Reaped := reapedHashes[types.Tx(tx2).Hash()]

	require.Len(t, reaped, 2, "both nonces reap into one block")
	require.True(t, tx1Reaped, "tx1 (nonce N) is reaped")
	require.True(t, tx2Reaped, "tx2 (nonce N+1) is INCLUDED same-block despite the drain race")
}

// TestTxMempool_DrainRaceRecheckGatesEviction pins the CROSS-block drain-race
// path: what happens to the survivor tx2 (nonce N+1) in the Update AFTER tx1
// (nonce N) has already mined in a prior block and drained the sender.
//
// This is NOT the arctic-1 same-block inclusion path (that is
// TestTxMempool_DrainRaceSameBlockIncludesInvalidTx, where both txs reap into
// one block before any Update runs). Here tx1 is mined via a standalone Update
// and tx2 is re-evaluated in that same Update against the now-drained balance.
// Whether tx2 is dropped before it can be reaped again is gated purely by the
// recheck arg to Update (ConsensusParams.ABCI.RecheckTx):
//   - recheck=true  -> tx2 is READY at recheck time, so the recheck loop
//     re-CheckTx's it, BuyGas fails on insufficient balance, and it is evicted.
//   - recheck=false -> tx2 is not app-rechecked; the post-Update compact refetches
//     the drained balance and demotes tx2 to PENDING (not evicted, not ready).
//     It is retained but no longer reapable, i.e. quarantined until funded.
//
// Note recheck=false here demotes-to-pending; it does NOT include tx2. Inclusion
// only happens on the same-block path, before any balance recompute has run.
// This is config-driven, not a bug; the test documents current behavior so a
// future change to the recheck gate is visible.
func TestTxMempool_DrainRaceRecheckGatesEviction(t *testing.T) {
	for _, tc := range []struct {
		name         string
		recheck      bool
		wantSize     int
		wantPending  int
		wantRetained bool
	}{
		{name: "recheck_true_evicts", recheck: true, wantSize: 0, wantPending: 0, wantRetained: false},
		{name: "recheck_false_demotes_to_pending", recheck: false, wantSize: 1, wantPending: 1, wantRetained: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			sender := common.HexToAddress("0x00000000000000000000000000000000000000dd")

			app := newEVMNonceApp()
			app.setBalance(sender, 100)
			cfg := TestConfig()
			cfg.CacheSize = 5000
			txmp := setup(cfg, proxy.New(app), NopTxConstraintsFetcher)

			// Nonces 0 and 1 from one sender, each requiring the full balance of
			// 100. Both are affordable, hence READY, at admission.
			tx1 := []byte(fmt.Sprintf("evm=%s=0=1=100", sender.Hex()))
			tx2 := []byte(fmt.Sprintf("evm=%s=1=1=100", sender.Hex()))
			_, err := txmp.CheckTx(ctx, tx1)
			require.NoError(t, err)
			_, err = txmp.CheckTx(ctx, tx2)
			require.NoError(t, err)
			require.Equal(t, 2, txmp.Size())
			require.Equal(t, 0, txmp.PendingSize())

			// tx1 mines in this standalone Update and drains the sender to 0. Until
			// the post-Update compact refetches the balance, tx2 is still READY (in
			// ReadyTxs), so the recheck loop can see it when recheck=true.
			app.markMined(sender)
			app.setBalance(sender, 0)

			blockTxs := types.Txs{types.Tx(tx1)}
			results := []*abci.ExecTxResult{{Code: code.CodeTypeOK}}
			require.NoError(t, txmp.Update(ctx, 1, blockTxs, results, utils.OrPanic1(NopTxConstraintsFetcher()), tc.recheck))

			require.Equal(t, tc.wantSize, txmp.Size())
			require.Equal(t, tc.wantPending, txmp.PendingSize())
			_, ok := txmp.txStore.ByHash(types.Tx(tx2).Hash())
			require.Equal(t, tc.wantRetained, ok, "tx2 retained-as-pending iff recheck=false")
		})
	}
}

// TestTxMempool_DrainRaceRecheckScopeSkipsPendingTx documents current
// pending-scope behavior: recheck only re-validates ReadyTxs (see the ReadyTxs
// scan in TxMempool.Update in mempool.go), so a tx that is PENDING at recheck
// time is never re-CheckTx'd and thus is not evicted for insufficient balance,
// even under recheck=true. Here a nonce gap keeps the tx pending; draining the
// balance below its requiredBalance changes nothing because the recheck loop
// never sees it. Characterization only, not an assertion of correctness.
func TestTxMempool_DrainRaceRecheckScopeSkipsPendingTx(t *testing.T) {
	ctx := t.Context()
	sender := common.HexToAddress("0x00000000000000000000000000000000000000ee")

	app := newEVMNonceApp()
	app.setBalance(sender, 100)
	cfg := TestConfig()
	cfg.CacheSize = 5000
	txmp := setup(cfg, proxy.New(app), NopTxConstraintsFetcher)

	// Expected nonce is 0, but only nonce 1 is submitted: the missing nonce-0
	// gap keeps this tx PENDING (never promoted to ready).
	tx := []byte(fmt.Sprintf("evm=%s=1=1=100", sender.Hex()))
	_, err := txmp.CheckTx(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())
	require.Equal(t, 1, txmp.PendingSize())

	// Drain below requiredBalance, then recheck. Because the tx is pending it is
	// outside the recheck loop's ReadyTxs scope, so recheck=true does NOT evict.
	app.setBalance(sender, 0)
	require.NoError(t, txmp.Update(ctx, 1, nil, nil, utils.OrPanic1(NopTxConstraintsFetcher()), true))

	require.Equal(t, 1, txmp.Size(), "pending tx survives recheck=true (out of ReadyTxs scope)")
	require.Equal(t, 1, txmp.PendingSize())
	_, ok := txmp.txStore.ByHash(types.Tx(tx).Hash())
	require.True(t, ok, "pending became-unaffordable tx is retained under recheck=true")
}
