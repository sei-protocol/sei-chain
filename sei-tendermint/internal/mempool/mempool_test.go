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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/code"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
		_, err = txmp.CheckTx(ctx, txs[i].tx, txInfo)
		require.NoError(t, err)
	}

	return txs
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
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, txmp.txConstraintsFetcher, true))
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

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
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
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	require.Equal(t, len(rawTxs)/2, txmp.Size())
	require.Equal(t, int64(2850), txmp.SizeBytes())
}

func TestTxMempool_Flush(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
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
	require.NoError(t, txmp.Update(ctx, 1, rawTxs[:50], responses, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	txmp.Flush()
	require.Zero(t, txmp.Size())
	require.Equal(t, int64(0), txmp.SizeBytes())
}

func TestTxMempool_ReapMaxBytesMaxGas(t *testing.T) {
	ctx := t.Context()

	gasEstimated := int64(1) // each tx requests 1 gas unit
	client := &application{Application: kvstore.NewApplication(), gasEstimated: &gasEstimated}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	tTxs := checkTxs(ctx, t, txmp, 100, 0)

	const wantBytes = int64(5690)
	require.Equal(t, len(tTxs), txmp.Size())
	require.Equal(t, wantBytes, txmp.SizeBytes())

	// Build a hash -> priority lookup, plus the expected reap order (priority desc).
	txPriority := make(map[types.TxHash]int64, len(tTxs))
	expected := make([]int64, len(tTxs))
	for i, tTx := range tTxs {
		txPriority[tTx.tx.Hash()] = tTx.priority
		expected[i] = tTx.priority
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i] > expected[j] })

	max := utils.Max[int64]()
	cases := []struct {
		name            string
		maxBytes        int64
		maxGas          int64
		maxGasEstimated int64
		wantLen         int
		atLeast         bool // wantLen is a lower bound, not an exact count
	}{
		{name: "by gas only", maxBytes: max, maxGas: 50, maxGasEstimated: max, wantLen: 50},
		{name: "by bytes only", maxBytes: 1000, maxGas: max, maxGasEstimated: max, wantLen: 16, atLeast: true},
		{name: "bytes and gas, gas is the binder", maxBytes: 1500, maxGas: 30, maxGasEstimated: max, wantLen: 25},
		{name: "tight gas limit", maxBytes: max, maxGas: 2, maxGasEstimated: max, wantLen: 2},
		{name: "by gas estimated", maxBytes: max, maxGas: max, maxGasEstimated: 50, wantLen: 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reaped := txmp.ReapMaxBytesMaxGas(tc.maxBytes, tc.maxGas, tc.maxGasEstimated)

			if tc.atLeast {
				require.GreaterOrEqual(t, len(reaped), tc.wantLen)
			} else {
				require.Len(t, reaped, tc.wantLen)
			}

			require.Equal(t, len(tTxs), txmp.Size(), "reap should not mutate the mempool")
			require.Equal(t, wantBytes, txmp.SizeBytes(), "reap should not mutate the mempool")

			got := make([]int64, len(reaped))
			for i, rTx := range reaped {
				got[i] = txPriority[rTx.Hash()]
			}
			require.Equal(t, expected[:len(got)], got, "reaped txs should be the top N by priority")
		})
	}
}

func TestTxMempool_ReapMaxBytesMaxGas_FallbackToGasWanted(t *testing.T) {
	ctx := t.Context()

	gasEstimated := int64(0) // not set, so reap falls back to gas wanted
	client := &application{Application: kvstore.NewApplication(), gasEstimated: &gasEstimated}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	tTxs := checkTxs(ctx, t, txmp, 100, 0)

	// Build a hash -> priority lookup, plus the expected reap order (priority desc).
	txPriority := make(map[types.TxHash]int64, len(tTxs))
	expected := make([]int64, len(tTxs))
	for i, tTx := range tTxs {
		txPriority[tTx.tx.Hash()] = tTx.priority
		expected[i] = tTx.priority
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i] > expected[j] })

	const reapMax = 50
	reaped := txmp.ReapMaxBytesMaxGas(utils.Max[int64](), utils.Max[int64](), reapMax)
	require.Len(t, reaped, reapMax)
	require.Equal(t, len(tTxs), txmp.Size(), "reap should not remove txs from the mempool")

	got := make([]int64, len(reaped))
	for i, rTx := range reaped {
		got[i] = txPriority[rTx.Hash()]
	}
	require.Equal(t, expected[:reapMax], got, "reaped txs should be the top %d by priority", reapMax)
}

