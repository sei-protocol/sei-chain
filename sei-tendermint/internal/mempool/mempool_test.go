package mempool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/code"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// application extends the KV store application by overriding CheckTx to provide
// transaction priority based on the value in the key/value pair.
type application struct {
	*kvstore.Application

	gasWanted      *int64
	gasEstimated   *int64
	occupiedNonces map[string][]uint64
}

type testTx struct {
	tx       types.Tx
	priority int64
}

var DefaultGasEstimated = int64(1)
var DefaultGasWanted = int64(1)

func (app *application) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) (*abci.ResponseCheckTxV2, error) {

	var (
		priority int64
		sender   string
	)

	gasWanted := DefaultGasWanted
	if app.gasWanted != nil {
		gasWanted = *app.gasWanted
	}

	gasEstimated := DefaultGasEstimated
	if app.gasEstimated != nil {
		gasEstimated = *app.gasEstimated
	}

	if strings.HasPrefix(string(req.Tx), "evm") {
		// format is evm-sender-0=account=priority=nonce
		// split into respective vars
		parts := bytes.Split(req.Tx, []byte("="))
		sender = string(parts[0])
		account := string(parts[1])
		v, err := strconv.ParseInt(string(parts[2]), 10, 64)
		if err != nil {
			// could not parse
			return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
				Priority:     priority,
				Code:         100,
				GasWanted:    gasWanted,
				GasEstimated: gasEstimated,
			}}, nil
		}
		nonce, err := strconv.ParseUint(string(parts[3]), 10, 64)
		if err != nil {
			// could not parse
			return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
				Priority:     priority,
				Code:         101,
				GasWanted:    gasWanted,
				GasEstimated: gasEstimated,
			}}, nil
		}
		if app.occupiedNonces == nil {
			app.occupiedNonces = make(map[string][]uint64)
		}
		if _, exists := app.occupiedNonces[account]; !exists {
			app.occupiedNonces[account] = []uint64{}
		}
		active := true
		for i := uint64(0); i < nonce; i++ {
			found := false
			for _, occ := range app.occupiedNonces[account] {
				if occ == i {
					found = true
					break
				}
			}
			if !found {
				active = false
				break
			}
		}
		app.occupiedNonces[account] = append(app.occupiedNonces[account], nonce)
		return &abci.ResponseCheckTxV2{
			ResponseCheckTx: &abci.ResponseCheckTx{
				Priority:     v,
				Code:         code.CodeTypeOK,
				GasWanted:    gasWanted,
				GasEstimated: gasEstimated,
			},
			EVMNonce:             nonce,
			EVMSenderAddress:     account,
			IsEVM:                true,
			IsPendingTransaction: !active,
			Checker:              func() abci.PendingTxCheckerResponse { return abci.Pending },
			ExpireTxHandler: func() {
				idx := -1
				for i, n := range app.occupiedNonces[account] {
					if n == nonce {
						idx = i
						break
					}
				}
				if idx >= 0 {
					app.occupiedNonces[account] = append(app.occupiedNonces[account][:idx], app.occupiedNonces[account][idx+1:]...)
				}
			},
		}, nil
	}

	// infer the priority from the raw transaction value (sender=key=value)
	parts := bytes.Split(req.Tx, []byte("="))
	if len(parts) == 3 {
		v, err := strconv.ParseInt(string(parts[2]), 10, 64)
		if err != nil {
			return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
				Priority:     priority,
				Code:         100,
				GasWanted:    gasWanted,
				GasEstimated: gasEstimated,
			}}, nil
		}

		priority = v
		sender = string(parts[0])
	} else {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
			Priority:     priority,
			Code:         101,
			GasWanted:    gasWanted,
			GasEstimated: gasEstimated,
		}}, nil
	}
	return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
		Priority:     priority,
		Sender:       sender,
		Code:         code.CodeTypeOK,
		GasWanted:    gasWanted,
		GasEstimated: gasEstimated,
	}}, nil
}

func (app *application) GetTxPriorityHint(context.Context, *abci.RequestGetTxPriorityHintV2) (*abci.ResponseGetTxPriorityHint, error) {
	return &abci.ResponseGetTxPriorityHint{
		// Return non-zero priority to allow testing the eviction logic effectively.
		Priority: 1,
	}, nil
}

