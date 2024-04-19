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
	mtx sync.RWMutex
	txs []*WrappedTx // priority heap
	// invariant 1: no duplicate nonce in the same queue
	// invariant 2: no nonce gap in the same queue
	// invariant 3: head of the queue must be in heap
	evmQueue map[string][]*WrappedTx // sorted by nonce
}

func insertToEVMQueue(queue []*WrappedTx, tx *WrappedTx, i int) []*WrappedTx {
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
		if queue[mid].IsBefore(tx) {
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

func (pq *TxPriorityQueue) GetTxWithSameNonce(tx *WrappedTx) (*WrappedTx, int) {
	pq.mtx.RLock()
	defer pq.mtx.RUnlock()
	return pq.getTxWithSameNonceUnsafe(tx)
}

func (pq *TxPriorityQueue) getTxWithSameNonceUnsafe(tx *WrappedTx) (*WrappedTx, int) {
	queue, ok := pq.evmQueue[tx.evmAddress]
	if !ok {
		return nil, -1
	}
	idx := binarySearch(queue, tx)
	if idx < len(queue) && queue[idx].evmNonce == tx.evmNonce {
		return queue[idx], idx
	}
	return nil, -1
}

func (pq *TxPriorityQueue) tryReplacementUnsafe(tx *WrappedTx) (replaced *WrappedTx, shouldDrop bool) {
	if !tx.isEVM {
		return nil, false
	}
	queue, ok := pq.evmQueue[tx.evmAddress]
	if ok && len(queue) > 0 {
		existing, idx := pq.getTxWithSameNonceUnsafe(tx)
		if existing != nil {
			if tx.priority > existing.priority {
				// should replace
				// replace heap if applicable
				if hi, ok := pq.findTxIndexUnsafe(existing); ok {
					heap.Remove(pq, hi)
					heap.Push(pq, tx) // need to be in the heap since it has the same nonce
				}
				pq.evmQueue[tx.evmAddress][idx] = tx // replace queue item in-place
				return existing, false
			}
			// tx should be dropped since it's dominated by an existing tx
			return nil, true
		}
	}
	return nil, false
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

	txs := []*WrappedTx{}
	txs = append(txs, pq.txs...)
	for _, queue := range pq.evmQueue {
		txs = append(txs, queue[1:]...)
	}

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

func (pq *TxPriorityQueue) removeQueuedEvmTxUnsafe(tx *WrappedTx) (removedIdx int) {
	if queue, ok := pq.evmQueue[tx.evmAddress]; ok {
		for i, t := range queue {
			if t.tx.Key() == tx.tx.Key() {
				pq.evmQueue[tx.evmAddress] = append(queue[:i], queue[i+1:]...)
				if len(pq.evmQueue[tx.evmAddress]) == 0 {
					delete(pq.evmQueue, tx.evmAddress)
				}
				return i
			}
		}
	}
	return -1
}

func (pq *TxPriorityQueue) findTxIndexUnsafe(tx *WrappedTx) (int, bool) {
	// safety check for race situation where heapIndex is out of range of txs
	if tx.heapIndex >= 0 && tx.heapIndex < len(pq.txs) && pq.txs[tx.heapIndex].tx.Key() == tx.tx.Key() {
		return tx.heapIndex, true
	}

	// heap index isn't trustable here, so attempt to find it
	for i, t := range pq.txs {
		if t.tx.Key() == tx.tx.Key() {
			return i, true
		}
	}
	return 0, false
}

// RemoveTx removes a specific transaction from the priority queue.
func (pq *TxPriorityQueue) RemoveTx(tx *WrappedTx, shouldReenqueue bool) (toBeReenqueued []*WrappedTx) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	var removedIdx int

	if idx, ok := pq.findTxIndexUnsafe(tx); ok {
		heap.Remove(pq, idx)
		if tx.isEVM {
			removedIdx = pq.removeQueuedEvmTxUnsafe(tx)
			if !shouldReenqueue && len(pq.evmQueue[tx.evmAddress]) > 0 {
				heap.Push(pq, pq.evmQueue[tx.evmAddress][0])
			}
		}
	} else if tx.isEVM {
		removedIdx = pq.removeQueuedEvmTxUnsafe(tx)
	}
	if tx.isEVM && shouldReenqueue && len(pq.evmQueue[tx.evmAddress]) > 0 && removedIdx >= 0 {
		toBeReenqueued = pq.evmQueue[tx.evmAddress][removedIdx:]
	}
	return
}

func (pq *TxPriorityQueue) pushTxUnsafe(tx *WrappedTx) {
	if !tx.isEVM {
		heap.Push(pq, tx)
		return
	}

	// if there aren't other waiting txs, init and return
	queue, exists := pq.evmQueue[tx.evmAddress]
	if !exists {
		pq.evmQueue[tx.evmAddress] = []*WrappedTx{tx}
		heap.Push(pq, tx)
		return
	}

	// this item is on the heap at the moment
	first := queue[0]

	// the queue's first item (and ONLY the first item) must be on the heap
	// if this tx is before the first item, then we need to remove the first
	// item from the heap
	if tx.IsBefore(first) {
		if idx, ok := pq.findTxIndexUnsafe(first); ok {
			heap.Remove(pq, idx)
		}
		heap.Push(pq, tx)
	}
	pq.evmQueue[tx.evmAddress] = insertToEVMQueue(queue, tx, binarySearch(queue, tx))
}

// These are available if we need to test the invariant checks
// these can be used to troubleshoot invariant violations
//func (pq *TxPriorityQueue) checkInvariants(msg string) {
//	uniqHashes := make(map[string]bool)
//	for idx, tx := range pq.txs {
//		if tx == nil {
//			pq.print()
//			panic(fmt.Sprintf("DEBUG PRINT: found nil item on heap: idx=%d\n", idx))
//		}
//		if tx.tx == nil {
//			pq.print()
//			panic(fmt.Sprintf("DEBUG PRINT: found nil tx.tx on heap: idx=%d\n", idx))
//		}
//		if _, ok := uniqHashes[fmt.Sprintf("%x", tx.tx.Key())]; ok {
//			pq.print()
//			panic(fmt.Sprintf("INVARIANT (%s): duplicate hash=%x in heap", msg, tx.tx.Key()))
//		}
//		uniqHashes[fmt.Sprintf("%x", tx.tx.Key())] = true
//
//		//if _, ok := pq.keys[tx.tx.Key()]; !ok {
//		//	pq.print()
//		//	panic(fmt.Sprintf("INVARIANT (%s): tx in heap but not in keys hash=%x", msg, tx.tx.Key()))
//		//}
//
//		if tx.isEVM {
//			if queue, ok := pq.evmQueue[tx.evmAddress]; ok {
//				if queue[0].tx.Key() != tx.tx.Key() {
//					pq.print()
//					panic(fmt.Sprintf("INVARIANT (%s): tx in heap but not at front of evmQueue hash=%x", msg, tx.tx.Key()))
//				}
//			} else {
//				pq.print()
//				panic(fmt.Sprintf("INVARIANT (%s): tx in heap but not in evmQueue hash=%x", msg, tx.tx.Key()))
//			}
//		}
//	}
//
//	// each item in all queues should be unique nonce
//	for _, queue := range pq.evmQueue {
//		hashes := make(map[string]bool)
//		for idx, tx := range queue {
//			if idx == 0 {
//				_, ok := pq.findTxIndexUnsafe(tx)
//				if !ok {
//					pq.print()
//					panic(fmt.Sprintf("INVARIANT (%s): did not find tx[0] hash=%x nonce=%d in heap", msg, tx.tx.Key(), tx.evmNonce))
//				}
//			}
//			//if _, ok := pq.keys[tx.tx.Key()]; !ok {
//			//	pq.print()
//			//	panic(fmt.Sprintf("INVARIANT (%s): tx in heap but not in keys hash=%x", msg, tx.tx.Key()))
//			//}
//			if _, ok := hashes[fmt.Sprintf("%x", tx.tx.Key())]; ok {
//				pq.print()
//				panic(fmt.Sprintf("INVARIANT (%s): duplicate hash=%x in queue nonce=%d", msg, tx.tx.Key(), tx.evmNonce))
//			}
//			hashes[fmt.Sprintf("%x", tx.tx.Key())] = true
//		}
//	}
//}

// for debugging situations where invariant violations occur
//func (pq *TxPriorityQueue) print() {
//	fmt.Println("PRINT PRIORITY QUEUE ****************** ")
//	for _, tx := range pq.txs {
//		if tx == nil {
//			fmt.Printf("DEBUG PRINT: heap (nil): nonce=?, hash=?\n")
//			continue
//		}
//		if tx.tx == nil {
//			fmt.Printf("DEBUG PRINT: heap (%s): nonce=%d, tx.tx is nil \n", tx.evmAddress, tx.evmNonce)
//			continue
//		}
//		fmt.Printf("DEBUG PRINT: heap (%s): nonce=%d, hash=%x, time=%d\n", tx.evmAddress, tx.evmNonce, tx.tx.Key(), tx.timestamp.UnixNano())
//	}
//
//	for addr, queue := range pq.evmQueue {
//		for idx, tx := range queue {
//			if tx == nil {
//				fmt.Printf("DEBUG PRINT: found nil item on evmQueue(%s): idx=%d\n", addr, idx)
//				continue
//			}
//			if tx.tx == nil {
//				fmt.Printf("DEBUG PRINT: found nil tx.tx on  evmQueue(%s): idx=%d\n", addr, idx)
//				continue
//			}
//
//			fmt.Printf("DEBUG PRINT: evmQueue(%s)[%d]: nonce=%d, hash=%x, time=%d\n", tx.evmAddress, idx, tx.evmNonce, tx.tx.Key(), tx.timestamp.UnixNano())
//		}
//	}
//}

// PushTx adds a valid transaction to the priority queue. It is thread safe.
func (pq *TxPriorityQueue) PushTx(tx *WrappedTx) (*WrappedTx, bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	replacedTx, shouldDrop := pq.tryReplacementUnsafe(tx)

	// tx was not inserted, and nothing was replaced
	if shouldDrop {
		return nil, false
	}

	// tx replaced an existing transaction
	if replacedTx != nil {
		return replacedTx, true
	}

	// tx was not inserted yet, so insert it
	pq.pushTxUnsafe(tx)
	return nil, true
}

func (pq *TxPriorityQueue) popTxUnsafe() *WrappedTx {
	if len(pq.txs) == 0 {
		return nil
	}

	// remove the first item from the heap
	x := heap.Pop(pq)
	if x == nil {
		return nil
	}
	tx := x.(*WrappedTx)

	// this situation is primarily for a test case that inserts nils
	if tx == nil {
		return nil
	}

	// non-evm transactions do not have txs waiting on a nonce
	if !tx.isEVM {
		return tx
	}

	// evm transactions can have txs waiting on this nonce
	// if there are any, we should replace the heap with the next nonce
	// for the address

	// remove the first item from the evmQueue
	pq.removeQueuedEvmTxUnsafe(tx)

	// if there is a next item, now it can be added to the heap
	if len(pq.evmQueue[tx.evmAddress]) > 0 {
		heap.Push(pq, pq.evmQueue[tx.evmAddress][0])
	}

	return tx
}

// PopTx removes the top priority transaction from the queue. It is thread safe.
func (pq *TxPriorityQueue) PopTx() *WrappedTx {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	return pq.popTxUnsafe()
}

// dequeue up to `max` transactions and reenqueue while locked
func (pq *TxPriorityQueue) ForEachTx(handler func(wtx *WrappedTx) bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	numTxs := len(pq.txs) + pq.numQueuedUnsafe()

	txs := make([]*WrappedTx, 0, numTxs)

	defer func() {
		for _, tx := range txs {
			pq.pushTxUnsafe(tx)
		}
	}()

	for i := 0; i < numTxs; i++ {
		popped := pq.popTxUnsafe()
		if popped == nil {
			break
		}
		txs = append(txs, popped)
		if !handler(popped) {
			return
		}
	}
}

// dequeue up to `max` transactions and reenqueue while locked
// TODO: use ForEachTx instead
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
	old[n-1] = nil         // avoid memory leak
	setHeapIndex(item, -1) // for safety
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
	setHeapIndex(pq.txs[i], i)
	setHeapIndex(pq.txs[j], j)
}

func setHeapIndex(tx *WrappedTx, i int) {
	// a removed tx can be nil
	if tx == nil {
		return
	}
	tx.heapIndex = i
}
