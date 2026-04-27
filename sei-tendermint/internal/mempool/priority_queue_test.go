package mempool

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/stretchr/testify/require"
)

// TxTestCase represents a single test case for the TxPriorityQueue
type TxTestCase struct {
	name           string
	inputTxs       []*WrappedTx // Input transactions
	expectedOutput []int64      // Expected order of transaction IDs
}

func TestTxPriorityQueue_ReapHalf(t *testing.T) {
	pq := NewTxPriorityQueue()

	// Generate transactions with different priorities and nonces
	txs := make([]*WrappedTx, 100)
	for i := range txs {
		txs[i] = &WrappedTx{
			hashedTx: newHashedTx(types.Tx(fmt.Sprintf("tx-%d", i))),
			priority: int64(i),
		}

		// Push the transaction
		pq.PushTx(txs[i])
	}

	//reverse sort txs by priority
	sort.Slice(txs, func(i, j int) bool {
		return txs[i].priority > txs[j].priority
	})

	// Reap half of the transactions
	reapedTxs := pq.PeekTxs(len(txs) / 2)

	// Check if the reaped transactions are in the correct order of their priorities and nonces
	for i, reapedTx := range reapedTxs {
		require.Equal(t, txs[i].priority, reapedTx.priority)
	}
}

func TestAvoidPanicIfTransactionIsNil(t *testing.T) {
	pq := NewTxPriorityQueue()
	pq.Push(&WrappedTx{hashedTx: newHashedTx(nil), sender: "1", isEVM: true, evmAddress: "0xabc", evmNonce: 1, priority: 10})
	pq.txs = append(pq.txs, nil)

	var count int
	pq.ForEachTx(func(tx *WrappedTx) bool {
		count++
		return true
	})

	require.Equal(t, 1, count)
}