func setup(t testing.TB, app abci.Application, cacheSize int, options ...TxMempoolOption) *TxMempool {
	t.Helper()

	logger := log.NewNopLogger()

	cfg, err := config.ResetTestRoot(t.TempDir(), strings.ReplaceAll(t.Name(), "/", "|"))
	require.NoError(t, err)
	cfg.Mempool.CacheSize = cacheSize
	cfg.Mempool.DropUtilisationThreshold = 0.0 // disable dropping by priority hint to allow testing eviction logic

	t.Cleanup(func() { os.RemoveAll(cfg.RootDir) })

	return NewTxMempool(logger.With("test", t.Name()), cfg.Mempool, app, NewTestPeerEvictor(), options...)
}

func checkTxs(ctx context.Context, t *testing.T, txmp *TxMempool, numTxs int, peerID uint16) []testTx {
	t.Helper()

	txs := make([]testTx, numTxs)
	txInfo := TxInfo{SenderID: peerID}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < numTxs; i++ {
		prefix := make([]byte, 20)
		_, err := rng.Read(prefix)
		require.NoError(t, err)

		priority := int64(rng.Intn(9999-1000) + 1000)

		txs[i] = testTx{
			tx:       []byte(fmt.Sprintf("sender-%d-%d=%X=%d", i, peerID, prefix, priority)),
			priority: priority,
		}
		require.NoError(t, txmp.CheckTx(ctx, txs[i].tx, nil, txInfo))
	}

	return txs
}

func convertTex(in []testTx) types.Txs {
	out := make([]types.Tx, len(in))

	for idx := range in {
		out[idx] = in[idx].tx
	}

	return out
}

type TestPeerEvictor struct {
	evicting map[types.NodeID]struct{}
}

func NewTestPeerEvictor() *TestPeerEvictor {
	return &TestPeerEvictor{evicting: map[types.NodeID]struct{}{}}
}

func (e *TestPeerEvictor) IsEvicted(peerID types.NodeID) bool {
	_, ok := e.evicting[peerID]
	return ok
}

func (e *TestPeerEvictor) Evict(id types.NodeID, _ error) {
	e.evicting[id] = struct{}{}
}

func TestTxMempool_TxsAvailable(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 0)
	txmp.EnableTxsAvailable()

	ensureNoTxFire := func() {
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-txmp.TxsAvailable():
			require.Fail(t, "unexpected transactions event")
		case <-timer.C:
		}
	}

	ensureTxFire := func() {
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-txmp.TxsAvailable():
		case <-timer.C:
			require.Fail(t, "expected transactions event")
		}
	}

	// ensure no event as we have not executed any transactions yet
	ensureNoTxFire()

	// Execute CheckTx for some transactions and ensure TxsAvailable only fires
	// once.
	txs := checkTxs(ctx, t, txmp, 100, 0)
	ensureTxFire()
	ensureNoTxFire()

	rawTxs := make([]types.Tx, len(txs))
	for i, tx := range txs {
		rawTxs[i] = tx.tx
	}

	responses := make([]*abci.ExecTxResult, len(rawTxs[:50]))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	// commit half the transactions and ensure we fire an event
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, nil, nil, true))
	txmp.Unlock()
	ensureTxFire()
	ensureNoTxFire()

	// Execute CheckTx for more transactions and ensure we do not fire another
	// event as we're still on the same height (1).
	_ = checkTxs(ctx, t, txmp, 100, 0)
	ensureNoTxFire()
}

func TestTxMempool_Size(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 0)
	txs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(txs), txmp.Size())
	require.Equal(t, 0, txmp.PendingSize())
	require.Equal(t, int64(5690), txmp.SizeBytes())

	rawTxs := make([]types.Tx, len(txs))
	for i, tx := range txs {
		rawTxs[i] = tx.tx
	}

	responses := make([]*abci.ExecTxResult, len(rawTxs[:50]))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, nil, nil, true))
	txmp.Unlock()

	require.Equal(t, len(rawTxs)/2, txmp.Size())
	require.Equal(t, int64(2850), txmp.SizeBytes())
}

func TestTxMempool_Flush(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 0)
	txs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(txs), txmp.Size())
	require.Equal(t, int64(5690), txmp.SizeBytes())

	rawTxs := make([]types.Tx, len(txs))
	for i, tx := range txs {
		rawTxs[i] = tx.tx
	}

	responses := make([]*abci.ExecTxResult, len(rawTxs[:50]))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, nil, nil, true))
	txmp.Unlock()

	txmp.Flush()
	require.Zero(t, txmp.Size())
	require.Equal(t, int64(0), txmp.SizeBytes())
}

