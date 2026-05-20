package mempool

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/code"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// application extends the KV store application by overriding CheckTx to provide
// transaction priority based on the value in the key/value pair.
type application struct {
	*kvstore.Application

	gasWanted    *int64
	gasEstimated *int64
}

type testTx struct {
	tx       types.Tx
	priority int64
}

var DefaultGasEstimated = int64(1)
var DefaultGasWanted = int64(1)

func (app *application) EvmNonce(common.Address) uint64 {
	return 0
}

func (app *application) EvmBalance(common.Address, []byte) *big.Int {
	return big.NewInt(0)
}

func (app *application) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {

	var priority int64

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
		account := string(parts[1])
		v, err := strconv.ParseInt(string(parts[2]), 10, 64)
		if err != nil {
			// could not parse
			return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
				Priority:     priority,
				Code:         100,
				GasWanted:    gasWanted,
				GasEstimated: gasEstimated,
			}}
		}
		nonce, err := strconv.ParseUint(string(parts[3]), 10, 64)
		if err != nil {
			// could not parse
			return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
				Priority:     priority,
				Code:         101,
				GasWanted:    gasWanted,
				GasEstimated: gasEstimated,
			}}
		}
		res := &abci.ResponseCheckTxV2{
			ResponseCheckTx: &abci.ResponseCheckTx{
				Priority:     v,
				Code:         code.CodeTypeOK,
				GasWanted:    gasWanted,
				GasEstimated: gasEstimated,
			},
			EVMNonce:           nonce,
			EVMSenderAddress:   common.HexToAddress(account),
			SeiSenderAddress:   sdk.AccAddress(common.HexToAddress(account).Bytes()),
			IsEVM:              true,
			EVMRequiredBalance: big.NewInt(0),
		}
		return res
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
			}}
		}

		priority = v
	} else {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
			Priority:     priority,
			Code:         101,
			GasWanted:    gasWanted,
			GasEstimated: gasEstimated,
		}}
	}
	return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
		Priority:     priority,
		Code:         code.CodeTypeOK,
		GasWanted:    gasWanted,
		GasEstimated: gasEstimated,
	}}
}

func (app *application) GetTxPriorityHint(context.Context, *abci.RequestGetTxPriorityHintV2) (*abci.ResponseGetTxPriorityHint, error) {
	return &abci.ResponseGetTxPriorityHint{
		// Return non-zero priority to allow testing the eviction logic effectively.
		Priority: 1,
	}, nil
}

func setup(t testing.TB, app *proxy.Proxy, cacheSize int, txConstraintsFetcher TxConstraintsFetcher) *TxMempool {
	t.Helper()

	cfg := TestConfig()
	cfg.CacheSize = cacheSize
	return NewTxMempool(cfg, app, NopMetrics(), txConstraintsFetcher)
}