func TestTxPriorityQueue_PriorityAndNonceOrdering(t *testing.T) {
	testCases := []TxTestCase{
		{
			name: "PriorityWithEVMAndNonEVMDuplicateNonce",
			inputTxs: []*WrappedTx{
				{hashedTx: newHashedTx(nil), sender: "1", isEVM: true, evmAddress: "0xabc", evmNonce: 1, priority: 10},
				{hashedTx: newHashedTx(nil), sender: "3", isEVM: true, evmAddress: "0xabc", evmNonce: 3, priority: 9},
				{hashedTx: newHashedTx(nil), sender: "2", isEVM: true, evmAddress: "0xabc", evmNonce: 1, priority: 7},
			},
			expectedOutput: []int64{1, 3},
		},
		{
			name: "PriorityWithEVMAndNonEVMDuplicateNonce",
			inputTxs: []*WrappedTx{
				{hashedTx: newHashedTx(nil), sender: "1", isEVM: true, evmAddress: "0xabc", evmNonce: 1, priority: 10},
				{hashedTx: newHashedTx(nil), sender: "2", isEVM: false, priority: 9},
				{hashedTx: newHashedTx(nil), sender: "4", isEVM: true, evmAddress: "0xabc", evmNonce: 0, priority: 9}, // Same EVM address as first, lower nonce
				{hashedTx: newHashedTx(nil), sender: "5", isEVM: true, evmAddress: "0xdef", evmNonce: 1, priority: 7},
				{hashedTx: newHashedTx(nil), sender: "3", isEVM: true, evmAddress: "0xdef", evmNonce: 0, priority: 8},
				{hashedTx: newHashedTx(nil), sender: "6", isEVM: false, priority: 6},
				{hashedTx: newHashedTx(nil), sender: "7", isEVM: true, evmAddress: "0xghi", evmNonce: 2, priority: 5},
			},
			expectedOutput: []int64{2, 4, 1, 3, 5, 6, 7},
		},
		{
			name: "PriorityWithEVMAndNonEVM",
			inputTxs: []*WrappedTx{
				{hashedTx: newHashedTx(nil), sender: "1", isEVM: true, evmAddress: "0xabc", evmNonce: 1, priority: 10},
				{hashedTx: newHashedTx(nil), sender: "2", isEVM: false, priority: 9},
				{hashedTx: newHashedTx(nil), sender: "4", isEVM: true, evmAddress: "0xabc", evmNonce: 0, priority: 9}, // Same EVM address as first, lower nonce
				{hashedTx: newHashedTx(nil), sender: "5", isEVM: true, evmAddress: "0xdef", evmNonce: 1, priority: 7},
				{hashedTx: newHashedTx(nil), sender: "3", isEVM: true, evmAddress: "0xdef", evmNonce: 0, priority: 8},
				{hashedTx: newHashedTx(nil), sender: "6", isEVM: false, priority: 6},
				{hashedTx: newHashedTx(nil), sender: "7", isEVM: true, evmAddress: "0xghi", evmNonce: 2, priority: 5},
			},
			expectedOutput: []int64{2, 4, 1, 3, 5, 6, 7},
		},
		{
			name: "IdenticalPrioritiesAndNoncesDifferentAddresses",
			inputTxs: []*WrappedTx{
				{hashedTx: newHashedTx(nil), sender: "1", isEVM: true, evmAddress: "0xabc", evmNonce: 2, priority: 5},
				{hashedTx: newHashedTx(nil), sender: "2", isEVM: true, evmAddress: "0xdef", evmNonce: 2, priority: 5},
				{hashedTx: newHashedTx(nil), sender: "3", isEVM: true, evmAddress: "0xghi", evmNonce: 2, priority: 5},
			},
			expectedOutput: []int64{1, 2, 3},
		},
		{
			name: "InterleavedEVAndNonEVMTransactions",
			inputTxs: []*WrappedTx{
				{hashedTx: newHashedTx(nil), sender: "7", isEVM: false, priority: 15},
				{hashedTx: newHashedTx(nil), sender: "8", isEVM: true, evmAddress: "0xabc", evmNonce: 1, priority: 20},
				{hashedTx: newHashedTx(nil), sender: "9", isEVM: false, priority: 10},
				{hashedTx: newHashedTx(nil), sender: "10", isEVM: true, evmAddress: "0xdef", evmNonce: 2, priority: 20},
			},
			expectedOutput: []int64{8, 10, 7, 9},
		},
		{
			name: "SameAddressPriorityDifferentNonces",
			inputTxs: []*WrappedTx{
				{hashedTx: newHashedTx(nil), sender: "11", isEVM: true, evmAddress: "0xabc", evmNonce: 3, priority: 10},
				{hashedTx: newHashedTx(nil), sender: "12", isEVM: true, evmAddress: "0xabc", evmNonce: 1, priority: 10},
				{hashedTx: newHashedTx(nil), sender: "13", isEVM: true, evmAddress: "0xabc", evmNonce: 2, priority: 10},
			},
			expectedOutput: []int64{12, 13, 11},
		},
		{
			name: "OneItem",
			inputTxs: []*WrappedTx{
				{hashedTx: newHashedTx(nil), sender: "14", isEVM: true, evmAddress: "0xabc", evmNonce: 1, priority: 10},
			},
			expectedOutput: []int64{14},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pq := NewTxPriorityQueue()
			now := time.Now()

			// Add input transactions to the queue and set timestamp to order inserted
			for i, tx := range tc.inputTxs {
				tx.timestamp = now.Add(time.Duration(i) * time.Second)
				tx.hashedTx = newHashedTx(types.Tx(fmt.Sprintf("%d", time.Now().UnixNano())))
				pq.PushTx(tx)
			}

			results := pq.PeekTxs(len(tc.inputTxs))
			// Validate the order of transactions
			require.Len(t, results, len(tc.expectedOutput))
			for i, expectedTxID := range tc.expectedOutput {
				tx := results[i]
				require.Equal(t, fmt.Sprintf("%d", expectedTxID), tx.sender)
			}
		})
	}
}

func TestTxPriorityQueue_SameAddressDifferentNonces(t *testing.T) {
	pq := NewTxPriorityQueue()
	address := "0x123"

	// Insert transactions with the same address but different nonces and priorities
	pq.PushTx(&WrappedTx{hashedTx: newHashedTx(types.Tx("tx1")), isEVM: true, evmAddress: address, evmNonce: 2, priority: 10})
	pq.PushTx(&WrappedTx{hashedTx: newHashedTx(types.Tx("tx2")), isEVM: true, evmAddress: address, evmNonce: 1, priority: 5})
	pq.PushTx(&WrappedTx{hashedTx: newHashedTx(types.Tx("tx3")), isEVM: true, evmAddress: address, evmNonce: 3, priority: 15})

	// Pop transactions and verify they are in the correct order of nonce
	tx1 := pq.PopTx()
	require.Equal(t, uint64(1), tx1.evmNonce)
	tx2 := pq.PopTx()
	require.Equal(t, uint64(2), tx2.evmNonce)
	tx3 := pq.PopTx()
	require.Equal(t, uint64(3), tx3.evmNonce)
}