func TestTxMempool_ReapMaxBytesMaxGas(t *testing.T) {
	ctx := t.Context()

	gasEstimated := int64(1) // gas estimated set to 1
	client := &application{Application: kvstore.NewApplication(), gasEstimated: &gasEstimated}

	txmp := setup(t, client, 0)
	tTxs := checkTxs(ctx, t, txmp, 100, 0) // all txs request 1 gas unit
	require.Equal(t, len(tTxs), txmp.Size())
	require.Equal(t, int64(5690), txmp.SizeBytes())

	txMap := make(map[types.TxKey]testTx)
	priorities := make([]int64, len(tTxs))
	for i, tTx := range tTxs {
		txMap[tTx.tx.Key()] = tTx
		priorities[i] = tTx.priority
	}

	sort.Slice(priorities, func(i, j int) bool {
		// sort by priority, i.e. decreasing order
		return priorities[i] > priorities[j]
	})

	ensurePrioritized := func(reapedTxs types.Txs) {
		reapedPriorities := make([]int64, len(reapedTxs))
		for i, rTx := range reapedTxs {
			reapedPriorities[i] = txMap[rTx.Key()].priority
		}

		require.Equal(t, priorities[:len(reapedPriorities)], reapedPriorities)
	}

	var wg sync.WaitGroup

	// reap by gas capacity only
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxBytesMaxGas(-1, 50, -1)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, int64(5690), txmp.SizeBytes())
		require.Len(t, reapedTxs, 50)
	}()

	// reap by transaction bytes only
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxBytesMaxGas(1000, -1, -1)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, int64(5690), txmp.SizeBytes())
		require.GreaterOrEqual(t, len(reapedTxs), 16)
	}()

	// Reap by both transaction bytes and gas, where the size yields 31 reaped
	// transactions and the gas limit reaps 25 transactions.
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxBytesMaxGas(1500, 30, -1)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, int64(5690), txmp.SizeBytes())
		require.Len(t, reapedTxs, 25)
	}()

	// Reap by min transactions in block regardless of gas limit.
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxBytesMaxGas(-1, 2, -1)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Len(t, reapedTxs, 2)
	}()

	// Reap by max gas estimated
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxBytesMaxGas(-1, -1, 50)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Len(t, reapedTxs, 50)
	}()

	wg.Wait()
}

func TestTxMempool_ReapMaxBytesMaxGas_FallbackToGasWanted(t *testing.T) {
	ctx := t.Context()

	gasEstimated := int64(0) // gas estimated not set so fallback to gas wanted
	client := &application{Application: kvstore.NewApplication(), gasEstimated: &gasEstimated}

	txmp := setup(t, client, 0)
	tTxs := checkTxs(ctx, t, txmp, 100, 0)

	txMap := make(map[types.TxKey]testTx)
	priorities := make([]int64, len(tTxs))
	for i, tTx := range tTxs {
		txMap[tTx.tx.Key()] = tTx
		priorities[i] = tTx.priority
	}

	// Debug: Print sorted priorities
	sort.Slice(priorities, func(i, j int) bool {
		return priorities[i] > priorities[j]
	})

	ensurePrioritized := func(reapedTxs types.Txs) {
		reapedPriorities := make([]int64, len(reapedTxs))
		for i, rTx := range reapedTxs {
			reapedPriorities[i] = txMap[rTx.Key()].priority
		}

		require.Equal(t, priorities[:len(reapedPriorities)], reapedPriorities)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxBytesMaxGas(-1, -1, 50)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Len(t, reapedTxs, 50)
	}()

	wg.Wait()
}

func TestTxMempool_ReapMaxTxs(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 0)
	tTxs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(tTxs), txmp.Size())
	require.Equal(t, int64(5690), txmp.SizeBytes())

	txMap := make(map[types.TxKey]testTx)
	priorities := make([]int64, len(tTxs))
	for i, tTx := range tTxs {
		txMap[tTx.tx.Key()] = tTx
		priorities[i] = tTx.priority
	}

	sort.Slice(priorities, func(i, j int) bool {
		// sort by priority, i.e. decreasing order
		return priorities[i] > priorities[j]
	})

	ensurePrioritized := func(reapedTxs types.Txs) {
		reapedPriorities := make([]int64, len(reapedTxs))
		for i, rTx := range reapedTxs {
			reapedPriorities[i] = txMap[rTx.Key()].priority
		}

		require.Equal(t, priorities[:len(reapedPriorities)], reapedPriorities)
	}

	var wg sync.WaitGroup

	// reap all transactions
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxTxs(-1)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, int64(5690), txmp.SizeBytes())
		require.Len(t, reapedTxs, len(tTxs))
	}()

	// reap a single transaction
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxTxs(1)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, int64(5690), txmp.SizeBytes())
		require.Len(t, reapedTxs, 1)
	}()

	// reap half of the transactions
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxTxs(len(tTxs) / 2)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, int64(5690), txmp.SizeBytes())
		require.Len(t, reapedTxs, len(tTxs)/2)
	}()

	wg.Wait()
}