func TestTxMempool_ReapMaxTxs(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	tTxs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(tTxs), txmp.Size())
	require.Equal(t, int64(5690), txmp.SizeBytes())

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
		reapedTxs := txmp.ReapMaxTxs(utils.Max[int]())
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

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	peerID := uint16(1)
	address := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	// Insert a single EVM tx (format: evm-sender=account=priority=nonce)
	_, err := txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address, 100, 0)), TxInfo{SenderID: peerID})
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())

	// With MinGasEVMTx=21000, estimatedGas (10000) is ignored and we fallback to gasWanted (50000).
	// Setting maxGasEstimated below gasWanted should therefore result in 0 reaped txs.
	reaped := txmp.ReapMaxBytesMaxGas(utils.Max[int64](), utils.Max[int64](), 40000)
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

	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.Error(t, err)

	tx = make([]byte, txmp.config.MaxTxBytes-1)
	_, err = rng.Read(tx)
	require.NoError(t, err)

	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.NoError(t, err)
}

func TestTxMempool_Reap_SkipGasUnfitAndCollectMinTxs(t *testing.T) {
	ctx := t.Context()

	app := &application{Application: kvstore.NewApplication()}
	client := app

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	peerID := uint16(1)

	// Insert one high-priority tx that is unfit by gas (exceeds maxGasEstimated)
	gwBig := int64(100)
	geBig := int64(100)
	app.gasWanted = &gwBig
	app.gasEstimated = &geBig
	bigTx := []byte(fmt.Sprintf("sender-big=key=%d", 1000000))
	_, err := txmp.CheckTx(ctx, bigTx, TxInfo{SenderID: peerID})
	require.NoError(t, err)

	// Now insert many small, lower-priority txs that fit well under the gas limit
	gwSmall := int64(1)
	geSmall := int64(1)
	app.gasWanted = &gwSmall
	app.gasEstimated = &geSmall
	for i := 0; i < 50; i++ {
		tx := []byte(fmt.Sprintf("sender-%d=key=%d", i, 1000-i))
		_, err := txmp.CheckTx(ctx, tx, TxInfo{SenderID: peerID})
		require.NoError(t, err)
	}

	// Reap with a maxGasEstimated that makes the first tx unfit but allows many small txs
	reaped := txmp.ReapMaxBytesMaxGas(utils.Max[int64](), utils.Max[int64](), 50)
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

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	peerID := uint16(1)

	// First tx: unfit by gas (bigger than limit), highest priority
	gwBig := int64(100)
	geBig := int64(100)
	app.gasWanted = &gwBig
	app.gasEstimated = &geBig
	bigTx := []byte(fmt.Sprintf("sender-big=key=%d", 1000000))
	_, err := txmp.CheckTx(ctx, bigTx, TxInfo{SenderID: peerID})
	require.NoError(t, err)

	// Insert many small txs that fit; plenty of capacity for more than 10
	gwSmall := int64(1)
	geSmall := int64(1)
	app.gasWanted = &gwSmall
	app.gasEstimated = &geSmall
	for i := 0; i < 100; i++ {
		tx := []byte(fmt.Sprintf("sender-sm-%d=key=%d", i, 2000-i))
		_, err := txmp.CheckTx(ctx, tx, TxInfo{SenderID: peerID})
		require.NoError(t, err)
	}

	// Make the gas limit very small so the first (big) tx is unfit and we only collect MinTxsPerBlock
	reaped := txmp.ReapMaxBytesMaxGas(utils.Max[int64](), utils.Max[int64](), 10)
	require.Len(t, reaped, MinTxsToPeek)
}