func TestTxPriorityQueue(t *testing.T) {
	pq := NewTxPriorityQueue()
	numTxs := 1000

	priorities := make([]int, numTxs)

	var wg sync.WaitGroup
	for i := 1; i <= numTxs; i++ {
		priorities[i-1] = i
		wg.Add(1)

		go func(i int) {
			pq.PushTx(&WrappedTx{
				hashedTx:  newHashedTx(types.Tx(fmt.Sprintf("%d", i))),
				priority:  int64(i),
				timestamp: time.Now(),
			})

			wg.Done()
		}(i)
	}

	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))

	wg.Wait()
	require.Equal(t, numTxs, pq.NumTxs())

	// Wait a second and push a tx with a duplicate priority
	time.Sleep(time.Second)
	now := time.Now()
	pq.PushTx(&WrappedTx{
		hashedTx:  newHashedTx(types.Tx(fmt.Sprintf("%d", time.Now().UnixNano()))),
		priority:  1000,
		timestamp: now,
	})
	require.Equal(t, 1001, pq.NumTxs())

	tx := pq.PopTx()
	require.Equal(t, 1000, pq.NumTxs())
	require.Equal(t, int64(1000), tx.priority)
	require.NotEqual(t, now, tx.timestamp)

	gotPriorities := make([]int, 0)
	for pq.NumTxs() > 0 {
		gotPriorities = append(gotPriorities, int(pq.PopTx().priority))
	}

	require.Equal(t, priorities, gotPriorities)
}

func TestTxPriorityQueue_GetEvictableTxs(t *testing.T) {
	pq := NewTxPriorityQueue()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	values := make([]int, 1000)

	for i := 0; i < 1000; i++ {
		tx := make([]byte, 5) // each tx is 5 bytes
		_, err := rng.Read(tx)
		require.NoError(t, err)

		x := rng.Intn(100000)
		pq.PushTx(&WrappedTx{
			hashedTx: newHashedTx(tx),
			priority: int64(x),
		})

		values[i] = x
	}

	sort.Ints(values)

	max := values[len(values)-1]
	min := values[0]
	totalSize := int64(len(values) * 5)

	testCases := []struct {
		name                             string
		priority, txSize, totalSize, cap int64
		expectedLen                      int
	}{
		{
			name:        "larest priority; single tx",
			priority:    int64(max + 1),
			txSize:      5,
			totalSize:   totalSize,
			cap:         totalSize,
			expectedLen: 1,
		},
		{
			name:        "larest priority; multi tx",
			priority:    int64(max + 1),
			txSize:      17,
			totalSize:   totalSize,
			cap:         totalSize,
			expectedLen: 4,
		},
		{
			name:        "larest priority; out of capacity",
			priority:    int64(max + 1),
			txSize:      totalSize + 1,
			totalSize:   totalSize,
			cap:         totalSize,
			expectedLen: 0,
		},
		{
			name:        "smallest priority; no tx",
			priority:    int64(min - 1),
			txSize:      5,
			totalSize:   totalSize,
			cap:         totalSize,
			expectedLen: 0,
		},
		{
			name:        "small priority; no tx",
			priority:    int64(min),
			txSize:      5,
			totalSize:   totalSize,
			cap:         totalSize,
			expectedLen: 0,
		},
	}

	for _, tc := range testCases {

		t.Run(tc.name, func(t *testing.T) {
			evictTxs := pq.GetEvictableTxs(tc.priority, tc.txSize, tc.totalSize, tc.cap)
			require.Len(t, evictTxs, tc.expectedLen)
		})
	}
}

func TestTxPriorityQueue_RemoveTxEvm(t *testing.T) {
	pq := NewTxPriorityQueue()

	tx1 := &WrappedTx{
		hashedTx:   newHashedTx(types.Tx("tx1")),
		priority:   1,
		isEVM:      true,
		evmAddress: "0xabc",
		evmNonce:   1,
	}
	tx2 := &WrappedTx{
		hashedTx:   newHashedTx(types.Tx("tx2")),
		priority:   1,
		isEVM:      true,
		evmAddress: "0xabc",
		evmNonce:   2,
	}

	pq.PushTx(tx1)
	pq.PushTx(tx2)

	pq.RemoveTx(tx1, false)

	result := pq.PopTx()
	require.Equal(t, tx2, result)
}

