package mempool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	abciclient "github.com/tendermint/tendermint/abci/client"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/libs/clist"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

var _ Mempool = (*TxMempool)(nil)

// TxMempoolOption sets an optional parameter on the TxMempool.
type TxMempoolOption func(*TxMempool)

// TxMempool defines a prioritized mempool data structure used by the v1 mempool
// reactor. It keeps a thread-safe priority queue of transactions that is used
// when a block proposer constructs a block and a thread-safe linked-list that
// is used to gossip transactions to peers in a FIFO manner.
type TxMempool struct {
	logger       log.Logger
	metrics      *Metrics
	config       *config.MempoolConfig
	proxyAppConn abciclient.Client

	// txsAvailable fires once for each height when the mempool is not empty
	txsAvailable         chan struct{}
	notifiedTxsAvailable bool

	// height defines the last block height process during Update()
	height int64

	// sizeBytes defines the total size of the mempool (sum of all tx bytes)
	sizeBytes int64

	// pendingSizeBytes defines the total size of the pending set (sum of all tx bytes)
	pendingSizeBytes int64

	// cache defines a fixed-size cache of already seen transactions as this
	// reduces pressure on the proxyApp.
	cache TxCache

	// txStore defines the main storage of valid transactions. Indexes are built
	// on top of this store.
	txStore *TxStore

	// gossipIndex defines the gossiping index of valid transactions via a
	// thread-safe linked-list. We also use the gossip index as a cursor for
	// rechecking transactions already in the mempool.
	gossipIndex *clist.CList

	// recheckCursor and recheckEnd are used as cursors based on the gossip index
	// to recheck transactions that are already in the mempool. Iteration is not
	// thread-safe and transaction may be mutated in serial order.
	//
	// XXX/TODO: It might be somewhat of a codesmell to use the gossip index for
	// iterator and cursor management when rechecking transactions. If the gossip
	// index changes or is removed in a future refactor, this will have to be
	// refactored. Instead, we should consider just keeping a slice of a snapshot
	// of the mempool's current transactions during Update and an integer cursor
	// into that slice. This, however, requires additional O(n) space complexity.
	recheckCursor *clist.CElement // next expected response
	recheckEnd    *clist.CElement // re-checking stops here

	// priorityIndex defines the priority index of valid transactions via a
	// thread-safe priority queue.
	priorityIndex *TxPriorityQueue

	// heightIndex defines a height-based, in ascending order, transaction index.
	// i.e. older transactions are first.
	heightIndex *WrappedTxList

	// timestampIndex defines a timestamp-based, in ascending order, transaction
	// index. i.e. older transactions are first.
	timestampIndex *WrappedTxList

	// pendingTxs stores transactions that are not valid yet but might become valid
	// if its checker returns Accepted
	pendingTxs *PendingTxs

	// A read/write lock is used to safe guard updates, insertions and deletions
	// from the mempool. A read-lock is implicitly acquired when executing CheckTx,
	// however, a caller must explicitly grab a write-lock via Lock when updating
	// the mempool via Update().
	mtx       sync.RWMutex
	preCheck  PreCheckFunc
	postCheck PostCheckFunc

	// NodeID to count of transactions failing CheckTx
	failedCheckTxCounts    map[types.NodeID]uint64
	mtxFailedCheckTxCounts sync.RWMutex

	peerManager PeerEvictor
}

func NewTxMempool(
	logger log.Logger,
	cfg *config.MempoolConfig,
	proxyAppConn abciclient.Client,
	peerManager PeerEvictor,
	options ...TxMempoolOption,
) *TxMempool {

	txmp := &TxMempool{
		logger:        logger,
		config:        cfg,
		proxyAppConn:  proxyAppConn,
		height:        -1,
		cache:         NopTxCache{},
		metrics:       NopMetrics(),
		txStore:       NewTxStore(),
		gossipIndex:   clist.New(),
		priorityIndex: NewTxPriorityQueue(),
		heightIndex: NewWrappedTxList(func(wtx1, wtx2 *WrappedTx) bool {
			return wtx1.height >= wtx2.height
		}),
		timestampIndex: NewWrappedTxList(func(wtx1, wtx2 *WrappedTx) bool {
			return wtx1.timestamp.After(wtx2.timestamp) || wtx1.timestamp.Equal(wtx2.timestamp)
		}),
		pendingTxs:          NewPendingTxs(cfg),
		failedCheckTxCounts: map[types.NodeID]uint64{},
		peerManager:         peerManager,
	}

	if cfg.CacheSize > 0 {
		txmp.cache = NewLRUTxCache(cfg.CacheSize)
	}

	for _, opt := range options {
		opt(txmp)
	}

	return txmp
}