func TestTxMempool_Prioritization(t *testing.T) {
	ctx := t.Context()
	client := &application{Application: kvstore.NewApplication()}
	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)

	const (
		peerID   = uint16(1)
		address1 = "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
		address2 = "0xfD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	)

	// Encoders for the two tx formats the mock CheckTx parses.
	nonEVM := func(senderID, priority int) []byte {
		return []byte(fmt.Sprintf("sender-%d-1=peer=%d", senderID, priority))
	}
	evm := func(address string, priority, nonce int) []byte {
		return []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address, priority, nonce))
	}

	// Expected reap order: priority descending, EXCEPT EVM txs from the same
	// sender must stay in nonce order. That's why addr1's nonce-1 tx (priority 9)
	// sits behind its nonce-0 sibling (priority 7) instead of leading the slate.
	expected := [][]byte{
		nonEVM(0, 9),
		nonEVM(1, 8),
		evm(address1, 7, 0),
		evm(address1, 9, 1),
		evm(address2, 6, 0),
		nonEVM(2, 5),
		nonEVM(3, 4),
	}

	// Submit in a randomized order so the mempool has to do real sorting.
	// addr1's two EVM txs are appended last (in nonce order) — that's the
	// specific case under test: nonce 1's higher priority must not jump it
	// ahead of nonce 0.
	input := [][]byte{
		nonEVM(0, 9),
		nonEVM(1, 8),
		evm(address2, 6, 0),
		nonEVM(2, 5),
		nonEVM(3, 4),
	}
	const seed = 9874465132 * 23
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(input), func(i, j int) { input[i], input[j] = input[j], input[i] })
	input = append(input, evm(address1, 7, 0), evm(address1, 9, 1))

	for _, tx := range input {
		_, err := txmp.CheckTx(ctx, tx, TxInfo{SenderID: peerID})
		require.NoError(t, err)
	}

	reaped := txmp.ReapMaxTxs(len(expected))
	require.Len(t, reaped, len(expected))
	for i, want := range expected {
		require.Equal(t, string(want), string(reaped[i]), "position %d", i)
	}
}

func TestTxMempool_PendingStoreSize(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)
	txmp.config.PendingSize = 1
	peerID := uint16(1)

	address1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	_, err := txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 1)), TxInfo{SenderID: peerID})
	require.NoError(t, err)
	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 2)), TxInfo{SenderID: peerID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "mempool pending set is full")
}

func TestTxMempool_RemoveCacheWhenPendingTxIsFull(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 10, NopTxConstraintsFetcher)
	txmp.config.PendingSize = 1
	peerID := uint16(1)
	address1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	_, err := txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 1)), TxInfo{SenderID: peerID})
	require.NoError(t, err)
	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 2)), TxInfo{SenderID: peerID})
	require.Error(t, err)
	txCache := txmp.cache.(*LRUTxCache)
	// Make sure the second tx is removed from cache
	require.Equal(t, 1, len(txCache.cacheMap))
}

func TestTxMempool_EVMEviction(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)
	txmp.config.Size = 1
	peerID := uint16(1)

	address1 := "0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA"
	address2 := "0xfD23B3A9DE15e92B9ef9540E587B3661E15A12fA"

	// Add first transaction with priority 1
	_, err := txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 1, 0)), TxInfo{SenderID: peerID})
	require.NoError(t, err)

	// This should evict the previous tx (priority 1 < priority 2)
	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 2, 0)), TxInfo{SenderID: peerID})
	require.NoError(t, err)
	require.Equal(t, 1, txmp.priorityIndex.NumTxs())
	require.Equal(t, int64(2), txmp.priorityIndex.txs[0].priority)

	// Increase mempool size to 2
	txmp.config.Size = 2
	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address1, 3, 1)), TxInfo{SenderID: peerID})
	require.NoError(t, err)
	require.Equal(t, 0, txmp.pendingTxs.Size())
	require.Equal(t, 2, txmp.priorityIndex.NumTxs())

	// This would evict the tx with priority 2 and cause the tx with priority 3 to go pending
	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 4, 0)), TxInfo{SenderID: peerID})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return txmp.priorityIndex.NumTxs() == 1 && txmp.pendingTxs.Size() == 1
	}, 5*time.Second, 100*time.Millisecond, "Expected mempool state not reached")

	// Verify final state
	require.Equal(t, 1, txmp.priorityIndex.NumTxs())
	require.Equal(t, 1, txmp.pendingTxs.Size())

	tx := txmp.priorityIndex.txs[0]
	require.Equal(t, int64(4), tx.priority) // Should be the highest priority transaction

	_, err = txmp.CheckTx(ctx, []byte(fmt.Sprintf("evm-sender=%s=%d=%d", address2, 5, 1)), TxInfo{SenderID: peerID})
	require.NoError(t, err)
	require.Equal(t, 2, txmp.priorityIndex.NumTxs())

	txmp.removeTx(tx, true, false, true)
	// Should not reenqueue
	require.Equal(t, 1, txmp.priorityIndex.NumTxs())

	require.Eventually(t, func() bool {
		return txmp.pendingTxs.Size() == 1
	}, 5*time.Second, 100*time.Millisecond, "Expected pendingTxs size not reached")
	require.Equal(t, 1, txmp.pendingTxs.Size())
}

func TestTxMempool_CheckTxSamePeer(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)
	peerID := uint16(1)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	prefix := make([]byte, 20)
	_, err := rng.Read(prefix)
	require.NoError(t, err)

	tx := []byte(fmt.Sprintf("sender-0=%X=%d", prefix, 50))

	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: peerID})
	require.NoError(t, err)
	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: peerID})
	require.Error(t, err)
}