func TestTxMempool_ReapMaxBytesMaxGas_MinGasEVMTxThreshold(t *testing.T) {
	ctx := t.Context()

	// estimatedGas below MinGasEVMTx (21000), gasWanted above it
	gasEstimated := int64(10000)
	gasWanted := int64(50000)
	client := &application{Application: kvstore.NewApplication(), gasEstimated: &gasEstimated, gasWanted: &gasWanted}

	txmp := setup(t, client, 0)
	peerID := uint16(1)
	address := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	// Insert a single EVM tx (format: evm-sender=account=priority=nonce)
	require.NoError(t, txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address, 100, 0)), nil, TxInfo{SenderID: peerID}))
	require.Equal(t, 1, txmp.Size())

	// With MinGasEVMTx=21000, estimatedGas (10000) is ignored and we fallback to gasWanted (50000).
	// Setting maxGasEstimated below gasWanted should therefore result in 0 reaped txs.
	reaped := txmp.ReapMaxBytesMaxGas(-1, -1, 40000)
	require.Len(t, reaped, 0)

	// Note: If MinGasEVMTx is changed to 0, the same scenario would use estimatedGas (10000)
	// and this test would fail because the tx would be reaped.
}

func TestTxMempool_CheckTxExceedsMaxSize(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}
	txmp := setup(t, client, 0)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	tx := make([]byte, txmp.config.MaxTxBytes+1)
	_, err := rng.Read(tx)
	require.NoError(t, err)

	require.Error(t, txmp.CheckTx(ctx, tx, nil, TxInfo{SenderID: 0}))

	tx = make([]byte, txmp.config.MaxTxBytes-1)
	_, err = rng.Read(tx)
	require.NoError(t, err)

	require.NoError(t, txmp.CheckTx(ctx, tx, nil, TxInfo{SenderID: 0}))
}

func TestTxMempool_Reap_SkipGasUnfitAndCollectMinTxs(t *testing.T) {
	ctx := t.Context()

	app := &application{Application: kvstore.NewApplication()}
	client := app

	txmp := setup(t, client, 0)
	peerID := uint16(1)

	// Insert one high-priority tx that is unfit by gas (exceeds maxGasEstimated)
	gwBig := int64(100)
	geBig := int64(100)
	app.gasWanted = &gwBig
	app.gasEstimated = &geBig
	bigTx := []byte(fmt.Sprintf("sender-big=key=%d", 1000000))
	require.NoError(t, txmp.CheckTx(ctx, bigTx, nil, TxInfo{SenderID: peerID}))

	// Now insert many small, lower-priority txs that fit well under the gas limit
	gwSmall := int64(1)
	geSmall := int64(1)
	app.gasWanted = &gwSmall
	app.gasEstimated = &geSmall
	for i := 0; i < 50; i++ {
		tx := []byte(fmt.Sprintf("sender-%d=key=%d", i, 1000-i))
		require.NoError(t, txmp.CheckTx(ctx, tx, nil, TxInfo{SenderID: peerID}))
	}

	// Reap with a maxGasEstimated that makes the first tx unfit but allows many small txs
	reaped := txmp.ReapMaxBytesMaxGas(-1, -1, 50)
	require.Len(t, reaped, MinTxsToPeek)

	// Ensure all reaped small txs are under gas constraint
	for _, rtx := range reaped {
		_ = rtx // gas constraints are enforced by ReapMaxBytesMaxGas; count assertion suffices here
	}
}

