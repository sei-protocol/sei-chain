package evmonly

import (
	"crypto/ecdsa"
	"math/big"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// TestExecutorOCCBlockSTMMatchesSequential runs blocks whose calls spread over enough entry points to
// run under Block-STM, with many dependency chains at once (hot counters, same-sender runs, readers of
// a hot recipient's and of the coinbase's balance, and writes that depend on an earlier transaction's)
// and a contract created and destroyed at the block's end. It
// requires each result to equal the sequential one and every transaction to stay within each retry
// budget and the combined start bound.
func TestExecutorOCCBlockSTMMatchesSequential(t *testing.T) {
	for _, withState := range []func(StateReader) Option{withTestState, withRowReadingTestState} {
		for _, workers := range []int{4, 16} {
			for seed := range uint64(4) {
				newState, req := blockSTMBlock(t, seed, 400, 65)
				seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withState(newState())).ExecuteBlock(t.Context(), req)
				require.NoError(t, err)
				phases := observeBlockSTM(t)
				occResult, err := blockSTMExecutor(workers, withState(newState())).ExecuteBlock(t.Context(), req)
				require.NoError(t, err)

				ran := phases()
				require.Len(t, ran, 1, "the block runs under Block-STM")
				require.Less(t, ran[0].maxFailures, occMaxTxIncarnations)
				require.LessOrEqual(t, ran[0].maxIncarnation, bstmParkBudget+occMaxTxIncarnations)
				require.LessOrEqual(t, ran[0].maxParks, bstmParkBudget)
				require.LessOrEqual(t, ran[0].maxValidationWaits, bstmValidationWaitBudget)
				require.LessOrEqual(t, ran[0].maxStarts, bstmStartBudget())
				require.Zero(t, ran[0].capped)
				require.False(t, occResult.OCCStats.Fallback, occResult.OCCStats.FallbackReason)
				require.Equal(t, seqResult.GasUsed, occResult.GasUsed)
				require.Equal(t, seqResult.Txs, occResult.Txs)
				require.Equal(t, seqResult.Receipts, occResult.Receipts)
				require.Equal(t, seqResult.ChangeSet, occResult.ChangeSet)
			}
		}
	}
}

// TestExecutorOCCBlockSTMNarrowsWithinItsBound runs fully contended blocks at sixteen workers, where the
// phase's aborts narrow it to occBlockSTMNarrowWorkers, and requires the narrowing to happen, no
// transaction to exceed a retry budget or the combined start bound, and each result to equal the
// sequential one.
func TestExecutorOCCBlockSTMNarrowsWithinItsBound(t *testing.T) {
	for seed := range uint64(12) {
		newState, req := blockSTMBlock(t, 100+seed, 400, 100)
		seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(newState())).ExecuteBlock(t.Context(), req)
		require.NoError(t, err)
		phases := observeBlockSTM(t)
		occResult, err := blockSTMExecutor(16, withTestState(newState())).ExecuteBlock(t.Context(), req)
		require.NoError(t, err)

		ran := phases()
		require.Len(t, ran, 1, "the block runs under Block-STM")
		require.NotZero(t, ran[0].narrowedAt, "the phase narrows")
		require.Less(t, ran[0].maxFailures, occMaxTxIncarnations)
		require.LessOrEqual(t, ran[0].maxIncarnation, bstmParkBudget+occMaxTxIncarnations)
		require.LessOrEqual(t, ran[0].maxParks, bstmParkBudget)
		require.LessOrEqual(t, ran[0].maxValidationWaits, bstmValidationWaitBudget)
		require.LessOrEqual(t, ran[0].maxStarts, bstmStartBudget())
		require.Zero(t, ran[0].capped)
		require.False(t, occResult.OCCStats.Fallback, occResult.OCCStats.FallbackReason)
		require.Equal(t, seqResult.Receipts, occResult.Receipts)
		require.Equal(t, seqResult.ChangeSet, occResult.ChangeSet)
	}
}