// WithPreCheck sets a filter for the mempool to reject a transaction if f(tx)
// returns an error. This is executed before CheckTx. It only applies to the
// first created block. After that, Update() overwrites the existing value.
func WithPreCheck(f PreCheckFunc) TxMempoolOption {
	return func(txmp *TxMempool) { txmp.preCheck = f }
}

// WithPostCheck sets a filter for the mempool to reject a transaction if
// f(tx, resp) returns an error. This is executed after CheckTx. It only applies
// to the first created block. After that, Update overwrites the existing value.
func WithPostCheck(f PostCheckFunc) TxMempoolOption {
	return func(txmp *TxMempool) { txmp.postCheck = f }
}

// WithMetrics sets the mempool's metrics collector.
func WithMetrics(metrics *Metrics) TxMempoolOption {
	return func(txmp *TxMempool) { txmp.metrics = metrics }
}

func (txmp *TxMempool) TxStore() *TxStore {
	return txmp.txStore
}

// Lock obtains a write-lock on the mempool. A caller must be sure to explicitly
// release the lock when finished.
func (txmp *TxMempool) Lock() {
	txmp.mtx.Lock()
}

// Unlock releases a write-lock on the mempool.
func (txmp *TxMempool) Unlock() {
	txmp.mtx.Unlock()
}

// Size returns the number of valid transactions in the mempool. It is
// thread-safe.
func (txmp *TxMempool) Size() int {
	return txmp.SizeWithoutPending() + txmp.PendingSize()
}

func (txmp *TxMempool) SizeWithoutPending() int {
	return txmp.txStore.Size()
}

// PendingSize returns the number of pending transactions in the mempool.
func (txmp *TxMempool) PendingSize() int {
	return txmp.pendingTxs.Size()
}

// SizeBytes return the total sum in bytes of all the valid transactions in the
// mempool. It is thread-safe.
func (txmp *TxMempool) SizeBytes() int64 {
	return atomic.LoadInt64(&txmp.sizeBytes)
}

func (txmp *TxMempool) PendingSizeBytes() int64 {
	return atomic.LoadInt64(&txmp.pendingSizeBytes)
}

// FlushAppConn executes FlushSync on the mempool's proxyAppConn.
//
// NOTE: The caller must obtain a write-lock prior to execution.
func (txmp *TxMempool) FlushAppConn(ctx context.Context) error {
	return txmp.proxyAppConn.Flush(ctx)
}

// WaitForNextTx returns a blocking channel that will be closed when the next
// valid transaction is available to gossip. It is thread-safe.
func (txmp *TxMempool) WaitForNextTx() <-chan struct{} {
	return txmp.gossipIndex.WaitChan()
}

// NextGossipTx returns the next valid transaction to gossip. A caller must wait
// for WaitForNextTx to signal a transaction is available to gossip first. It is
// thread-safe.
func (txmp *TxMempool) NextGossipTx() *clist.CElement {
	return txmp.gossipIndex.Front()
}

// EnableTxsAvailable enables the mempool to trigger events when transactions
// are available on a block by block basis.
func (txmp *TxMempool) EnableTxsAvailable() {
	txmp.mtx.Lock()
	defer txmp.mtx.Unlock()

	txmp.txsAvailable = make(chan struct{}, 1)
}

// TxsAvailable returns a channel which fires once for every height, and only
// when transactions are available in the mempool. It is thread-safe.
func (txmp *TxMempool) TxsAvailable() <-chan struct{} {
	return txmp.txsAvailable
}