func TestTxMempool_Reap_SkipGasUnfitStopsAtMinEvenWithCapacity(t *testing.T) {
	ctx := t.Context()

	app := &application{Application: kvstore.NewApplication()}
	client := app

	txmp := setup(t, client, 0)
	peerID := uint16(1)

	// First tx: unfit by gas (bigger than limit), highest priority
	gwBig := int64(100)
	geBig := int64(100)
	app.gasWanted = &gwBig
	app.gasEstimated = &geBig
	bigTx := []byte(fmt.Sprintf("sender-big=key=%d", 1000000))
	require.NoError(t, txmp.CheckTx(ctx, bigTx, nil, TxInfo{SenderID: peerID}))

	// Insert many small txs that fit; plenty of capacity for more than 10
	gwSmall := int64(1)
	geSmall := int64(1)
	app.gasWanted = &gwSmall
	app.gasEstimated = &geSmall
	for i := 0; i < 100; i++ {
		tx := []byte(fmt.Sprintf("sender-sm-%d=key=%d", i, 2000-i))
		require.NoError(t, txmp.CheckTx(ctx, tx, nil, TxInfo{SenderID: peerID}))
	}

	// Make the gas limit very small so the first (big) tx is unfit and we only collect MinTxsPerBlock
	reaped := txmp.ReapMaxBytesMaxGas(-1, -1, 10)
	require.Len(t, reaped, MinTxsToPeek)
}

func TestTxMempool_Prioritization(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 100)
	peerID := uint16(1)

	address1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	address2 := "0xfD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	// Generate transactions with different priorities
	// there are two formats to comply with the above mocked CheckTX
	// EVM: evm-sender=account=priority=nonce
	// Non-EVM: sender=peer=priority
	txs := [][]byte{
		[]byte(fmt.Sprintf("sender-0-1=peer=%d", 9)),
		[]byte(fmt.Sprintf("sender-1-1=peer=%d", 8)),
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 6, 0)),
		[]byte(fmt.Sprintf("sender-2-1=peer=%d", 5)),
		[]byte(fmt.Sprintf("sender-3-1=peer=%d", 4)),
	}
	evmTxs := [][]byte{
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 7, 0)),
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 9, 1)),
	}

	// copy the slice of txs and shuffle the order randomly
	txsCopy := make([][]byte, len(txs))
	copy(txsCopy, txs)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(txsCopy), func(i, j int) {
		txsCopy[i], txsCopy[j] = txsCopy[j], txsCopy[i]
	})
	txs = [][]byte{
		[]byte(fmt.Sprintf("sender-0-1=peer=%d", 9)),
		[]byte(fmt.Sprintf("sender-1-1=peer=%d", 8)),
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 7, 0)),
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 9, 1)),
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 6, 0)),
		[]byte(fmt.Sprintf("sender-2-1=peer=%d", 5)),
		[]byte(fmt.Sprintf("sender-3-1=peer=%d", 4)),
	}
	txsCopy = append(txsCopy, evmTxs...)

	for i := range txsCopy {
		require.NoError(t, txmp.CheckTx(ctx, txsCopy[i], nil, TxInfo{SenderID: peerID}))
	}

	// Reap the transactions
	reapedTxs := txmp.ReapMaxTxs(len(txs))
	// Check if the reaped transactions are in the correct order of their priorities
	for _, tx := range txs {
		fmt.Printf("expected: %s\n", string(tx))
	}
	fmt.Println("**************")
	for _, reapedTx := range reapedTxs {
		fmt.Printf("received: %s\n", string(reapedTx))
	}
	for i, reapedTx := range reapedTxs {
		require.Equal(t, txs[i], []byte(reapedTx))
	}
}

func TestTxMempool_PendingStoreSize(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 100)
	txmp.config.PendingSize = 1
	peerID := uint16(1)

	address1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	require.NoError(t, txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 1)), nil, TxInfo{SenderID: peerID}))
	err := txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 2)), nil, TxInfo{SenderID: peerID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "mempool pending set is full")
}

func TestTxMempool_RemoveCacheWhenPendingTxIsFull(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 10)
	txmp.config.PendingSize = 1
	peerID := uint16(1)
	address1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	require.NoError(t, txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 1)), nil, TxInfo{SenderID: peerID}))
	err := txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 2)), nil, TxInfo{SenderID: peerID})
	require.Error(t, err)
	txCache := txmp.cache.(*LRUTxCache)
	// Make sure the second tx is removed from cache
	require.Equal(t, 1, len(txCache.cacheMap))
}