func checkTxs(ctx context.Context, t *testing.T, txmp *TxMempool, numTxs int) []testTx {
	t.Helper()

	txs := make([]testTx, numTxs)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < numTxs; i++ {
		prefix := make([]byte, 20)
		_, err := rng.Read(prefix)
		require.NoError(t, err)

		priority := int64(rng.Intn(9999-1000) + 1000)

		txs[i] = testTx{
			tx:       []byte(fmt.Sprintf("sender-%d=%X=%d", i, prefix, priority)),
			priority: priority,
		}
		_, err = txmp.CheckTx(ctx, txs[i].tx)
		require.NoError(t, err)
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

func totalTxSizeBytes(txs []testTx) uint64 {
	var total uint64
	for _, tx := range txs {
		total += uint64(len(tx.tx))
	}
	return total
}

func totalRawTxSizeBytes(txs []types.Tx) uint64 {
	var total uint64
	for _, tx := range txs {
		total += uint64(len(tx))
	}
	return total
}

func expectedReapCountByBytes(txs []testTx, maxBytes int64) int {
	var total int64
	count := 0
	for _, tx := range txs {
		txSize := types.ComputeProtoSizeForTxs([]types.Tx{tx.tx})
		if maxBytes-total < txSize {
			break
		}
		total += txSize
		count++
	}
	return count
}

func TestTxMempool_TxsAvailable(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)

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
	txs := checkTxs(ctx, t, txmp, 100)
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
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()
	ensureTxFire()
	ensureNoTxFire()

	// Execute CheckTx for more transactions and ensure we do not fire another
	// event as we're still on the same height (1).
	_ = checkTxs(ctx, t, txmp, 100)
	ensureNoTxFire()
}

func TestTxMempool_Size(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	txs := checkTxs(ctx, t, txmp, 100)
	require.Equal(t, len(txs), txmp.Size())
	require.Equal(t, 0, txmp.PendingSize())
	require.Equal(t, totalTxSizeBytes(txs), txmp.SizeBytes())

	rawTxs := make([]types.Tx, len(txs))
	for i, tx := range txs {
		rawTxs[i] = tx.tx
	}

	responses := make([]*abci.ExecTxResult, len(rawTxs[:50]))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	require.Equal(t, len(rawTxs)/2, txmp.Size())
	require.Equal(t, totalRawTxSizeBytes(rawTxs[50:]), txmp.SizeBytes())
}

func TestTxMempool_Flush(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	txs := checkTxs(ctx, t, txmp, 100)
	require.Equal(t, len(txs), txmp.Size())
	require.Equal(t, totalTxSizeBytes(txs), txmp.SizeBytes())

	rawTxs := make([]types.Tx, len(txs))
	for i, tx := range txs {
		rawTxs[i] = tx.tx
	}

	responses := make([]*abci.ExecTxResult, len(rawTxs[:50]))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	txmp.Flush()
	require.Zero(t, txmp.Size())
	require.Zero(t, txmp.SizeBytes())
}

func TestTxMempool_ReapMaxBytesMaxGas(t *testing.T) {
	ctx := t.Context()

	gasEstimated := int64(1) // gas estimated set to 1
	client := &application{Application: kvstore.NewApplication(), gasEstimated: &gasEstimated}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	tTxs := checkTxs(ctx, t, txmp, 100) // all txs request 1 gas unit
	require.Equal(t, len(tTxs), txmp.Size())
	require.Equal(t, totalTxSizeBytes(tTxs), txmp.SizeBytes())

	txMap := make(map[types.TxHash]testTx)
	priorities := make([]int64, len(tTxs))
	for i, tTx := range tTxs {
		txMap[tTx.tx.Hash()] = tTx
		priorities[i] = tTx.priority
	}

	sort.Slice(priorities, func(i, j int) bool {
		// sort by priority, i.e. decreasing order
		return priorities[i] > priorities[j]
	})

	sortedTxs := append([]testTx(nil), tTxs...)
	sort.Slice(sortedTxs, func(i, j int) bool {
		return sortedTxs[i].priority > sortedTxs[j].priority
	})

	ensurePrioritized := func(reapedTxs types.Txs) {
		reapedPriorities := make([]int64, len(reapedTxs))
		for i, rTx := range reapedTxs {
			reapedPriorities[i] = txMap[rTx.Hash()].priority
		}

		require.Equal(t, priorities[:len(reapedPriorities)], reapedPriorities)
	}

	var wg sync.WaitGroup

	// reap by gas capacity only
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{MaxGasWanted: utils.Some(int64(50))})
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, totalTxSizeBytes(tTxs), txmp.SizeBytes())
		require.Len(t, reapedTxs, 50)
	}()

	// reap by transaction bytes only
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{MaxBytes: utils.Some(int64(1000))})
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, totalTxSizeBytes(tTxs), txmp.SizeBytes())
		require.Len(t, reapedTxs, expectedReapCountByBytes(sortedTxs, 1000))
	}()

	// Reap by both transaction bytes and gas, where the size yields 31 reaped
	// transactions and the gas limit reaps 25 transactions.
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{
			MaxBytes:     utils.Some(int64(1500)),
			MaxGasWanted: utils.Some(int64(30)),
		})
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, totalTxSizeBytes(tTxs), txmp.SizeBytes())
		require.Len(t, reapedTxs, min(expectedReapCountByBytes(sortedTxs, 1500), 30))
	}()

	// Reap by min transactions in block regardless of gas limit.
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{MaxGasWanted: utils.Some(int64(2))})
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Len(t, reapedTxs, 2)
	}()

	// Reap by max gas estimated
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{MaxGasEstimated: utils.Some(int64(50))})
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

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	tTxs := checkTxs(ctx, t, txmp, 100)

	txMap := make(map[types.TxHash]testTx)
	priorities := make([]int64, len(tTxs))
	for i, tTx := range tTxs {
		txMap[tTx.tx.Hash()] = tTx
		priorities[i] = tTx.priority
	}

	// Debug: Print sorted priorities
	sort.Slice(priorities, func(i, j int) bool {
		return priorities[i] > priorities[j]
	})

	ensurePrioritized := func(reapedTxs types.Txs) {
		reapedPriorities := make([]int64, len(reapedTxs))
		for i, rTx := range reapedTxs {
			reapedPriorities[i] = txMap[rTx.Hash()].priority
		}

		require.Equal(t, priorities[:len(reapedPriorities)], reapedPriorities)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{MaxGasEstimated: utils.Some(int64(50))})
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Len(t, reapedTxs, 50)
	}()

	wg.Wait()
}