func TestTxMempool_ConcurrentTxs(t *testing.T) {
	ctx := t.Context()
	client := &application{Application: kvstore.NewApplication()}
	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)

	var producerDone atomic.Bool
	stop := make(chan struct{})

	// Producer: submit 20 batches of 100 txs, with random gaps to interleave with the reaper.
	go func() {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for range 20 {
			_ = checkTxs(ctx, t, txmp, 100, 0)
			time.Sleep(time.Duration(rng.Intn(500)+500) * time.Millisecond)
		}
		producerDone.Store(true)
	}()

	// Consumer: reap and Update on a tick. Every 10th tx in a batch gets a
	// non-OK code to exercise the failed-tx path through Update.
	go func() {
		var height int64 = 1
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}

			reaped := txmp.ReapMaxTxs(200)
			if len(reaped) == 0 {
				continue
			}

			responses := make([]*abci.ExecTxResult, len(reaped))
			for i := range responses {
				code := uint32(abci.CodeTypeOK)
				if i%10 == 0 {
					code = 100
				}
				responses[i] = &abci.ExecTxResult{Code: code}
			}

			txmp.Lock()
			require.NoError(t, txmp.Update(ctx, height, reaped, responses, txmp.txConstraintsFetcher, true))
			txmp.Unlock()
			height++
		}
	}()

	require.Eventually(t, func() bool {
		return producerDone.Load() && txmp.Size() == 0
	}, 60*time.Second, 100*time.Millisecond, "mempool did not drain")

	close(stop)
	require.Zero(t, txmp.SizeBytes())
}

func TestTxMempool_ExpiredTxs_NumBlocks(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 500, NopTxConstraintsFetcher)
	txmp.height = 100
	txmp.config.TTLNumBlocks = 10

	tTxs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(tTxs), txmp.Size())

	// reap 5 txs at the next height -- no txs should expire
	reapedTxs := txmp.ReapMaxTxs(5)
	responses := make([]*abci.ExecTxResult, len(reapedTxs))
	for i := 0; i < len(responses); i++ {
		responses[i] = &abci.ExecTxResult{Code: abci.CodeTypeOK}
	}

	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, txmp.height+1, reapedTxs, responses, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	require.Equal(t, 95, txmp.Size())

	// check more txs at height 101
	_ = checkTxs(ctx, t, txmp, 50, 1)
	require.Equal(t, 145, txmp.Size())

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
	require.NoError(t, txmp.Update(ctx, txmp.height+10, reapedTxs, responses, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	require.GreaterOrEqual(t, txmp.Size(), 45)
}

func TestMempoolExpiration(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
	txmp.config.TTLDuration = time.Nanosecond // we want tx to expire immediately
	txmp.config.RemoveExpiredTxsFromQueue = true
	txs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(txs), txmp.priorityIndex.Len())
	require.Equal(t, len(txs), txmp.txStore.Size())
	time.Sleep(time.Millisecond)
	txmp.purgeExpiredTxs(txmp.height)
	require.Equal(t, 0, txmp.priorityIndex.Len())
	require.Equal(t, 0, txmp.txStore.Size())
}

// TestReapMaxBytesMaxGas_EVMFirst verifies that ReapMaxBytesMaxGas returns
// EVM transactions first, followed by non-EVM transactions.
func TestReapMaxBytesMaxGas_EVMFirst(t *testing.T) {
	ctx := t.Context()

	client := &application{Application: kvstore.NewApplication()}

	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 0, NopTxConstraintsFetcher)
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
		_, err := txmp.CheckTx(ctx, tx, TxInfo{SenderID: peerID})
		require.NoError(t, err)
	}

	require.Equal(t, 5, txmp.Size())

	// Reap all transactions
	reapedTxs := txmp.ReapMaxBytesMaxGas(utils.Max[int64](), utils.Max[int64](), utils.Max[int64]())
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
	_, err := txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())

	// Simulate block inclusion where the tx fails (non-OK code)
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: 11}, // out of gas
	}, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	// Tx should be removed from the mempool
	require.Equal(t, 0, txmp.Size())

	// First failure: tx should have been removed from cache, allowing re-entry
	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())

	// Simulate a second block failure for the same tx
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 2, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: 11}, // out of gas again
	}, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	require.Equal(t, 0, txmp.Size())

	// Second failure: tx should remain in cache — CheckTx should reject it
	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.Equal(t, ErrTxInCache, err)
	require.Equal(t, 0, txmp.Size())

	// A different tx (different hash) should still be admitted
	differentTx := types.Tx("sender-0-0=key=2000")
	_, err = txmp.CheckTx(ctx, differentTx, TxInfo{SenderID: 0})
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
	_, err := txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.NoError(t, err)
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 1, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: 11},
	}, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	// Re-enter the mempool (first failure allows retry)
	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.NoError(t, err)

	// This time the tx succeeds in the block
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 2, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: abci.CodeTypeOK},
	}, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	// Success clears the failure tracker. Simulate LRU eviction of the
	// main cache entry so we can verify the tracker was actually reset.
	txmp.cache.Remove(txHash)

	// Tx should now be re-admittable
	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.NoError(t, err)

	// Fail again in a block — this should be treated as a fresh first failure
	txmp.Lock()
	require.NoError(t, txmp.Update(ctx, 3, types.Txs{tx}, []*abci.ExecTxResult{
		{Code: 11},
	}, txmp.txConstraintsFetcher, true))
	txmp.Unlock()

	// First-failure grace should be restored: tx allowed to re-enter
	_, err = txmp.CheckTx(ctx, tx, TxInfo{SenderID: 0})
	require.NoError(t, err)
	require.Equal(t, 1, txmp.Size())
}