// CheckTx executes the ABCI CheckTx method for a given transaction.
// It acquires a read-lock and attempts to execute the application's
// CheckTx ABCI method synchronously. We return an error if any of
// the following happen:
//
//   - The CheckTx execution fails.
//   - The transaction already exists in the cache and we've already received the
//     transaction from the peer. Otherwise, if it solely exists in the cache, we
//     return nil.
//   - The transaction size exceeds the maximum transaction size as defined by the
//     configuration provided to the mempool.
//   - The transaction fails Pre-Check (if it is defined).
//   - The proxyAppConn fails, e.g. the buffer is full.
//
// If the mempool is full, we still execute CheckTx and attempt to find a lower
// priority transaction to evict. If such a transaction exists, we remove the
// lower priority transaction and add the new one with higher priority.
//
// NOTE:
// - The applications' CheckTx implementation may panic.
// - The caller is not to explicitly require any locks for executing CheckTx.
func (txmp *TxMempool) CheckTx(
	ctx context.Context,
	tx types.Tx,
	cb func(*abci.ResponseCheckTx),
	txInfo TxInfo,
) error {
	txmp.mtx.RLock()
	defer txmp.mtx.RUnlock()

	if txSize := len(tx); txSize > txmp.config.MaxTxBytes {
		return types.ErrTxTooLarge{
			Max:    txmp.config.MaxTxBytes,
			Actual: txSize,
		}
	}

	if txmp.preCheck != nil {
		if err := txmp.preCheck(tx); err != nil {
			return types.ErrPreCheck{Reason: err}
		}
	}

	if err := txmp.proxyAppConn.Error(); err != nil {
		return err
	}

	txHash := tx.Key()

	// We add the transaction to the mempool's cache and if the
	// transaction is already present in the cache, i.e. false is returned, then we
	// check if we've seen this transaction and error if we have.
	if !txmp.cache.Push(tx) {
		txmp.txStore.GetOrSetPeerByTxHash(txHash, txInfo.SenderID)
		return types.ErrTxInCache
	}

	res, err := txmp.proxyAppConn.CheckTx(ctx, &abci.RequestCheckTx{Tx: tx})

	// when a transaction is removed/expired/rejected, this should be called
	// The expire tx handler unreserves the pending nonce
	removeHandler := func(removeFromCache bool) {
		if removeFromCache {
			txmp.cache.Remove(tx)
		}
		if res.ExpireTxHandler != nil {
			res.ExpireTxHandler()
		}
	}

	if err != nil {
		removeHandler(true)
		res.Log = txmp.AppendCheckTxErr(res.Log, err.Error())
	}

	wtx := &WrappedTx{
		tx:            tx,
		hash:          txHash,
		timestamp:     time.Now().UTC(),
		height:        txmp.height,
		evmNonce:      res.EVMNonce,
		evmAddress:    res.EVMSenderAddress,
		isEVM:         res.IsEVM,
		removeHandler: removeHandler,
	}

	if err == nil {
		// only add new transaction if checkTx passes and is not pending
		if !res.IsPendingTransaction {
			err = txmp.addNewTransaction(wtx, res.ResponseCheckTx, txInfo)
			if err != nil {
				return err
			}
		} else {
			// otherwise add to pending txs store
			if res.Checker == nil {
				return errors.New("no checker available for pending transaction")
			}
			if err := txmp.canAddPendingTx(wtx); err != nil {
				// TODO: eviction strategy for pending transactions
				removeHandler(true)
				return err
			}
			atomic.AddInt64(&txmp.pendingSizeBytes, int64(wtx.Size()))
			if err := txmp.pendingTxs.Insert(wtx, res, txInfo); err != nil {
				return err
			}
		}
	}

	if cb != nil {
		cb(res.ResponseCheckTx)
	}

	return nil
}

func (txmp *TxMempool) isInMempool(tx types.Tx) bool {
	existingTx := txmp.txStore.GetTxByHash(tx.Key())
	return existingTx != nil && !existingTx.removed
}

func (txmp *TxMempool) RemoveTxByKey(txKey types.TxKey) error {
	txmp.Lock()
	defer txmp.Unlock()

	// remove the committed transaction from the transaction store and indexes
	if wtx := txmp.txStore.GetTxByHash(txKey); wtx != nil {
		txmp.removeTx(wtx, false, true, true)
		return nil
	}

	return errors.New("transaction not found")
}

func (txmp *TxMempool) HasTx(txKey types.TxKey) bool {
	txmp.Lock()
	defer txmp.Unlock()
	return txmp.txStore.GetTxByHash(txKey) != nil
}

func (txmp *TxMempool) GetTxsForKeys(txKeys []types.TxKey) types.Txs {
	txmp.mtx.RLock()
	defer txmp.mtx.RUnlock()

	txs := make([]types.Tx, 0, len(txKeys))
	for _, txKey := range txKeys {
		wtx := txmp.txStore.GetTxByHash(txKey)
		txs = append(txs, wtx.tx)
	}
	return txs
}

// Flush empties the mempool. It acquires a read-lock, fetches all the
// transactions currently in the transaction store and removes each transaction
// from the store and all indexes and finally resets the cache.
//
// NOTE:
// - Flushing the mempool may leave the mempool in an inconsistent state.
func (txmp *TxMempool) Flush() {
	txmp.mtx.RLock()
	defer txmp.mtx.RUnlock()

	txmp.heightIndex.Reset()
	txmp.timestampIndex.Reset()

	for _, wtx := range txmp.txStore.GetAllTxs() {
		txmp.removeTx(wtx, false, false, true)
	}

	atomic.SwapInt64(&txmp.sizeBytes, 0)
	txmp.cache.Reset()
}