func TestTxMempool_ReapMaxTxs(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	tTxs := checkTxs(ctx, t, txmp, 100)
	require.Equal(t, len(tTxs), txmp.Size())
	require.Equal(t, totalTxSizeBytes(tTxs), txmp.SizeBytes())

	txMap := make(map[types.TxHash]testTx)
	priorities := make([]int64, len(tTxs))
	for i, tTx := range tTxs {
		txMap[tTx.tx.Hash()] = tTx
		priorities[i] = tTx.priority
	}

	sort.Slice(priorities, func(i, j int) bool {
		// sort by priority, i.e. decreasing order
		return priorities[i] > priorities[j]
	})

	ensurePrioritized := func(reapedTxs types.Txs) {
		reapedPriorities := make([]int64, len(reapedTxs))
		for i, rTx := range reapedTxs {
			reapedPriorities[i] = txMap[rTx.Hash()].priority
		}

		require.Equal(t, priorities[:len(reapedPriorities)], reapedPriorities)
	}

	var wg sync.WaitGroup

	// reap all transactions
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{})
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, totalTxSizeBytes(tTxs), txmp.SizeBytes())
		require.Len(t, reapedTxs, len(tTxs))
	}()

	// reap a single transaction
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{MaxTxs: utils.Some(uint64(1))})
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, totalTxSizeBytes(tTxs), txmp.SizeBytes())
		require.Len(t, reapedTxs, 1)
	}()

	// reap half of the transactions
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapTxs(ReapLimits{MaxTxs: utils.Some(uint64(len(tTxs) / 2))})
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, totalTxSizeBytes(tTxs), txmp.SizeBytes())
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

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	address := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	// Insert a single EVM tx (format: evm-sender=account=priority=nonce)
	_, err := txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address, 100, 0)))
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())

	// With MinGasEVMTx=21000, estimatedGas (10000) is ignored and we fallback to gasWanted (50000).
	// Setting maxGasEstimated below gasWanted should therefore result in 0 reaped txs.
	reaped := txmp.ReapTxs(ReapLimits{MaxGasEstimated: utils.Some(int64(40000))})
	require.Len(t, reaped, 0)

	// Note: If MinGasEVMTx is changed to 0, the same scenario would use estimatedGas (10000)
	// and this test would fail because the tx would be reaped.
}

func TestTxMempool_CheckTxExceedsMaxSize(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}
	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	tx := make([]byte, txmp.config.MaxTxBytes+1)
	_, err := rng.Read(tx)
	require.NoError(t, err)

	_, err = txmp.CheckTx(ctx, tx)
	require.Error(t, err)

	tx = make([]byte, txmp.config.MaxTxBytes-1)
	_, err = rng.Read(tx)
	require.NoError(t, err)

	_, err = txmp.CheckTx(ctx, tx)
	require.NoError(t, err)
}

func TestTxMempool_Prioritization(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)

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
	txsCopy = append(txsCopy, evmTxs...)

	for i := range txsCopy {
		_, err := txmp.CheckTx(ctx, txsCopy[i])
		require.NoError(t, err)
	}

	expectedReapedTxs := types.Txs{
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 7, 0)),
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 9, 1)),
		[]byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 6, 0)),
		[]byte(fmt.Sprintf("sender-0-1=peer=%d", 9)),
		[]byte(fmt.Sprintf("sender-1-1=peer=%d", 8)),
		[]byte(fmt.Sprintf("sender-2-1=peer=%d", 5)),
		[]byte(fmt.Sprintf("sender-3-1=peer=%d", 4)),
	}

	reapedTxs := txmp.ReapTxs(ReapLimits{MaxTxs: utils.Some(uint64(len(expectedReapedTxs)))})
	require.Equal(t, expectedReapedTxs, reapedTxs)
}

