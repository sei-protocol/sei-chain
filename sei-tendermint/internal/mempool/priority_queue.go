package mempool

import (
	"container/heap"
	"sort"
	"sync"

	tmmath "github.com/tendermint/tendermint/libs/math"
)

var _ heap.Interface = (*TxPriorityQueue)(nil)

// TxPriorityQueue defines a thread-safe priority queue for valid transactions.
type TxPriorityQueue struct {
	mtx      sync.RWMutex
	txs      []*WrappedTx
	evmQueue map[string][]*WrappedTx
}

func insertToEVMQueue(queue []*WrappedTx, tx *WrappedTx) []*WrappedTx {
	// Using BinarySearch to find the appropriate index to insert tx
	i := binarySearch(queue, tx)

	// Make room for new value and add it
	queue = append(queue, nil)
	copy(queue[i+1:], queue[i:])
	queue[i] = tx
	return queue
}

// binarySearch finds the index at which tx should be inserted in queue
func binarySearch(queue []*WrappedTx, tx *WrappedTx) int {
	low, high := 0, len(queue)
	for low < high {
		mid := low + (high-low)/2
		if queue[mid].evmNonce < tx.evmNonce {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return low
}

func NewTxPriorityQueue() *TxPriorityQueue {
	pq := &TxPriorityQueue{
		txs:      make([]*WrappedTx, 0),
		evmQueue: make(map[string][]*WrappedTx),
	}

	heap.Init(pq)

	return pq
}

// GetEvictableTxs attempts to find and return a list of *WrappedTx than can be
// evicted to make room for another *WrappedTx with higher priority. If no such
// list of *WrappedTx exists, nil will be returned. The returned list of *WrappedTx
// indicate that these transactions can be removed due to them being of lower
// priority and that their total sum in size allows room for the incoming
// transaction according to the mempool's configured limits.
func (pq *TxPriorityQueue) GetEvictableTxs(priority, txSize, totalSize, cap int64) []*WrappedTx {
	pq.mtx.RLock()
	defer pq.mtx.RUnlock()

	txs := make([]*WrappedTx, len(pq.txs))
	copy(txs, pq.txs)

	sort.Slice(txs, func(i, j int) bool {
		return txs[i].priority < txs[j].priority
	})

	var (
		toEvict []*WrappedTx
		i       int
	)

	currSize := totalSize

	// Loop over all transactions in ascending priority order evaluating those
	// that are only of less priority than the provided argument. We continue
	// evaluating transactions until there is sufficient capacity for the new
	// transaction (size) as defined by txSize.
	for i < len(txs) && txs[i].priority < priority {
		toEvict = append(toEvict, txs[i])
		currSize -= int64(txs[i].Size())

		if currSize+txSize <= cap {
			return toEvict
		}

		i++
	}

	return nil
}

// requires read lock
func (pq *TxPriorityQueue) numQueuedUnsafe() int {
	var result int
	for _, queue := range pq.evmQueue {
		result += len(queue)
	}
	// first items in queue are also in heap, subtract one
	return result - len(pq.evmQueue)
}

// NumTxs returns the number of transactions in the priority queue. It is
// thread safe.
func (pq *TxPriorityQueue) NumTxs() int {
	pq.mtx.RLock()
	defer pq.mtx.RUnlock()

	return len(pq.txs) + pq.numQueuedUnsafe()
}

func (pq *TxPriorityQueue) removeQueuedEvmTxUnsafe(tx *WrappedTx) {
	if queue, ok := pq.evmQueue[tx.evmAddress]; ok {
		for i, t := range queue {
			if t.evmNonce == tx.evmNonce {
				pq.evmQueue[tx.evmAddress] = append(queue[:i], queue[i+1:]...)
				if len(pq.evmQueue[tx.evmAddress]) == 0 {
					delete(pq.evmQueue, tx.evmAddress)
				} else {
					heap.Push(pq, pq.evmQueue[tx.evmAddress][0])
				}
				break
			}
		}
	}
}

func (pq *TxPriorityQueue) findTxIndexUnsafe(tx *WrappedTx) (int, bool) {
	for i, t := range pq.txs {
		if t == tx {
			return i, true
		}
	}
	return 0, false
}

// RemoveTx removes a specific transaction from the priority queue.
func (pq *TxPriorityQueue) RemoveTx(tx *WrappedTx) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	if idx, ok := pq.findTxIndexUnsafe(tx); ok {
		heap.Remove(pq, idx)
	}

	if tx.isEVM {
		pq.removeQueuedEvmTxUnsafe(tx)
	}
}

func (pq *TxPriorityQueue) pushTxUnsafe(tx *WrappedTx) {
	if !tx.isEVM {
		heap.Push(pq, tx)
		return
	}

	queue, exists := pq.evmQueue[tx.evmAddress]
	if !exists {
		pq.evmQueue[tx.evmAddress] = []*WrappedTx{tx}
		heap.Push(pq, tx)
		return
	}

	first := queue[0]
	if tx.evmNonce < first.evmNonce {
		if idx, ok := pq.findTxIndexUnsafe(first); ok {
			heap.Remove(pq, idx)
		}
		heap.Push(pq, tx)
	}

	pq.evmQueue[tx.evmAddress] = insertToEVMQueue(queue, tx)
}

// PushTx adds a valid transaction to the priority queue. It is thread safe.
func (pq *TxPriorityQueue) PushTx(tx *WrappedTx) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.pushTxUnsafe(tx)
}

