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

	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/example/code"
	"github.com/tendermint/tendermint/abci/example/kvstore"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

// application extends the KV store application by overriding CheckTx to provide
// transaction priority based on the value in the key/value pair.
type application struct {
	*kvstore.Application
}

type testTx struct {
	tx       types.Tx
	priority int64
}

func (app *application) CheckTx(_ context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTxV2, error) {
	var (
		priority int64
		sender   string
	)

	// infer the priority from the raw transaction value (sender=key=value)
	parts := bytes.Split(req.Tx, []byte("="))
	if len(parts) == 3 {
		v, err := strconv.ParseInt(string(parts[2]), 10, 64)
		if err != nil {
			return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
				Priority:  priority,
				Code:      100,
				GasWanted: 1,
			}}, nil
		}

		priority = v
		sender = string(parts[0])
	} else {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
			Priority:  priority,
			Code:      101,
			GasWanted: 1,
		}}, nil
	}

	return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{
		Priority:  priority,
		Sender:    sender,
		Code:      code.CodeTypeOK,
		GasWanted: 1,
	}}, nil
}

func setup(t testing.TB, app abciclient.Client, cacheSize int, options ...TxMempoolOption) *TxMempool {
	t.Helper()

	logger := log.NewNopLogger()

	cfg, err := config.ResetTestRoot(t.TempDir(), strings.ReplaceAll(t.Name(), "/", "|"))
	require.NoError(t, err)
	cfg.Mempool.CacheSize = cacheSize

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

func (e *TestPeerEvictor) Errored(peerID types.NodeID, err error) {
	e.evicting[peerID] = struct{}{}
}

func TestTxMempool_TxsAvailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

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

	require.Equal(t, len(rawTxs)/2, txmp.Size())
	require.Equal(t, int64(2850), txmp.SizeBytes())
}

func TestTxMempool_Flush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

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
		reapedTxs := txmp.ReapMaxBytesMaxGas(-1, 50)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, int64(5690), txmp.SizeBytes())
		require.Len(t, reapedTxs, 50)
	}()

	// reap by transaction bytes only
	wg.Add(1)
	go func() {
		defer wg.Done()
		reapedTxs := txmp.ReapMaxBytesMaxGas(1000, -1)
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
		reapedTxs := txmp.ReapMaxBytesMaxGas(1500, 30)
		ensurePrioritized(reapedTxs)
		require.Equal(t, len(tTxs), txmp.Size())
		require.Equal(t, int64(5690), txmp.SizeBytes())
		require.Len(t, reapedTxs, 25)
	}()

	wg.Wait()
}

func TestTxMempool_ReapMaxTxs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

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

func TestTxMempool_CheckTxExceedsMaxSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)
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

func TestTxMempool_CheckTxSamePeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

	txmp := setup(t, client, 500)
	txmp.height = 100
	txmp.config.TTLNumBlocks = 10

	tTxs := checkTxs(ctx, t, txmp, 100, 0)
	require.Equal(t, len(tTxs), txmp.Size())
	require.Equal(t, 100, txmp.heightIndex.Size())

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
	require.Equal(t, 95, txmp.heightIndex.Size())

	// check more txs at height 101
	_ = checkTxs(ctx, t, txmp, 50, 1)
	require.Equal(t, 145, txmp.Size())
	require.Equal(t, 145, txmp.heightIndex.Size())

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
	require.GreaterOrEqual(t, txmp.heightIndex.Size(), 45)
}

func TestTxMempool_CheckTxPostCheckError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		testCase := tc
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
			if err := client.Start(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.Wait)

			postCheckFn := func(_ types.Tx, _ *abci.ResponseCheckTx) error {
				return testCase.err
			}
			txmp := setup(t, client, 0, WithPostCheck(postCheckFn))
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			tx := make([]byte, txmp.config.MaxTxBytes-1)
			_, err := rng.Read(tx)
			require.NoError(t, err)

			callback := func(res *abci.ResponseCheckTx) {
				expectedErrString := ""
				if testCase.err != nil {
					expectedErrString = testCase.err.Error()
					require.Equal(t, expectedErrString, txmp.postCheck(tx, res).Error())
				} else {
					require.Equal(t, nil, txmp.postCheck(tx, res))
				}
			}
			if testCase.err == nil {
				require.NoError(t, txmp.CheckTx(ctx, tx, callback, TxInfo{SenderID: 0}))
			} else {
				err = txmp.CheckTx(ctx, tx, callback, TxInfo{SenderID: 0})
				fmt.Print(err.Error())
			}
		})
	}
}

func TestTxMempool_FailedCheckTxCount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)

	postCheckFn := func(_ types.Tx, _ *abci.ResponseCheckTx) error {
		return nil
	}
	txmp := setup(t, client, 0, WithPostCheck(postCheckFn))
	tx := []byte("bad tx")

	callback := func(res *abci.ResponseCheckTx) {
		require.Equal(t, nil, txmp.postCheck(tx, res))
	}
	require.Equal(t, uint64(0), txmp.GetPeerFailedCheckTxCount("sender"))
	// bad tx
	require.NoError(t, txmp.CheckTx(ctx, tx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.Equal(t, uint64(1), txmp.GetPeerFailedCheckTxCount("sender"))

	// bad tx again
	require.NoError(t, txmp.CheckTx(ctx, tx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.Equal(t, uint64(2), txmp.GetPeerFailedCheckTxCount("sender"))

	tx = []byte("sender=key=1")
	// good tx
	require.NoError(t, txmp.CheckTx(ctx, tx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.Equal(t, uint64(2), txmp.GetPeerFailedCheckTxCount("sender"))

	// enable blacklisting
	txmp.config.CheckTxErrorBlacklistEnabled = true
	txmp.config.CheckTxErrorThreshold = 0
	tx = []byte("bad tx")
	require.NoError(t, txmp.CheckTx(ctx, tx, callback, TxInfo{SenderID: 0, SenderNodeID: "sender"}))
	require.True(t, txmp.peerManager.(*TestPeerEvictor).IsEvicted("sender"))
}

func TestAppendCheckTxErr(t *testing.T) {
	// Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := abciclient.NewLocalClient(log.NewNopLogger(), &application{Application: kvstore.NewApplication()})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Wait)
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