func TestTxPriorityQueue_RemoveTx(t *testing.T) {
	pq := NewTxPriorityQueue()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	numTxs := 1000

	values := make([]int, numTxs)

	for i := 0; i < numTxs; i++ {
		x := rng.Intn(100000)
		pq.PushTx(&WrappedTx{
			hashedTx: newHashedTx(types.Tx(fmt.Sprintf("%d", i))),
			priority: int64(x),
		})

		values[i] = x
	}

	require.Equal(t, numTxs, pq.NumTxs())

	sort.Ints(values)
	max := values[len(values)-1]

	wtx := pq.txs[pq.NumTxs()/2]
	pq.RemoveTx(wtx, false)
	require.Equal(t, numTxs-1, pq.NumTxs())
	require.Equal(t, int64(max), pq.PopTx().priority)
	require.Equal(t, numTxs-2, pq.NumTxs())

	require.NotPanics(t, func() {
		pq.RemoveTx(&WrappedTx{hashedTx: newHashedTx(nil), heapIndex: numTxs}, false)
		pq.RemoveTx(&WrappedTx{hashedTx: newHashedTx(nil), heapIndex: numTxs + 1}, false)
	})
	require.Equal(t, numTxs-2, pq.NumTxs())
}

func TestTxPriorityQueue_TryReplacement(t *testing.T) {
	for _, test := range []struct {
		tx               *WrappedTx
		existing         []*WrappedTx
		expectedReplaced bool
		expectedDropped  bool
		expectedQueue    []*WrappedTx
		expectedHeap     []*WrappedTx
	}{
		// non-evm transaction is inserted into empty queue
		{&WrappedTx{hashedTx: newHashedTx(nil), isEVM: false}, []*WrappedTx{}, false, false, []*WrappedTx{{hashedTx: newHashedTx(nil), isEVM: false}}, []*WrappedTx{{hashedTx: newHashedTx(nil), isEVM: false}}},
		// evm transaction is inserted into empty queue
		{&WrappedTx{hashedTx: newHashedTx(nil), isEVM: true, evmAddress: "addr1"}, []*WrappedTx{}, false, false, []*WrappedTx{{hashedTx: newHashedTx(nil), isEVM: true, evmAddress: "addr1"}}, []*WrappedTx{{hashedTx: newHashedTx(nil), isEVM: true, evmAddress: "addr1"}}},
		// evm transaction (new nonce) is inserted into queue with existing tx (lower nonce)
		{
			&WrappedTx{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 1, priority: 100}, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
			}, false, false, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
				{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 1, priority: 100},
			}, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
				{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 1, priority: 100},
			},
		},
		// evm transaction (new nonce) is not inserted because it's a duplicate nonce and same priority
		{
			&WrappedTx{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100}, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
			}, false, true, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
			}, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
			},
		},
		// evm transaction (new nonce) replaces the existing nonce transaction because its priority is higher
		{
			&WrappedTx{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 101}, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
			}, true, false, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 101},
			}, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 101},
			},
		},
		{
			&WrappedTx{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 1, priority: 100}, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
				{hashedTx: newHashedTx(types.Tx("ghi")), isEVM: true, evmAddress: "addr1", evmNonce: 1, priority: 99},
			}, true, false, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
				{hashedTx: newHashedTx(types.Tx("abc")), isEVM: true, evmAddress: "addr1", evmNonce: 1, priority: 100},
			}, []*WrappedTx{
				{hashedTx: newHashedTx(types.Tx("def")), isEVM: true, evmAddress: "addr1", evmNonce: 0, priority: 100},
			},
		},
	} {
		pq := NewTxPriorityQueue()
		for _, e := range test.existing {
			pq.PushTx(e)
		}
		replaced, inserted := pq.PushTx(test.tx)
		if test.expectedReplaced {
			require.NotNil(t, replaced)
		} else {
			require.Nil(t, replaced)
		}
		require.Equal(t, test.expectedDropped, !inserted)
		for i, q := range pq.evmQueue[test.tx.evmAddress] {
			require.Equal(t, test.expectedQueue[i].Key(), q.Key())
			require.Equal(t, test.expectedQueue[i].priority, q.priority)
			require.Equal(t, test.expectedQueue[i].evmNonce, q.evmNonce)
		}
		for i, q := range pq.txs {
			require.Equal(t, test.expectedHeap[i].Key(), q.Key())
			require.Equal(t, test.expectedHeap[i].priority, q.priority)
			require.Equal(t, test.expectedHeap[i].evmNonce, q.evmNonce)
		}
	}
}