func TestTxMempool_RemoveCacheWhenPendingTxIsFull(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	cfg := TestConfig()
	cfg.CacheSize = 10
	cfg.Size = 1
	cfg.PendingSize = 0
	txmp := NewTxMempool(cfg, proxy.New(client, proxy.NopMetrics()), NopMetrics(), NopTxConstraintsFetcher)

	firstTx := []byte("sender-0=peer=100")
	_, err := txmp.CheckTx(ctx, firstTx)
	require.NoError(t, err)

	// The store only reports mempool-full once insertion crosses the hard limit
	// and compaction drops the newly inserted low-priority tx.
	_, err = txmp.CheckTx(ctx, []byte("sender-1=peer=50"))
	require.NoError(t, err)

	rejectedTx := []byte("sender-2=peer=1")
	_, err = txmp.CheckTx(ctx, rejectedTx)
	require.ErrorIs(t, err, errMempoolFull)

	require.Equal(t, 1, txmp.Size())
	// The rejected transaction should be removed from cache so it can be retried later.
	_, rejectedInCache := txmp.cache.cacheMap[txmp.cache.toCacheKey(types.Tx(rejectedTx).Hash())]
	require.False(t, rejectedInCache)

	_, err = txmp.CheckTx(ctx, rejectedTx)
	require.ErrorIs(t, err, errMempoolFull)
	_, rejectedInCache = txmp.cache.cacheMap[txmp.cache.toCacheKey(types.Tx(rejectedTx).Hash())]
	require.False(t, rejectedInCache)
}

func TestTxMempool_EVMEviction(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)
	txmp.config.Size = 1

	address1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	address2 := "0xfD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	// Add first transaction with priority 1
	_, err := txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 0)))
	require.NoError(t, err)

	// This should evict the previous tx (priority 1 < priority 2)
	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 2, 0)))
	require.NoError(t, err)
	// Increase mempool size to 2
	txmp.config.Size = 2
	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 3, 1)))
	require.NoError(t, err)
	require.Equal(t, 0, txmp.PendingSize())

	// This would evict the tx with priority 2 and cause the tx with priority 3 to go pending
	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 4, 0)))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return txmp.NumTxsNotPending() == 1 && txmp.PendingSize() == 1
	}, 5*time.Second, 100*time.Millisecond, "Expected mempool state not reached")

	// Verify final state
	require.Equal(t, 1, txmp.NumTxsNotPending())
	require.Equal(t, 1, txmp.PendingSize())

	tx := txmp.txStore.AllReady()[0]
	require.Equal(t, int64(4), tx.priority) // Should be the highest priority transaction

	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 5, 1)))
	require.NoError(t, err)
	require.Equal(t, 2, txmp.NumTxsNotPending())

	//TODO: txmp.removeTx(tx, true, false, true)
	// Should not reenqueue
	require.Equal(t, 1, txmp.NumTxsNotPending())

	require.Eventually(t, func() bool {
		return txmp.PendingSize() == 1
	}, 5*time.Second, 100*time.Millisecond, "Expected pendingTxs size not reached")
	require.Equal(t, 1, txmp.PendingSize())
}

func TestTxMempool_CheckTxSamePeer(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	prefix := make([]byte, 20)
	_, err := rng.Read(prefix)
	require.NoError(t, err)

	tx := []byte(fmt.Sprintf("sender-0=%X=%d", prefix, 50))

	_, err = txmp.CheckTx(ctx, tx)
	require.NoError(t, err)
	_, err = txmp.CheckTx(ctx, tx)
	require.Error(t, err)
}