// ReapMaxBytesMaxGas returns a list of transactions within the provided size
// and gas constraints. Transaction are retrieved in priority order.
//
// NOTE:
//   - Transactions returned are not removed from the mempool transaction
//     store or indexes.
func (txmp *TxMempool) ReapMaxBytesMaxGas(maxBytes, maxGas int64) types.Txs {
	txmp.mtx.Lock()
	defer txmp.mtx.Unlock()

	var (
		totalGas  int64
		totalSize int64
	)

	var txs []types.Tx
	if uint64(txmp.SizeWithoutPending()) < txmp.config.TxNotifyThreshold {
		// do not reap anything if threshold is not met
		return txs
	}
	txmp.priorityIndex.ForEachTx(func(wtx *WrappedTx) bool {
		size := types.ComputeProtoSizeForTxs([]types.Tx{wtx.tx})

		if maxBytes > -1 && totalSize+size > maxBytes {
			return false
		}
		totalSize += size
		gas := totalGas + wtx.gasWanted
		if maxGas > -1 && gas > maxGas {
			return false
		}

		totalGas = gas

		txs = append(txs, wtx.tx)
		return true
	})

	return txs
}

// ReapMaxTxs returns a list of transactions within the provided number of
// transactions bound. Transaction are retrieved in priority order.
//
// NOTE:
//   - Transactions returned are not removed from the mempool transaction
//     store or indexes.
func (txmp *TxMempool) ReapMaxTxs(max int) types.Txs {
	txmp.mtx.Lock()
	defer txmp.mtx.Unlock()

	wTxs := txmp.priorityIndex.PeekTxs(max)
	txs := make([]types.Tx, 0, len(wTxs))
	for _, wtx := range wTxs {
		txs = append(txs, wtx.tx)
	}
	if len(txs) < max {
		// retrieve more from pending txs
		pending := txmp.pendingTxs.Peek(max - len(txs))
		for _, ptx := range pending {
			txs = append(txs, ptx.tx.tx)
		}
	}
	return txs
}

// Update iterates over all the transactions provided by the block producer,
// removes them from the cache (if applicable), and removes
// the transactions from the main transaction store and associated indexes.
// If there are transactions remaining in the mempool, we initiate a
// re-CheckTx for them (if applicable), otherwise, we notify the caller more
// transactions are available.
//
// NOTE:
// - The caller must explicitly acquire a write-lock.
func (txmp *TxMempool) Update(
	ctx context.Context,
	blockHeight int64,
	blockTxs types.Txs,
	execTxResult []*abci.ExecTxResult,
	newPreFn PreCheckFunc,
	newPostFn PostCheckFunc,
	recheck bool,
) error {
	txmp.height = blockHeight
	txmp.notifiedTxsAvailable = false

	if newPreFn != nil {
		txmp.preCheck = newPreFn
	}
	if newPostFn != nil {
		txmp.postCheck = newPostFn
	}

	for i, tx := range blockTxs {
		if execTxResult[i].Code == abci.CodeTypeOK {
			// add the valid committed transaction to the cache (if missing)
			_ = txmp.cache.Push(tx)
		} else if !txmp.config.KeepInvalidTxsInCache {
			// allow invalid transactions to be re-submitted
			txmp.cache.Remove(tx)
		}

		// remove the committed transaction from the transaction store and indexes
		if wtx := txmp.txStore.GetTxByHash(tx.Key()); wtx != nil {
			txmp.removeTx(wtx, false, false, true)
		}
		if execTxResult[i].EvmTxInfo != nil {
			// remove any tx that has the same nonce (because the committed tx
			// may be from block proposal and is never in the local mempool)
			if wtx, _ := txmp.priorityIndex.GetTxWithSameNonce(&WrappedTx{
				evmAddress: execTxResult[i].EvmTxInfo.SenderAddress,
				evmNonce:   execTxResult[i].EvmTxInfo.Nonce,
			}); wtx != nil {
				txmp.removeTx(wtx, false, false, true)
			}
		}
	}

	txmp.purgeExpiredTxs(blockHeight)
	txmp.handlePendingTransactions()

	// If there any uncommitted transactions left in the mempool, we either
	// initiate re-CheckTx per remaining transaction or notify that remaining
	// transactions are left.
	if txmp.Size() > 0 {
		if recheck {
			txmp.logger.Debug(
				"executing re-CheckTx for all remaining transactions",
				"num_txs", txmp.Size(),
				"height", blockHeight,
			)
			txmp.updateReCheckTxs(ctx)
		} else {
			txmp.notifyTxsAvailable()
		}
	}

	txmp.metrics.Size.Set(float64(txmp.SizeWithoutPending()))
	txmp.metrics.PendingSize.Set(float64(txmp.PendingSize()))
	return nil
}