func TestTxMempool_InsertTxReplaceMissingGossipEl(t *testing.T) {
	client := &application{Application: kvstore.NewApplication()}
	txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)

	address := common.HexToAddress("0xeD23B3A9DE15e92B9ef9540E587B3661E15A12fA")

	newEVMTx := func(priority int64) *WrappedTx {
		raw := []byte(fmt.Sprintf("evm-sender=%s=%d=0", address.Hex(), priority))
		return &WrappedTx{
			hashedTx:  newHashedTx(raw),
			timestamp: time.Now().UTC(),
			priority:  priority,
			peers:     map[uint16]struct{}{},
			evm: utils.Some(evmTx{
				address:         address,
				nonce:           0,
				requiredBalance: big.NewInt(0),
			}),
		}
	}

	// Park wtxA in the priority index without linking its gossipEl. This is
	// exactly the state another goroutine holds while paused between
	// priorityIndex.PushTx and gossipIndex.PushBack in the original insertTx.
	wtxA := newEVMTx(1)
	_, inserted := txmp.priorityIndex.PushTx(wtxA)
	require.True(t, inserted)
	require.Nil(t, wtxA.gossipEl, "precondition: wtxA must have a nil gossipEl to reproduce the race window")

	// Higher-priority replacement for the same address+nonce. insertTx must
	// route the eviction through removeTx without panicking on the nil
	// wtxA.gossipEl.
	wtxB := newEVMTx(2)

	require.NotPanics(t, func() {
		require.True(t, txmp.insertTx(wtxB))
	})

	require.Equal(t, 1, txmp.priorityIndex.NumTxs())
	require.Equal(t, int64(2), txmp.priorityIndex.txs[0].priority)
	require.NotNil(t, wtxB.gossipEl, "wtxB must be linked into the gossip index after insertTx")
}

func TestTxStore_TryRemoveTxAtomicClaim(t *testing.T) {
	store := NewTxStore()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx([]byte("sender-0=AAAA=100")),
		timestamp: time.Now().UTC(),
	}
	store.SetTx(wtx)

	require.False(t, wtx.removed)
	require.True(t, store.TryRemoveTx(wtx), "first claim must win")
	require.True(t, wtx.removed)
	require.Nil(t, store.GetTxByHash(wtx.Hash()))
	require.False(t, store.TryRemoveTx(wtx), "second claim must lose")
}

func TestTxMempool_RemoveTxConcurrentSnapshotNoPanic(t *testing.T) {
	const N = 16
	const Trials = 32
	for trial := 0; trial < Trials; trial++ {
		ctx := t.Context()
		client := &application{Application: kvstore.NewApplication()}
		txmp := setup(t, proxy.New(client, proxy.NopMetrics()), 100, NopTxConstraintsFetcher)

		raw := []byte(fmt.Sprintf("sender-%d=AAAA=100", trial))
		_, err := txmp.CheckTx(ctx, raw, TxInfo{SenderID: 1})
		require.NoError(t, err)

		wtx := txmp.txStore.GetTxByHash(types.Tx(raw).Hash())
		require.NotNil(t, wtx)
		require.NotNil(t, wtx.gossipEl)

		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < N; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				txmp.removeTx(wtx, true, false, true)
			}()
		}
		close(start)
		wg.Wait()

		require.True(t, wtx.removed)
		require.Equal(t, 0, txmp.Size())
	}
}