func TestTxMempool_ConcurrentTxs(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	checkTxDone := make(chan struct{})

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		for i := 0; i < 20; i++ {
			_ = checkTxs(ctx, t, txmp, 100)
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
			reapedTxs := txmp.ReapTxs(ReapLimits{MaxTxs: utils.Some(uint64(200))})
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
				require.NoError(t, txmp.Update(ctx, height, reapedTxs, responses, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
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

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 500, NopTxConstraintsFetcher)
	txmp.height = 100
	txmp.config.TTLNumBlocks = utils.Some(int64(10))

	tTxs := checkTxs(ctx, t, txmp, 100)
	require.Equal(t, len(tTxs), txmp.Size())

	// reap 5 txs at the next height -- no txs should expire
	reapedTxs := txmp.ReapTxs(ReapLimits{MaxTxs: utils.Some(uint64(5))})
	responses := make([]*abci.ExecTxResult, len(reapedTxs))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, txmp.height+1, reapedTxs, responses, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	require.Equal(t, 95, txmp.Size())

	// check more txs at height 101
	_ = checkTxs(ctx, t, txmp, 50)
	require.Equal(t, 145, txmp.Size())

	// Reap 5 txs at a height that would expire all the transactions from before
	// the previous Update (height 100).
	//
	// NOTE: When we reap txs below, we do not know if we're picking txs from the
	// initial CheckTx calls or from the second round of CheckTx calls. Thus, we
	// cannot guarantee that all 95 txs are remaining that should be expired and
	// removed. However, we do know that that at most 95 txs can be expired and
	// removed.
	reapedTxs = txmp.ReapTxs(ReapLimits{MaxTxs: utils.Some(uint64(5))})
	responses = make([]*abci.ExecTxResult, len(reapedTxs))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, txmp.height+10, reapedTxs, responses, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	require.GreaterOrEqual(t, txmp.Size(), 45)
}

func TestMempoolExpiration(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	txmp.config.TTLDuration = utils.Some(time.Nanosecond) // we want tx to expire immediately
	txmp.config.RemoveExpiredTxsFromQueue = true
	txs := checkTxs(ctx, t, txmp, 100)
	require.Equal(t, len(txs), txmp.Size())
	time.Sleep(time.Millisecond)
	//txmp.purgeExpiredTxs(txmp.height)
	require.Equal(t, 0, txmp.Size())
}

// TestReapMaxBytesMaxGas_EVMFirst verifies that ReapMaxBytesMaxGas returns
// EVM transactions first, followed by non-EVM transactions.
func TestReapMaxBytesMaxGas_EVMFirst(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)

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
		_, err := txmp.CheckTx(ctx, tx)
		require.NoError(t, err)
	}

	require.Equal(t, 5, txmp.Size())

	// Reap all transactions
	reapedTxs := txmp.ReapTxs(ReapLimits{})
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

func TestBlockFailedTxNotReAdmittedAfterSecondFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := &application{Application: kvstore.NewApplication()}
	txmp := setup(t, proxy.New(app, proxy.NopMetrics()), 500, NopTxConstraintsFetcher)

	tx := types.Tx("sender-0-0=key=1000")

	// Submit the tx — should enter the mempool
	_, err := txmp.CheckTx(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())

	// Simulate block inclusion where the tx fails (non-OK code)
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: 11}, // out of gas
	}, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	// Tx should be removed from the mempool
	require.Equal(t, 0, txmp.Size())

	// First failure: tx should have been removed from cache, allowing re-entry
	_, err = txmp.CheckTx(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())

	// Simulate a second block failure for the same tx
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 2, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: 11}, // out of gas again
	}, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	require.Equal(t, 0, txmp.Size())

	// Second failure: tx should remain in cache — CheckTx should reject it
	_, err = txmp.CheckTx(ctx, tx)
	require.Equal(t, ErrTxInCache, err)
	require.Equal(t, 0, txmp.Size())

	// A different tx (different hash) should still be admitted
	differentTx := types.Tx("sender-0-0=key=2000")
	_, err = txmp.CheckTx(ctx, differentTx)
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())
}

func TestBlockFailedTxTrackerClearedOnSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := &application{Application: kvstore.NewApplication()}
	txmp := setup(t, proxy.New(app, proxy.NopMetrics()), 500, NopTxConstraintsFetcher)

	tx := types.Tx("sender-0-0=key=1000")
	txHash := tx.Hash()

	// Submit and fail once in a block
	_, err := txmp.CheckTx(ctx, tx)
	require.NoError(t, err)
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: 11},
	}, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	// Re-enter the mempool (first failure allows retry)
	_, err = txmp.CheckTx(ctx, tx)
	require.NoError(t, err)

	// This time the tx succeeds in the block
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 2, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: abci.CodeTypeOK},
	}, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	// Success clears the failure tracker. Simulate LRU eviction of the
	// main cache entry so we can verify the tracker was actually reset.
	txmp.cache.Remove(txHash)

	// Tx should now be re-admittable
	_, err = txmp.CheckTx(ctx, tx)
	require.NoError(t, err)

	// Fail again in a block — this should be treated as a fresh first failure
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 3, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: 11},
	}, utils.OrPanic1(txmp.txConstraintsFetcher()), true))
	txmp.Unlock()

	// First-failure grace should be restored: tx allowed to re-enter
	_, err = txmp.CheckTx(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())
}