// addNewTransaction is invoked for a new unique transaction after CheckTx
// has been executed by the ABCI application for the first time on that transaction.
// CheckTx can be called again for the same transaction later when re-checking;
// however, this function will not be called. A recheck after a block is committed
// goes to handleRecheckResult.
//
// addNewTransaction runs after the ABCI application executes CheckTx.
// It runs the postCheck hook if one is defined on the mempool.
// If the CheckTx response code is not OK, or if the postCheck hook
// reports an error, the transaction is rejected. Otherwise, we attempt to insert
// the transaction into the mempool.
//
// When inserting a transaction, we first check if there is sufficient capacity.
// If there is, the transaction is added to the txStore and all indexes.
// Otherwise, if the mempool is full, we attempt to find a lower priority transaction
// to evict in place of the new incoming transaction. If no such transaction exists,
// the new incoming transaction is rejected.
//
// NOTE:
// - An explicit lock is NOT required.
func (txmp *TxMempool) addNewTransaction(wtx *WrappedTx, res *abci.ResponseCheckTx, txInfo TxInfo) error {
	var err error
	if txmp.postCheck != nil {
		err = txmp.postCheck(wtx.tx, res)
	}

	if err != nil || res.Code != abci.CodeTypeOK {
		// ignore bad transactions
		txmp.logger.Info(
			"rejected bad transaction",
			"priority", wtx.priority,
			"tx", fmt.Sprintf("%X", wtx.tx.Hash()),
			"peer_id", txInfo.SenderNodeID,
			"code", res.Code,
			"post_check_err", err,
		)

		txmp.metrics.FailedTxs.Add(1)

		wtx.removeHandler(!txmp.config.KeepInvalidTxsInCache)
		if res.Code != abci.CodeTypeOK {
			txmp.mtxFailedCheckTxCounts.Lock()
			defer txmp.mtxFailedCheckTxCounts.Unlock()
			txmp.failedCheckTxCounts[txInfo.SenderNodeID]++
			if txmp.config.CheckTxErrorBlacklistEnabled && txmp.failedCheckTxCounts[txInfo.SenderNodeID] > uint64(txmp.config.CheckTxErrorThreshold) {
				// evict peer
				txmp.peerManager.Errored(txInfo.SenderNodeID, errors.New("checkTx error exceeded threshold"))
			}
		}
		return err
	}

	sender := res.Sender
	priority := res.Priority

	if len(sender) > 0 {
		if wtx := txmp.txStore.GetTxBySender(sender); wtx != nil {
			txmp.logger.Error(
				"rejected incoming good transaction; tx already exists for sender",
				"tx", fmt.Sprintf("%X", wtx.tx.Hash()),
				"sender", sender,
			)
			txmp.metrics.RejectedTxs.Add(1)
			return nil
		}
	}

	if err := txmp.canAddTx(wtx); err != nil {
		evictTxs := txmp.priorityIndex.GetEvictableTxs(
			priority,
			int64(wtx.Size()),
			txmp.SizeBytes(),
			txmp.config.MaxTxsBytes,
		)
		if len(evictTxs) == 0 {
			// No room for the new incoming transaction so we just remove it from
			// the cache.
			wtx.removeHandler(true)
			txmp.logger.Error(
				"rejected incoming good transaction; mempool full",
				"tx", fmt.Sprintf("%X", wtx.tx.Hash()),
				"err", err.Error(),
			)
			txmp.metrics.RejectedTxs.Add(1)
			return nil
		}

		// evict an existing transaction(s)
		//
		// NOTE:
		// - The transaction, toEvict, can be removed while a concurrent
		//   reCheckTx callback is being executed for the same transaction.
		for _, toEvict := range evictTxs {
			txmp.removeTx(toEvict, true, true, true)
			txmp.logger.Debug(
				"evicted existing good transaction; mempool full",
				"old_tx", fmt.Sprintf("%X", toEvict.tx.Hash()),
				"old_priority", toEvict.priority,
				"new_tx", fmt.Sprintf("%X", wtx.tx.Hash()),
				"new_priority", wtx.priority,
			)
			txmp.metrics.EvictedTxs.Add(1)
		}
	}

	wtx.gasWanted = res.GasWanted
	wtx.priority = priority
	wtx.sender = sender
	wtx.peers = map[uint16]struct{}{
		txInfo.SenderID: {},
	}

	if txmp.isInMempool(wtx.tx) {
		return nil
	}

	if txmp.insertTx(wtx) {
		txmp.logger.Debug(
			"inserted good transaction",
			"priority", wtx.priority,
			"tx", fmt.Sprintf("%X", wtx.tx.Hash()),
			"height", txmp.height,
			"num_txs", txmp.SizeWithoutPending(),
		)
		txmp.notifyTxsAvailable()
	}

	return nil
}