func (pq *TxPriorityQueue) popTxUnsafe() *WrappedTx {
	x := heap.Pop(pq)
	if x == nil {
		return nil
	}

	tx := x.(*WrappedTx)

	if !tx.isEVM {
		return tx
	}

	pq.removeQueuedEvmTxUnsafe(tx)

	return tx
}

// PopTx removes the top priority transaction from the queue. It is thread safe.
func (pq *TxPriorityQueue) PopTx() *WrappedTx {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.popTxUnsafe()
}

// dequeue up to `max` transactions and reenqueue while locked
func (pq *TxPriorityQueue) PeekTxs(max int) []*WrappedTx {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	numTxs := len(pq.txs) + pq.numQueuedUnsafe()
	if max < 0 {
		max = numTxs
	}

	cap := tmmath.MinInt(numTxs, max)
	res := make([]*WrappedTx, 0, cap)
	for i := 0; i < cap; i++ {
		popped := pq.popTxUnsafe()
		if popped == nil {
			break
		}

		res = append(res, popped)
	}

	for _, tx := range res {
		pq.pushTxUnsafe(tx)
	}
	return res
}

// Push implements the Heap interface.
//
// NOTE: A caller should never call Push. Use PushTx instead.
func (pq *TxPriorityQueue) Push(x interface{}) {
	n := len(pq.txs)
	item := x.(*WrappedTx)
	item.heapIndex = n
	pq.txs = append(pq.txs, item)
}

// Pop implements the Heap interface.
//
// NOTE: A caller should never call Pop. Use PopTx instead.
func (pq *TxPriorityQueue) Pop() interface{} {
	old := pq.txs
	n := len(old)
	item := old[n-1]
	old[n-1] = nil      // avoid memory leak
	item.heapIndex = -1 // for safety
	pq.txs = old[0 : n-1]
	return item
}

// Len implements the Heap interface.
//
// NOTE: A caller should never call Len. Use NumTxs instead.
func (pq *TxPriorityQueue) Len() int {
	return len(pq.txs)
}

// Less implements the Heap interface. It returns true if the transaction at
// position i in the queue is of less priority than the transaction at position j.
func (pq *TxPriorityQueue) Less(i, j int) bool {
	// If there exists two transactions with the same priority, consider the one
	// that we saw the earliest as the higher priority transaction.
	if pq.txs[i].priority == pq.txs[j].priority {
		return pq.txs[i].timestamp.Before(pq.txs[j].timestamp)
	}

	// We want Pop to give us the highest, not lowest, priority so we use greater
	// than here.
	return pq.txs[i].priority > pq.txs[j].priority
}

// Swap implements the Heap interface. It swaps two transactions in the queue.
func (pq *TxPriorityQueue) Swap(i, j int) {
	pq.txs[i], pq.txs[j] = pq.txs[j], pq.txs[i]
	pq.txs[i].heapIndex = i
	pq.txs[j].heapIndex = j
}