// TestExecutorOCCBlockSTMParkBudgetBinds runs the contended blocks of
// TestExecutorOCCBlockSTMNarrowsWithinItsBound with a park budget of one, so every transaction that stops
// at an estimate is held until the transactions below it commit, and requires the hold to happen, no
// transaction to stop at an estimate more than once, and each result to equal the sequential one.
func TestExecutorOCCBlockSTMParkBudgetBinds(t *testing.T) {
	budget := bstmParkBudget
	bstmParkBudget = 1
	t.Cleanup(func() { bstmParkBudget = budget })
	for seed := range uint64(6) {
		newState, req := blockSTMBlock(t, 100+seed, 400, 100)
		seqResult, err := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, withTestState(newState())).ExecuteBlock(t.Context(), req)
		require.NoError(t, err)
		phases := observeBlockSTM(t)
		occResult, err := blockSTMExecutor(16, withTestState(newState())).ExecuteBlock(t.Context(), req)
		require.NoError(t, err)

		ran := phases()
		require.Len(t, ran, 1, "the block runs under Block-STM")
		require.NotZero(t, ran[0].estimateAborts, "transactions stop at estimates")
		require.NotZero(t, ran[0].delayed, "the budget holds them")
		require.LessOrEqual(t, ran[0].maxParks, 1)
		require.Less(t, ran[0].maxFailures, occMaxTxIncarnations)
		require.LessOrEqual(t, ran[0].maxIncarnation, 1+occMaxTxIncarnations)
		require.LessOrEqual(t, ran[0].maxValidationWaits, bstmValidationWaitBudget)
		require.LessOrEqual(t, ran[0].maxStarts, bstmStartBudget())
		require.Zero(t, ran[0].capped)
		require.False(t, occResult.OCCStats.Fallback, occResult.OCCStats.FallbackReason)
		require.Equal(t, seqResult.GasUsed, occResult.GasUsed)
		require.Equal(t, seqResult.Receipts, occResult.Receipts)
		require.Equal(t, seqResult.ChangeSet, occResult.ChangeSet)
	}
}

// TestBlockSTMValidationWaitBudgetBindsAcrossWriters stops one transaction behind successive lower
// writers and requires the final stop to hold it without registering another waiter.
func TestBlockSTMValidationWaitBudgetBindsAcrossWriters(t *testing.T) {
	budget := bstmValidationWaitBudget
	bstmValidationWaitBudget = 3
	t.Cleanup(func() { bstmValidationWaitBudget = budget })

	s := newBlockSTMScheduler(5, 1)
	s.active.Add(1)
	inc, ok := s.tryIncarnate(4)
	require.True(t, ok)
	require.Zero(t, inc)
	s.recordStart(4)
	require.True(t, s.addValidationDependency(4, 0))

	firstWriter := &s.txs[0]
	firstWriter.mu.Lock()
	waiters := firstWriter.validationWaiters
	firstWriter.validationWaiters = nil
	firstWriter.mu.Unlock()
	require.Equal(t, []int{4}, waiters)
	s.releaseValidationWaiters(waiters)

	s.active.Add(1)
	inc, ok = s.tryIncarnate(4)
	require.True(t, ok)
	require.Zero(t, inc)
	s.recordStart(4)
	secondWriter := &s.txs[1]
	secondWriter.mu.Lock()
	secondWriter.status = bstmExecuted
	secondWriter.passed.Store(1)
	secondWriter.mu.Unlock()
	require.False(t, s.addValidationDependency(4, 1), "a writer that passed during registration does not park")

	s.recordStart(4)
	require.True(t, s.addValidationDependency(4, 2))
	thirdWriter := &s.txs[2]
	thirdWriter.mu.Lock()
	waiters = thirdWriter.validationWaiters
	thirdWriter.mu.Unlock()
	require.Empty(t, waiters, "the exhausted budget holds the transaction")

	state := &s.txs[4]
	state.mu.Lock()
	require.Equal(t, bstmDelayed, state.status)
	require.Equal(t, 3, state.validationWaits)
	require.Equal(t, 3, state.starts)
	state.mu.Unlock()
	require.EqualValues(t, 1, s.delayedCount.Load())
	require.Zero(t, s.active.Load())
}

// TestExecutorOCCBlockSTMReportsTheSameConflicts runs one Block-STM block twenty times at sixteen
// workers and requires the same conflict count and result every time.
func TestExecutorOCCBlockSTMReportsTheSameConflicts(t *testing.T) {
	newState, req := blockSTMBlock(t, 11, 600, 40)
	var first *BlockResult
	for range 20 {
		executor := blockSTMExecutor(16, withTestState(newState()))
		result, err := executor.ExecuteBlock(t.Context(), req)
		executor.Close()
		require.NoError(t, err)
		require.False(t, result.OCCStats.Fallback, result.OCCStats.FallbackReason)
		if first == nil {
			first = result
			require.NotZero(t, result.OCCStats.ConflictCount)
			continue
		}
		require.Equal(t, first.OCCStats.RerunCount, result.OCCStats.RerunCount)
		require.Equal(t, first.OCCStats.ConflictCount, result.OCCStats.ConflictCount)
		require.Equal(t, first.Receipts, result.Receipts)
		require.Equal(t, first.ChangeSet, result.ChangeSet)
	}
}