// handleRecheckResult handles the responses from ABCI CheckTx calls issued
// during the recheck phase of a block Update.  It removes any transactions
// invalidated by the application.
//
// The caller must hold a mempool write-lock (via Lock()) and when
// executing Update(), if the mempool is non-empty and Recheck is
// enabled, then all remaining transactions will be rechecked via
// CheckTx. The order transactions are rechecked must be the same as
// the order in which this callback is called.
//
// This method is NOT executed for the initial CheckTx on a new transaction;
// that case is handled by addNewTransaction instead.
func (txmp *TxMempool) handleRecheckResult(tx types.Tx, res *abci.ResponseCheckTxV2) {
	if txmp.recheckCursor == nil {
		return
	}

	txmp.metrics.RecheckTimes.Add(1)

	wtx := txmp.recheckCursor.Value.(*WrappedTx)

	// Search through the remaining list of tx to recheck for a transaction that matches
	// the one we received from the ABCI application.
	for {
		if bytes.Equal(tx, wtx.tx) {
			// We've found a tx in the recheck list that matches the tx that we
			// received from the ABCI application.
			// Break, and use this transaction for further checks.
			break
		}

		txmp.logger.Debug(
			"re-CheckTx transaction mismatch",
			"got", wtx.tx.Hash(),
			"expected", tx.Key(),
		)

		if txmp.recheckCursor == txmp.recheckEnd {
			// we reached the end of the recheckTx list without finding a tx
			// matching the one we received from the ABCI application.
			// Return without processing any tx.
			txmp.recheckCursor = nil
			return
		}

		txmp.recheckCursor = txmp.recheckCursor.Next()
		wtx = txmp.recheckCursor.Value.(*WrappedTx)
	}

	// Only evaluate transactions that have not been removed. This can happen
	// if an existing transaction is evicted during CheckTx and while this
	// callback is being executed for the same evicted transaction.
	if !txmp.txStore.IsTxRemoved(wtx) {
		var err error
		if txmp.postCheck != nil {
			err = txmp.postCheck(tx, res.ResponseCheckTx)
		}

		// we will treat a transaction that turns pending in a recheck as invalid and evict it
		if res.Code == abci.CodeTypeOK && err == nil && !res.IsPendingTransaction {
			wtx.priority = res.Priority
		} else {
			txmp.logger.Debug(
				"existing transaction no longer valid; failed re-CheckTx callback",
				"priority", wtx.priority,
				"tx", fmt.Sprintf("%X", wtx.tx.Hash()),
				"err", err,
				"code", res.Code,
			)

			if wtx.gossipEl != txmp.recheckCursor {
				panic("corrupted reCheckTx cursor")
			}

			txmp.removeTx(wtx, !txmp.config.KeepInvalidTxsInCache, true, true)
		}
	}

	// move reCheckTx cursor to next element
	if txmp.recheckCursor == txmp.recheckEnd {
		txmp.recheckCursor = nil
	} else {
		txmp.recheckCursor = txmp.recheckCursor.Next()
	}

	if txmp.recheckCursor == nil {
		txmp.logger.Debug("finished rechecking transactions")

		if txmp.SizeWithoutPending() > 0 {
			txmp.notifyTxsAvailable()
		}
	}

	txmp.metrics.Size.Set(float64(txmp.SizeWithoutPending()))
	txmp.metrics.PendingSize.Set(float64(txmp.PendingSize()))
}

// updateReCheckTxs updates the recheck cursors using the gossipIndex. For
// each transaction, it executes CheckTx. The global callback defined on
// the proxyAppConn will be executed for each transaction after CheckTx is
// executed.
//
// NOTE:
// - The caller must have a write-lock when executing updateReCheckTxs.
func (txmp *TxMempool) updateReCheckTxs(ctx context.Context) {
	if txmp.Size() == 0 {
		panic("attempted to update re-CheckTx txs when mempool is empty")
	}
	txmp.logger.Debug(
		"executing re-CheckTx for all remaining transactions",
		"num_txs", txmp.Size(),
		"height", txmp.height,
	)

	txmp.recheckCursor = txmp.gossipIndex.Front()
	txmp.recheckEnd = txmp.gossipIndex.Back()

	for e := txmp.gossipIndex.Front(); e != nil; e = e.Next() {
		wtx := e.Value.(*WrappedTx)

		// Only execute CheckTx if the transaction is not marked as removed which
		// could happen if the transaction was evicted.
		if !txmp.txStore.IsTxRemoved(wtx) {
			res, err := txmp.proxyAppConn.CheckTx(ctx, &abci.RequestCheckTx{
				Tx:   wtx.tx,
				Type: abci.CheckTxType_Recheck,
			})
			if err != nil {
				// no need in retrying since the tx will be rechecked after the next block
				txmp.logger.Debug("failed to execute CheckTx during recheck", "err", err, "hash", fmt.Sprintf("%x", wtx.tx.Hash()))
				continue
			}
			txmp.handleRecheckResult(wtx.tx, res)
		}
	}

	if err := txmp.proxyAppConn.Flush(ctx); err != nil {
		txmp.logger.Error("failed to flush transactions during rechecking", "err", err)
	}
}