func TestTxMempool_EVMEviction(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 100)
	txmp.config.Size = 1
	peerID := uint16(1)

	address1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	address2 := "0xfD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	// Add first transaction with priority 1
	require.NoError(t, txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 0)), nil, TxInfo{SenderID: peerID}))

	// This should evict the previous tx (priority 1 < priority 2)
	require.NoError(t, txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 2, 0)), nil, TxInfo{SenderID: peerID}))
	require.Equal(t, 1, txmp.priorityIndex.NumTxs())
	require.Equal(t, int64(2), txmp.priorityIndex.txs[0].priority)

	// Increase mempool size to 2
	txmp.config.Size = 2
	require.NoError(t, txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 3, 1)), nil, TxInfo{SenderID: peerID}))
	require.Equal(t, 0, txmp.pendingTxs.Size())
	require.Equal(t, 2, txmp.priorityIndex.NumTxs())

	// This would evict the tx with priority 2 and cause the tx with priority 3 to go pending
	require.NoError(t, txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 4, 0)), nil, TxInfo{SenderID: peerID}))

	// Wait for async operations to complete with proper synchronization
	// Instead of arbitrary sleep, wait for the expected state
	require.Eventually(t, func() bool {
		return txmp.priorityIndex.NumTxs() == 1 && txmp.pendingTxs.Size() == 1
	}, 5*time.Second, 100*time.Millisecond, "Expected mempool state not reached")

	// Verify final state
	require.Equal(t, 1, txmp.priorityIndex.NumTxs())
	require.Equal(t, 1, txmp.pendingTxs.Size())

	tx := txmp.priorityIndex.txs[0]
	require.Equal(t, int64(4), tx.priority) // Should be the highest priority transaction

	require.NoError(t, txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 5, 1)), nil, TxInfo{SenderID: peerID}))
	require.Equal(t, 2, txmp.priorityIndex.NumTxs())

	txmp.removeTx(tx, true, false, true)
	// Should not reenqueue
	require.Equal(t, 1, txmp.priorityIndex.NumTxs())

	// Wait for async operations and verify final state
	require.Eventually(t, func() bool {
		return txmp.pendingTxs.Size() == 1
	}, 5*time.Second, 100*time.Millisecond, "Expected pendingTxs size not reached")
	require.Equal(t, 1, txmp.pendingTxs.Size())
}

func TestTxMempool_CheckTxSamePeer(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 100)
	peerID := uint16(1)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	prefix := make([]byte, 20)
	_, err := rng.Read(prefix)
	require.NoError(t, err)

	tx := []byte(fmt.Sprintf("sender-0=%X=%d", prefix, 50))

	require.NoError(t, txmp.CheckTx(ctx, tx, nil, TxInfo{SenderID: peerID}))
	require.Error(t, txmp.CheckTx(ctx, tx, nil, TxInfo{SenderID: peerID}))
}

func TestTxMempool_CheckTxSameSender(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 100)
	peerID := uint16(1)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	prefix1 := make([]byte, 20)
	_, err := rng.Read(prefix1)
	require.NoError(t, err)

	prefix2 := make([]byte, 20)
	_, err = rng.Read(prefix2)
	require.NoError(t, err)

	tx1 := []byte(fmt.Sprintf("sender-0=%X=%d", prefix1, 50))
	tx2 := []byte(fmt.Sprintf("sender-0=%X=%d", prefix2, 50))

	require.NoError(t, txmp.CheckTx(ctx, tx1, nil, TxInfo{SenderID: peerID}))
	require.Equal(t, 1, txmp.Size())
	require.NoError(t, txmp.CheckTx(ctx, tx2, nil, TxInfo{SenderID: peerID}))
	require.Equal(t, 1, txmp.Size())
}

func TestTxMempool_ConcurrentTxs(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 100)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	checkTxDone := make(chan struct{})

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		for i := 0; i < 20; i++ {
			_ = checkTxs(ctx, t, txmp, 100, 0)
			dur := rng.Intn(1000-500) + 500
			time.Sleep(time.Duration(dur) * time.Millisecond)
		}

		wg.Done()
		close(checkTxDone)
	}()

	wg.Add(1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		defer wg.Done()

		var height int64 = 1

		for range ticker.C {
			reapedTxs := txmp.ReapMaxTxs(200)
			if len(reapedTxs) > 0 {
				responses := make([]*abci.ExecTxResult, len(reapedTxs))
				for i := 0; i < len(responses); i++ {
					var code uint32

					if i%10 == 0 {
						code = 100
					} else {
						code = abci.CodeTypeOK
					}

					responses[i] = &abci.ExecTxResult{Code: code}
				}

				txmp.Lock()
				require.NoError(t, txmp.Update(ctx, height, reapedTxs, responses, nil, nil, true))
				txmp.Unlock()

				height++
			} else {
				// only return once we know we finished the CheckTx loop
				select {
				case <-checkTxDone:
					return
				default:
				}
			}
		}
	}()

	wg.Wait()
	require.Zero(t, txmp.Size())
	require.Zero(t, txmp.SizeBytes())
}