// TestBlockSTMLastWorkerToParkEndsTheBlock runs out of work on two workers whose claims overlap, so
// each fails the other's done check, and requires the second to park to end the block rather than
// wait for a wake-up nobody will send.
func TestBlockSTMLastWorkerToParkEndsTheBlock(t *testing.T) {
	s := newBlockSTMScheduler(1, 2)
	st := &s.txs[0]
	s.ready.clear(0)
	s.readyCount.Store(0)
	st.status, st.validatedInc = bstmExecuted, 0
	s.executed.set(0)
	s.execIdx.Store(1)
	s.valIdx.Store(1)

	s.active.Add(1) // the other worker's claim, still looking
	require.Equal(t, bstmNoTask, s.nextTask().kind)
	require.False(t, s.done.Load(), "a claim in flight fails the done check")
	s.active.Add(-1) // which then finds nothing and fails the check too

	parked := make(chan bool, 2)
	go func() { parked <- s.park() }()
	require.Eventually(t, func() bool { return s.parked.Load() == 1 }, 5*time.Second, time.Millisecond)
	go func() { parked <- s.park() }()
	for range 2 {
		select {
		case more := <-parked:
			require.False(t, more)
		case <-time.After(5 * time.Second):
			t.Fatal("every worker parked and the block never ended")
		}
	}
	require.True(t, s.done.Load())
}

// blockSTMExecutor returns an executor at workers that runs every parallel block under Block-STM.
func blockSTMExecutor(workers int, opts ...Option) *Executor {
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: workers}, opts...)
	executor.occPath = occPathBlockSTM
	return executor
}

// blockSTMBlock builds a block of many dependency chains (hot storage counters, same-sender nonce runs,
// readers of a hot recipient's and of the coinbase's balance, and a contract whose writes depend on an
// earlier transaction's) with calldata on every call and each sender's independent transfer sent to a
// contract of its own, and with one more transaction at its end, from a sender of its own, that creates a
// contract which destroys itself in its constructor. The contracts read only the first calldata byte.
func blockSTMBlock(t *testing.T, seed uint64, senders int, percent int) (func() *MemoryState, BlockRequest) {
	chainID := big.NewInt(testChainID)
	counters := []common.Address{testAddress(0xc1), testAddress(0xc2), testAddress(0xc3)}
	brancher := testAddress(0xb1)
	balanceReader := testAddress(0xb2)
	coinbaseReader := testAddress(0xb3)
	hotRecipients := []common.Address{testAddress(0xe1), testAddress(0xe2)}
	rng := rand.New(rand.NewPCG(seed, 7))
	var funded []common.Address
	var rawTxs [][]byte
	sign := func(key *ecdsa.PrivateKey, nonce uint64, to common.Address, value int64, first byte) {
		k := len(rawTxs)
		data := []byte{first, byte(k), byte(k >> 8), 0x5e}
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, nonce, &to, big.NewInt(value), data, 200_000, big.NewInt(1)))
	}
	for i := range senders {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		funded = append(funded, crypto.PubkeyToAddress(key.PublicKey))
		runs := 1
		if rng.IntN(10) == 0 {
			runs = 2 + rng.IntN(4)
		}
		for nonce := range runs {
			var to common.Address
			var first byte
			value := 1 + rng.Int64N(5)
			switch r := rng.IntN(65); {
			case rng.IntN(100) >= percent:
				to = common.BigToAddress(big.NewInt(int64(70_000 + i)))
			case r < 25:
				to = counters[rng.IntN(len(counters))]
			case r < 40:
				to, value, first = brancher, 0, byte(rng.IntN(3))
			case r < 45:
				to = balanceReader
			case r < 50:
				to = coinbaseReader
			default:
				to = hotRecipients[rng.IntN(len(hotRecipients))]
			}
			sign(key, uint64(nonce), to, value, first) //nolint:gosec // nonce is non-negative.
		}
	}
	creatorKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	creator := crypto.PubkeyToAddress(creatorKey.PublicKey)
	// CALLER SELFDESTRUCT
	rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, creatorKey, chainID, 0, nil, big.NewInt(0), []byte{0x33, 0xff}, 200_000, big.NewInt(1)))
	newState := func() *MemoryState {
		state := NewMemoryState()
		for i, counter := range counters {
			state.SetCode(counter, counterCode(testHash(byte(0x10+i))))
		}
		state.SetCode(brancher, branchingCounterCode)
		state.SetCode(balanceReader, balanceStoreCode(hotRecipients[0], testHash(0x20)))
		state.SetCode(coinbaseReader, balanceStoreCode(blockContext(chainID).Coinbase, testHash(0x21)))
		for _, sender := range append(funded, creator) {
			state.SetBalance(sender, big.NewInt(1_000_000_000))
		}
		return state
	}
	return newState, BlockRequest{Context: blockContext(chainID), Txs: rawTxs}
}