// canAddTx returns an error if we cannot insert the provided *WrappedTx into
// the mempool due to mempool configured constraints. If it returns nil,
// the transaction can be inserted into the mempool.
func (txmp *TxMempool) canAddTx(wtx *WrappedTx) error {
	var (
		numTxs    = txmp.SizeWithoutPending()
		sizeBytes = txmp.SizeBytes()
	)

	if numTxs >= txmp.config.Size || int64(wtx.Size())+sizeBytes > txmp.config.MaxTxsBytes {
		return types.ErrMempoolIsFull{
			NumTxs:      numTxs,
			MaxTxs:      txmp.config.Size,
			TxsBytes:    sizeBytes,
			MaxTxsBytes: txmp.config.MaxTxsBytes,
		}
	}

	return nil
}

func (txmp *TxMempool) canAddPendingTx(wtx *WrappedTx) error {
	var (
		numTxs    = txmp.PendingSize()
		sizeBytes = txmp.PendingSizeBytes()
	)

	if numTxs >= txmp.config.PendingSize || int64(wtx.Size())+sizeBytes > txmp.config.MaxPendingTxsBytes {
		return types.ErrMempoolPendingIsFull{
			NumTxs:      numTxs,
			MaxTxs:      txmp.config.PendingSize,
			TxsBytes:    sizeBytes,
			MaxTxsBytes: txmp.config.MaxPendingTxsBytes,
		}
	}

	return nil
}

func (txmp *TxMempool) insertTx(wtx *WrappedTx) bool {
	replacedTx, inserted := txmp.priorityIndex.PushTx(wtx)
	if !inserted {
		return false
	}
	txmp.metrics.TxSizeBytes.Observe(float64(wtx.Size()))
	txmp.metrics.Size.Set(float64(txmp.SizeWithoutPending()))
	txmp.metrics.PendingSize.Set(float64(txmp.PendingSize()))

	if replacedTx != nil {
		txmp.removeTx(replacedTx, true, false, false)
	}

	txmp.txStore.SetTx(wtx)
	txmp.heightIndex.Insert(wtx)
	txmp.timestampIndex.Insert(wtx)

	// Insert the transaction into the gossip index and mark the reference to the
	// linked-list element, which will be needed at a later point when the
	// transaction is removed.
	gossipEl := txmp.gossipIndex.PushBack(wtx)
	wtx.gossipEl = gossipEl

	txmp.metrics.InsertedTxs.Add(1)
	atomic.AddInt64(&txmp.sizeBytes, int64(wtx.Size()))
	return true
}

func (txmp *TxMempool) removeTx(wtx *WrappedTx, removeFromCache bool, shouldReenqueue bool, updatePriorityIndex bool) {
	if txmp.txStore.IsTxRemoved(wtx) {
		return
	}

	txmp.txStore.RemoveTx(wtx)
	toBeReenqueued := []*WrappedTx{}
	if updatePriorityIndex {
		toBeReenqueued = txmp.priorityIndex.RemoveTx(wtx, shouldReenqueue)
	}
	txmp.heightIndex.Remove(wtx)
	txmp.timestampIndex.Remove(wtx)

	// Remove the transaction from the gossip index and cleanup the linked-list
	// element so it can be garbage collected.
	txmp.gossipIndex.Remove(wtx.gossipEl)
	wtx.gossipEl.DetachPrev()

	txmp.metrics.RemovedTxs.Add(1)
	atomic.AddInt64(&txmp.sizeBytes, int64(-wtx.Size()))

	wtx.removeHandler(removeFromCache)

	if shouldReenqueue {
		for _, reenqueue := range toBeReenqueued {
			txmp.removeTx(reenqueue, removeFromCache, false, true)
		}
		for _, reenqueue := range toBeReenqueued {
			rtx := reenqueue.tx
			go func() {
				if err := txmp.CheckTx(context.Background(), rtx, nil, TxInfo{}); err != nil {
					txmp.logger.Error(fmt.Sprintf("failed to reenqueue transaction %X due to %s", rtx.Hash(), err))
				}
			}()
		}
	}
}

func (txmp *TxMempool) expire(blockHeight int64, wtx *WrappedTx) {
	txmp.metrics.ExpiredTxs.Add(1)
	txmp.logExpiredTx(blockHeight, wtx)
	wtx.removeHandler(!txmp.config.KeepInvalidTxsInCache)
}