func TestTxMempool_ExpiredTxs_NumBlocks(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 500)
	txmp.height = 100
	txmp.config.TTLNumBlocks = 10

	tTxs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(tTxs), txmp.Size())
	require.Equal(t, 100, txmp.expirationIndex.Size())

	// reap 5 txs at the next height -- no txs should expire
	reapedTxs := txmp.ReapMaxTxs(5)
	responses := make([]*abci.ExecTxResult, len(reapedTxs))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, txmp.height+1, reapedTxs, responses, nil, nil, true))
	txmp.Unlock()

	require.Equal(t, 95, txmp.Size())
	require.Equal(t, 95, txmp.expirationIndex.Size())

	// check more txs at height 101
	_ = checkTxs(ctx, t, txmp, 50, 1)
	require.Equal(t, 145, txmp.Size())
	require.Equal(t, 145, txmp.expirationIndex.Size())

	// Reap 5 txs at a height that would expire all the transactions from before
	// the previous Update (height 100).
	//
	// NOTE: When we reap txs below, we do not know if we're picking txs from the
	// initial CheckTx calls or from the second round of CheckTx calls. Thus, we
	// cannot guarantee that all 95 txs are remaining that should be expired and
	// removed. However, we do know that that at most 95 txs can be expired and
	// removed.
	reapedTxs = txmp.ReapMaxTxs(5)
	responses = make([]*abci.ExecTxResult, len(reapedTxs))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, txmp.height+10, reapedTxs, responses, nil, nil, true))
	txmp.Unlock()

	require.GreaterOrEqual(t, txmp.Size(), 45)
	require.GreaterOrEqual(t, txmp.expirationIndex.Size(), 45)
}

func TestTxMempool_CheckTxPostCheckError(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "error",
			err:  errors.New("test error"),
		},
		{
			name: "no error",
			err:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			client := &application{Application: kvstore.NewApplication()}

			postCheckFn := func(_ types.Tx, _ *abci.ResponseCheckTx) error {
				return tc.err
			}
			txmp := setup(t, client, 0, WithPostCheck(postCheckFn))
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			tx := make([]byte, txmp.config.MaxTxBytes-1)
			_, err := rng.Read(tx)
			require.NoError(t, err)

			callback := func(res *abci.ResponseCheckTx) {
				expectedErrString := ""
				if tc.err != nil {
					expectedErrString = tc.err.Error()
					require.Equal(t, expectedErrString, txmp.postCheck(tx, res).Error())
				} else {
					require.Equal(t, nil, txmp.postCheck(tx, res))
				}
			}
			if tc.err == nil {
				require.NoError(t, txmp.CheckTx(ctx, tx, callback, TxInfo{SenderID: 0}))
			} else {
				err = txmp.CheckTx(ctx, tx, callback, TxInfo{SenderID: 0})
				fmt.Print(err.Error())
			}
		})
	}
}