// observeBlockSTM records what every Block-STM phase did until the test ends, and returns what it
// recorded so far.
func observeBlockSTM(t *testing.T) func() []bstmCounts {
	var mu sync.Mutex
	var phases []bstmCounts
	bstmObserve = func(counts bstmCounts) {
		mu.Lock()
		phases = append(phases, counts)
		mu.Unlock()
	}
	t.Cleanup(func() { bstmObserve = nil })
	return func() []bstmCounts {
		mu.Lock()
		defer mu.Unlock()
		return append([]bstmCounts(nil), phases...)
	}
}

// rowReadingTestStore is readOnlyTestStore whose views read an account's fields in one row with its
// code hash, as FlatKV's views do, so the executor's reads and OCC's reruns take the row path.
type rowReadingTestStore struct {
	*readOnlyTestStore
}

func (s rowReadingTestStore) OpenView() gigatypes.StateView {
	return rowReadingView{s.readOnlyTestStore.OpenView().(*memoryStoreSnapshot)}
}

func (s rowReadingTestStore) OpenViewAt(blockNum int64) (gigatypes.StateView, bool) {
	view, ok := s.readOnlyTestStore.OpenViewAt(blockNum)
	if !ok {
		return nil, false
	}
	return rowReadingView{view.(*memoryStoreSnapshot)}, true
}

type rowReadingView struct {
	*memoryStoreSnapshot
}

func (v rowReadingView) ReadAccount(addr gigatypes.Address) (gigatypes.AccountSnapshot, bool) {
	if !v.AccountExists(addr) {
		return gigatypes.AccountSnapshot{}, false
	}
	return gigatypes.AccountSnapshot{Balance: v.GetBalance(addr), Nonce: v.GetNonce(addr), CodeHash: v.GetCodeHash(addr)}, true
}

func withRowReadingTestState(state StateReader) Option {
	store := rowReadingTestStore{&readOnlyTestStore{MemoryStore: NewMemoryStore(state)}}
	return withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet)
}

// counterCode adds one to the value at key.
func counterCode(key common.Hash) []byte {
	code := append([]byte{0x7f}, key.Bytes()...)
	code = append(code, 0x54, 0x60, 0x01, 0x01, 0x7f)
	code = append(code, key.Bytes()...)
	return append(code, 0x55, 0x00)
}

// branchingCounterCode is one contract with three modes, chosen by the first calldata byte. Mode 0
// stores one at slot 2 if the counter at slot 1 is odd and at slot 3 otherwise, then increments slot 1;
// modes 1 and 2 increment slots 2 and 3. Which slot a mode-0 call writes depends on how many mode-0
// calls came before it, so a rerun can write a slot its speculation did not.
var branchingCounterCode = common.FromHex("60003560f81c8060a757507f00000000000000000000000000000000000000000000000000000000000000015480600116605b5760017f0000000000000000000000000000000000000000000000000000000000000003556080565b60017f0000000000000000000000000000000000000000000000000000000000000002555b6001017f000000000000000000000000000000000000000000000000000000000000000155005b60021460f6577f0000000000000000000000000000000000000000000000000000000000000002546001017f000000000000000000000000000000000000000000000000000000000000000255005b7f0000000000000000000000000000000000000000000000000000000000000003546001017f00000000000000000000000000000000000000000000000000000000000000035500")
