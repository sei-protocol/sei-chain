package mempool

import (
	"cmp"
	"container/heap"
	"slices"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	tmmath "github.com/sei-protocol/sei-chain/sei-tendermint/libs/math"
)

var _ heap.Interface = (*TxPriorityQueue)(nil)

// TxPriorityQueue defines a thread-safe priority queue for valid transactions.
type TxPriorityQueue struct {
	mtx sync.RWMutex
	txs []*WrappedTx // priority heap
	// invariant 1: no duplicate nonce in the same queue
	// invariant 2: no nonce gap in the same queue
	// invariant 3: head of the queue must be in heap
	evmQueue map[common.Address][]*WrappedTx // indexed by sender address, sorted by nonce
}

func insertToEVMQueue(queue []*WrappedTx, tx *WrappedTx, i int) []*WrappedTx {
	// Make room for new value and add it
	queue = append(queue, nil)
	copy(queue[i+1:], queue[i:])
	queue[i] = tx
	return queue
}

// binarySearch finds the index at which nonce should be inserted in queue and
// whether an exact nonce match already exists.
func binarySearch(queue []*WrappedTx, nonce uint64) (int, bool) {
	return slices.BinarySearchFunc(queue, nonce, func(tx *WrappedTx, target uint64) int {
		return cmp.Compare(tx.EVMNonce(), target)
	})
}

func NewTxPriorityQueue() *TxPriorityQueue {
	pq := &TxPriorityQueue{
		txs:      nil,
		evmQueue: map[common.Address][]*WrappedTx{},
	}
	heap.Init(pq)
	return pq
}

func (pq *TxPriorityQueue) TxByAddrNonce(addr common.Address, nonce uint64) (*WrappedTx, int) {
	pq.mtx.RLock()
	defer pq.mtx.RUnlock()
	return pq.txByAddrNonceUnsafe(addr, nonce)
}

func (pq *TxPriorityQueue) txByAddrNonceUnsafe(addr common.Address, nonce uint64) (*WrappedTx, int) {
	queue := pq.evmQueue[addr]
	if idx, found := binarySearch(queue, nonce); found {
		return queue[idx], idx
	}
	return nil, -1
}

func (pq *TxPriorityQueue) tryReplacementUnsafe(tx *WrappedTx) (replaced *WrappedTx, shouldDrop bool) {
	evm, ok := tx.evm.Get()
	if !ok {
		return nil, false
	}
	queue := pq.evmQueue[evm.address]
	if len(queue) == 0 {
		return nil, false
	}
	existing, idx := pq.txByAddrNonceUnsafe(evm.address, evm.nonce)
	if existing == nil {
		return nil, false
	}
	if tx.priority <= existing.priority {
		// tx should be dropped since it's dominated by an existing tx
		return nil, true
	}
	// should replace
	// replace heap if applicable
	if hi, ok := pq.findTxIndexUnsafe(existing); ok {
		heap.Remove(pq, hi)
		heap.Push(pq, tx) // need to be in the heap since it has the same nonce
	}
	pq.evmQueue[evm.address][idx] = tx // replace queue item in-place
	return existing, false
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
	txs := append([]*WrappedTx{}, pq.txs...)
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
	evm, ok := tx.evm.Get()
	if !ok {
		return -1
	}
	if queue, ok := pq.evmQueue[evm.address]; ok {
		for i, t := range queue {
			if t.Hash() == tx.Hash() {
				pq.evmQueue[evm.address] = append(queue[:i], queue[i+1:]...)
				if len(pq.evmQueue[evm.address]) == 0 {
					delete(pq.evmQueue, evm.address)
				}
				return i
			}
		}
	}
	return -1
}

func (pq *TxPriorityQueue) findTxIndexUnsafe(tx *WrappedTx) (int, bool) {
	// safety check for race situation where heapIndex is out of range of txs
	if tx.heapIndex >= 0 && tx.heapIndex < len(pq.txs) && pq.txs[tx.heapIndex].Hash() == tx.Hash() {
		return tx.heapIndex, true
	}

	// heap index isn't trustable here, so attempt to find it
	for i, t := range pq.txs {
		if t.Hash() == tx.Hash() {
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
		if evm, ok := tx.evm.Get(); ok {
			removedIdx = pq.removeQueuedEvmTxUnsafe(tx)
			if !shouldReenqueue && len(pq.evmQueue[evm.address]) > 0 {
				heap.Push(pq, pq.evmQueue[evm.address][0])
			}
		}
	} else if tx.evm.IsPresent() {
		removedIdx = pq.removeQueuedEvmTxUnsafe(tx)
	}
	if evm, ok := tx.evm.Get(); ok && shouldReenqueue && len(pq.evmQueue[evm.address]) > 0 && removedIdx >= 0 {
		toBeReenqueued = pq.evmQueue[evm.address][removedIdx:]
	}
	return
}

func (pq *TxPriorityQueue) pushTxUnsafe(tx *WrappedTx) {
	evm, ok := tx.evm.Get()
	if !ok {
		heap.Push(pq, tx)
		return
	}

	// if there aren't other waiting txs, init and return
	queue, exists := pq.evmQueue[evm.address]
	if !exists {
		pq.evmQueue[evm.address] = []*WrappedTx{tx}
		heap.Push(pq, tx)
		return
	}

	// this item is on the heap at the moment
	first := queue[0]

	// the queue's first item (and ONLY the first item) must be on the heap
	// if this tx is before the first item, then we need to remove the first
	// item from the heap
	if evm.nonce < first.EVMNonce() {
		if idx, ok := pq.findTxIndexUnsafe(first); ok {
			heap.Remove(pq, idx)
		}
		heap.Push(pq, tx)
	}
	idx, _ := binarySearch(queue, evm.nonce)
	pq.evmQueue[evm.address] = insertToEVMQueue(queue, tx, idx)
}

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
	evm, ok := tx.evm.Get()
	if !ok {
		return tx
	}

	// evm transactions can have txs waiting on this nonce
	// if there are any, we should replace the heap with the next nonce
	// for the address

	// remove the first item from the evmQueue
	pq.removeQueuedEvmTxUnsafe(tx)

	// if there is a next item, now it can be added to the heap
	if len(pq.evmQueue[evm.address]) > 0 {
		heap.Push(pq, pq.evmQueue[evm.address][0])
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

	for range numTxs {
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