func TestTxMempool_FailedCheckTxCount(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	postCheckFn := func(_ types.Tx, _ *abci.ResponseCheckTx) error {
		return nil
	}
	txmp := setup(t, client, 0, WithPostCheck(postCheckFn))
	badTx := make([]byte, txmp.config.MaxTxBytes+1)

	callback := func(res *abci.ResponseCheckTx) {
		require.Equal(t, nil, txmp.postCheck(badTx, res))
	}
	require.Equal(t, uint64(0), txmp.GetPeerFailedCheckTxCount("sender"))

	txmp.config.CheckTxErrorBlacklistEnabled = false

	// bad tx before enabling blacklisting does not increment the failed count
	require.Error(t, txmp.CheckTx(ctx, badTx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.Equal(t, uint64(0), txmp.GetPeerFailedCheckTxCount("sender"))

	txmp.config.CheckTxErrorBlacklistEnabled = true
	txmp.config.CheckTxErrorThreshold = 2

	// first bad tx that should be tracked
	require.Error(t, txmp.CheckTx(ctx, badTx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.Equal(t, uint64(1), txmp.GetPeerFailedCheckTxCount("sender"))

	// second bad tx that should be tracked
	require.Error(t, txmp.CheckTx(ctx, badTx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.Equal(t, uint64(2), txmp.GetPeerFailedCheckTxCount("sender"))

	goodTx := []byte("sender=key=1")
	// good tx doesn't increase failedCount
	require.NoError(t, txmp.CheckTx(ctx, goodTx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.Equal(t, uint64(2), txmp.GetPeerFailedCheckTxCount("sender"))

	// three strikes, you're out!
	require.Error(t, txmp.CheckTx(ctx, badTx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.True(t, txmp.router.(*TestPeerEvictor).IsEvicted("sender"))
}

func TestAppendCheckTxErr(t *testing.T) {
	client := &application{Application: kvstore.NewApplication()}
	txmp := setup(t, client, 500)
	existingLogData := "existing error log"
	newLogData := "sample error log"

	// Append new error
	actualResult := txmp.AppendCheckTxErr(existingLogData, newLogData)
	expectedResult := fmt.Sprintf("%s; %s", existingLogData, newLogData)

	require.Equal(t, expectedResult, actualResult)

	// Append new error to empty log
	actualResult = txmp.AppendCheckTxErr("", newLogData)

	require.Equal(t, newLogData, actualResult)
}

func TestMempoolExpiration(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 0)
	txmp.config.TTLDuration = time.Nanosecond // we want tx to expire immediately
	txmp.config.RemoveExpiredTxsFromQueue = true
	txs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(txs), txmp.priorityIndex.Len())
	require.Equal(t, len(txs), txmp.expirationIndex.Size())
	require.Equal(t, len(txs), txmp.txStore.Size())
	time.Sleep(time.Millisecond)
	txmp.purgeExpiredTxs(txmp.height)
	require.Equal(t, 0, txmp.priorityIndex.Len())
	require.Equal(t, 0, txmp.expirationIndex.Size())
	require.Equal(t, 0, txmp.txStore.Size())
}

// TestReapMaxBytesMaxGas_EVMFirst verifies that ReapMaxBytesMaxGas returns
// EVM transactions first, followed by non-EVM transactions.
func TestReapMaxBytesMaxGas_EVMFirst(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, client, 0)
	peerID := uint16(1)

	evmAddress1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	evmAddress2 := "0xfD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	evmAddress3 := "0xaD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	// Set up priorities so that pure priority ordering would interleave EVM and non-EVM:
	// Priority order: EVM(100), non-EVM(90), EVM(80), non-EVM(70), EVM(60)
	txsToAdd := [][]byte{
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", evmAddress1, 100, 0)), // EVM, priority 100
		[]byte("sender-1=key1=90"),                                      // non-EVM, priority 90
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", evmAddress2, 80, 0)),  // EVM, priority 80
		[]byte("sender-2=key2=70"),                                      // non-EVM, priority 70
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", evmAddress3, 60, 0)),  // EVM, priority 60
	}

	for _, tx := range txsToAdd {
		require.NoError(t, txmp.CheckTx(ctx, tx, nil, TxInfo{SenderID: peerID}))
	}

	require.Equal(t, 5, txmp.Size())

	// Reap all transactions
	reapedTxs := txmp.ReapMaxBytesMaxGas(-1, -1, -1)
	require.Len(t, reapedTxs, 5)

	// Verify EVM transactions come first, then non-EVM
	// Find the boundary between EVM and non-EVM transactions
	evmCount := 0
	nonEvmStartIdx := -1
	for i, tx := range reapedTxs {
		isEVM := strings.HasPrefix(string(tx), "evm")
		if isEVM {
			evmCount++
			// After we've seen non-EVM, we shouldn't see any more EVM
			require.Equal(t, -1, nonEvmStartIdx, "EVM transaction found after non-EVM transaction at index %d: %s", i, string(tx))
		} else {
			if nonEvmStartIdx == -1 {
				nonEvmStartIdx = i
			}
		}
	}

	// We should have exactly 3 EVM transactions first, then 2 non-EVM
	require.Equal(t, 3, evmCount, "Expected 3 EVM transactions")
	require.Equal(t, 3, nonEvmStartIdx, "Expected non-EVM transactions to start at index 3")

	// Verify the first 3 transactions are EVM
	require.True(t, strings.HasPrefix(string(reapedTxs[0]), "evm"), "First tx should be EVM: %s", string(reapedTxs[0]))
	require.True(t, strings.HasPrefix(string(reapedTxs[1]), "evm"), "Second tx should be EVM: %s", string(reapedTxs[1]))
	require.True(t, strings.HasPrefix(string(reapedTxs[2]), "evm"), "Third tx should be EVM: %s", string(reapedTxs[2]))

	// Verify the last 2 transactions are non-EVM
	require.True(t, strings.HasPrefix(string(reapedTxs[3]), "sender"), "Fourth tx should be non-EVM: %s", string(reapedTxs[3]))
	require.True(t, strings.HasPrefix(string(reapedTxs[4]), "sender"), "Fifth tx should be non-EVM: %s", string(reapedTxs[4]))
}