func (txmp *TxMempool) logExpiredTx(blockHeight int64, wtx *WrappedTx) {
	// defensive check
	if wtx == nil {
		return
	}

	txmp.logger.Info(
		"transaction expired",
		"priority", wtx.priority,
		"tx", fmt.Sprintf("%X", wtx.tx.Hash()),
		"address", wtx.evmAddress,
		"evm", wtx.isEVM,
		"nonce", wtx.evmNonce,
		"height", blockHeight,
		"tx_height", wtx.height,
		"tx_timestamp", wtx.timestamp,
		"age", time.Since(wtx.timestamp),
	)
}

// purgeExpiredTxs removes all transactions that have exceeded their respective
// height- and/or time-based TTLs from their respective indexes. Every expired
// transaction will be removed from the mempool, but preserved in the cache (except for pending txs).
//
// NOTE: purgeExpiredTxs must only be called during TxMempool#Update in which
// the caller has a write-lock on the mempool and so we can safely iterate over
// the height and time based indexes.
func (txmp *TxMempool) purgeExpiredTxs(blockHeight int64) {
	now := time.Now()
	expiredTxs := make(map[types.TxKey]*WrappedTx)

	if txmp.config.TTLNumBlocks > 0 {
		purgeIdx := -1
		for i, wtx := range txmp.heightIndex.txs {
			if (blockHeight - wtx.height) > txmp.config.TTLNumBlocks {
				expiredTxs[wtx.tx.Key()] = wtx
				purgeIdx = i
			} else {
				// since the index is sorted, we know no other txs can be be purged
				break
			}
		}

		if purgeIdx >= 0 {
			txmp.heightIndex.txs = txmp.heightIndex.txs[purgeIdx+1:]
		}
	}

	if txmp.config.TTLDuration > 0 {
		purgeIdx := -1
		for i, wtx := range txmp.timestampIndex.txs {
			if now.Sub(wtx.timestamp) > txmp.config.TTLDuration {
				expiredTxs[wtx.tx.Key()] = wtx
				purgeIdx = i
			} else {
				// since the index is sorted, we know no other txs can be be purged
				break
			}
		}

		if purgeIdx >= 0 {
			txmp.timestampIndex.txs = txmp.timestampIndex.txs[purgeIdx+1:]
		}
	}

	for _, wtx := range expiredTxs {
		txmp.expire(blockHeight, wtx)
	}

	// remove pending txs that have expired
	txmp.pendingTxs.PurgeExpired(blockHeight, now, func(wtx *WrappedTx) {
		atomic.AddInt64(&txmp.pendingSizeBytes, int64(-wtx.Size()))
		txmp.expire(blockHeight, wtx)
	})
}

func (txmp *TxMempool) notifyTxsAvailable() {
	if txmp.SizeWithoutPending() == 0 {
		return
	}

	if txmp.txsAvailable != nil && !txmp.notifiedTxsAvailable {
		// channel cap is 1, so this will send once
		txmp.notifiedTxsAvailable = true

		select {
		case txmp.txsAvailable <- struct{}{}:
		default:
		}
	}
}

func (txmp *TxMempool) GetPeerFailedCheckTxCount(nodeID types.NodeID) uint64 {
	txmp.mtxFailedCheckTxCounts.RLock()
	defer txmp.mtxFailedCheckTxCounts.RUnlock()
	return txmp.failedCheckTxCounts[nodeID]
}

// AppendCheckTxErr wraps error message into an ABCIMessageLogs json string
func (txmp *TxMempool) AppendCheckTxErr(existingLogs string, log string) string {
	var builder strings.Builder

	builder.WriteString(existingLogs)
	// If there are already logs, append the new log with a separator
	if builder.Len() > 0 {
		builder.WriteString("; ")
	}
	builder.WriteString(log)

	return builder.String()
}

func (txmp *TxMempool) handlePendingTransactions() {
	accepted, rejected := txmp.pendingTxs.EvaluatePendingTransactions()
	for _, tx := range accepted {
		atomic.AddInt64(&txmp.pendingSizeBytes, int64(-tx.tx.Size()))
		if err := txmp.addNewTransaction(tx.tx, tx.checkTxResponse.ResponseCheckTx, tx.txInfo); err != nil {
			txmp.logger.Error(fmt.Sprintf("error adding pending transaction: %s", err))
		}
	}
	for _, tx := range rejected {
		atomic.AddInt64(&txmp.pendingSizeBytes, int64(-tx.tx.Size()))
		if !txmp.config.KeepInvalidTxsInCache {
			tx.tx.removeHandler(true)
		}
	}
}
