package mempool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/clist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/reservoir"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "mempool")

// ErrTxInCache is returned to the client if we saw tx earlier.
var ErrTxInCache = errors.New("tx already exists in cache")

// ErrTxTooLarge defines an error when a transaction is too big to be sent to peers.
var ErrTxTooLarge = errors.New("tx too large")

// Using SHA-256 truncated to 128 bits as the cache key: At 2K tx/sec, the
// collision probability is effectively zero (≈10^-29 for 120K keys in a minute,
// still negligible over years). If reduced 3× smaller (~43 bits), collisions
// become probable within a day and guaranteed over longer periods.
//
// For the purposes of the LRU cache key both sizes are sufficiently secure. For
// now. 128 bits is a safe balance between performance and collision probability
// and we may revisit later.
const maxCacheKeySize = sha256.Size / 2

// MinTxsPerBlock is how many txs we will attempt to have in a block if there's still space.
// MinGasEVMTx is the minimum the gas estimate can be for an EVM tx to be considered valid.
const (
	MinTxsToPeek = 10
	MinGasEVMTx  = 21000
)

type Config struct {
	// Maximum number of transactions in the mempool
	Size int

	// Limit the total size of all txs in the mempool.
	// This only accounts for raw transactions (e.g. given 1MB transactions and
	// max-txs-bytes=5MB, mempool will only accept 5 transactions).
	MaxTxsBytes int64

	// Size of the cache (used to filter transactions we saw earlier) in transactions
	CacheSize int

	// Size of the duplicate cache used to track duplicate txs
	DuplicateTxsCacheSize int

	// Do not remove invalid transactions from the cache (default: false)
	// Set to true if it's not possible for any invalid transaction to become
	// valid again in the future.
	KeepInvalidTxsInCache bool

	// Maximum size of a single transaction
	// NOTE: the max size of a tx transmitted over the network is {max-tx-bytes}.
	MaxTxBytes int

	// TTLDuration, if non-zero, defines the maximum amount of time a transaction
	// can exist for in the mempool.
	//
	// Note, if TTLNumBlocks is also defined, a transaction will be removed if it
	// has existed in the mempool at least TTLNumBlocks number of blocks or if it's
	// insertion time into the mempool is beyond TTLDuration.
	TTLDuration time.Duration

	// TTLNumBlocks, if non-zero, defines the maximum number of blocks a transaction
	// can exist for in the mempool.
	//
	// Note, if TTLDuration is also defined, a transaction will be removed if it
	// has existed in the mempool at least TTLNumBlocks number of blocks or if
	// it's insertion time into the mempool is beyond TTLDuration.
	TTLNumBlocks int64

	// TxNotifyThreshold, if non-zero, defines the minimum number of transactions
	// needed to trigger a notification in mempool's Tx notifier
	TxNotifyThreshold uint64

	// Maximum number of transactions in the pending set
	PendingSize int

	// Limit the total size of all txs in the pending set.
	MaxPendingTxsBytes int64

	RemoveExpiredTxsFromQueue bool

	// DropPriorityThreshold defines the percentage of transactions with the lowest
	// priority hint (expressed as a float in the range [0.0, 1.0]) that will be
	// dropped from the mempool once the configured utilisation threshold is reached.
	//
	// The default value of 0.1 means that the lowest 10% of transactions by
	// priority will be dropped when the mempool utilisation exceeds the
	// DropUtilisationThreshold.
	//
	// See DropUtilisationThreshold.
	DropPriorityThreshold float64

	// DropUtilisationThreshold defines the mempool utilisation level (expressed as
	// a percentage in the range [0.0, 1.0]) above which transactions will be
	// selectively dropped based on their priority hint.
	//
	// For example, if this parameter is set to 0.8, then once the mempool reaches
	// 80% capacity, transactions with priority hints below DropPriorityThreshold
	// percentile will be dropped to make room for new transactions.
	DropUtilisationThreshold float64

	// DropPriorityReservoirSize defines the size of the reservoir for keeping track
	// of the distribution of transaction priorities in the mempool.
	//
	// This is used to determine the priority threshold below which transactions will
	// be dropped when the mempool utilisation exceeds DropUtilisationThreshold.
	//
	// The reservoir is a statistically representative sample of transaction
	// priorities in the mempool, and is used to estimate the priority distribution
	// without needing to store all transaction priorities.
	//
	// A larger reservoir size will yield a more accurate estimate of the priority
	// distribution, but will consume more memory.
	//
	// The default value of 10,240 is a reasonable compromise between accuracy and
	// memory usage for most use cases. It takes approximately 80KB of memory storing
	// int64 transaction priorities.
	//
	// See DropUtilisationThreshold and DropPriorityThreshold.
	DropPriorityReservoirSize int `mapstructure:"drop-priority-reservoir-size"`
}

func DefaultConfig() *Config {
	return &Config{
		// Each signature verification takes .5ms, Size reduced until we implement
		// ABCI Recheck
		Size:                      5000,
		MaxTxsBytes:               1024 * 1024 * 1024, // 1GB
		CacheSize:                 10000,
		DuplicateTxsCacheSize:     100000,
		MaxTxBytes:                1024 * 1024,     // 1MB
		TTLDuration:               5 * time.Second, // prevent stale txs from filling mempool
		TTLNumBlocks:              10,              // remove txs after 10 blocks
		TxNotifyThreshold:         0,
		PendingSize:               5000,
		MaxPendingTxsBytes:        1024 * 1024 * 1024, // 1GB
		RemoveExpiredTxsFromQueue: true,
		DropPriorityThreshold:     0.1,
		DropUtilisationThreshold:  1.0,
		DropPriorityReservoirSize: 10_240,
	}
}

// TxMempool defines a prioritized mempool data structure used by the v1 mempool
// reactor. It keeps a thread-safe priority queue of transactions that is used
// when a block proposer constructs a block and a thread-safe linked-list that
// is used to gossip transactions to peers in a FIFO manner.
type TxMempool struct {
	metrics *Metrics
	config  *Config
	app     abci.Application

	// txsAvailable fires once for each height when the mempool is not empty
	txsAvailable         chan struct{}
	notifiedTxsAvailable atomic.Bool

	// height defines the last block height process during Update()
	height int64

	// cache defines a fixed-size cache of already seen transactions as this
	// reduces pressure on the proxyApp.
	cache TxCache

	// blockFailedTxs tracks tx hashes that have previously failed during
	// block execution. Used to prevent infinite re-entry of txs that
	// consistently fail before fee charging in DeliverTx.
	blockFailedTxs TxCache

	// A TTL cache which keeps all txs that we have seen before over the TTL window.
	// Currently, this can be used for tracking whether checkTx is always serving the same tx or not.
	duplicateTxsCache utils.Option[*DuplicateTxCache]

	// txStore defines the main storage of valid transactions. Indexes are built
	// on top of this store.
	txStore *TxStore

	// gossipIndex defines the gossiping index of valid transactions via a
	// thread-safe linked-list. We also use the gossip index as a cursor for
	// rechecking transactions already in the mempool.
	gossipIndex *clist.CList[*WrappedTx]

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
	recheckCursor *clist.CElement[*WrappedTx] // next expected response
	recheckEnd    *clist.CElement[*WrappedTx] // re-checking stops here

	// priorityIndex defines the priority index of valid transactions via a
	// thread-safe priority queue.
	priorityIndex *TxPriorityQueue

	// expirationIndex defines a timestamp-based, in ascending order, transaction
	// index. i.e. older transactions are first.
	expirationIndex *WrappedTxList

	// pendingTxs stores transactions that are not valid yet but might become valid
	// if its checker returns Accepted
	pendingTxs *PendingTxs

	// A read/write lock is used to safe guard updates, insertions and deletions
	// from the mempool. A read-lock is implicitly acquired when executing CheckTx,
	// however, a caller must explicitly grab a write-lock via Lock when updating
	// the mempool via Update().
	mtx                  sync.RWMutex
	txConstraintsFetcher TxConstraintsFetcher

	priorityReservoir *reservoir.Sampler[int64]
}

func NewTxMempool(
	cfg *Config,
	app abci.Application,
	metrics *Metrics,
	txConstraintsFetcher TxConstraintsFetcher,
) *TxMempool {

	txmp := &TxMempool{
		config:               cfg,
		app:                  app,
		txsAvailable:         make(chan struct{}, 1),
		height:               -1,
		cache:                NopTxCache{},
		blockFailedTxs:       NopTxCache{},
		metrics:              metrics,
		txStore:              NewTxStore(),
		gossipIndex:          clist.New[*WrappedTx](),
		priorityIndex:        NewTxPriorityQueue(),
		expirationIndex:      NewWrappedTxList(),
		pendingTxs:           NewPendingTxs(cfg),
		txConstraintsFetcher: txConstraintsFetcher,
		priorityReservoir:    reservoir.New[int64](cfg.DropPriorityReservoirSize, cfg.DropPriorityThreshold, nil), // Use non-deterministic RNG
	}

	if cfg.CacheSize > 0 {
		txmp.cache = NewLRUTxCache(cfg.CacheSize, maxCacheKeySize)
		txmp.blockFailedTxs = NewLRUTxCache(cfg.CacheSize, maxCacheKeySize)
	}

	if cfg.DuplicateTxsCacheSize > 0 {
		txmp.duplicateTxsCache = utils.Some(NewDuplicateTxCache(cfg.DuplicateTxsCacheSize, 1*time.Minute, maxCacheKeySize))
	}

	return txmp
}

func (txmp *TxMempool) Config() *Config { return txmp.config }

func (txmp *TxMempool) App() abci.Application { return txmp.app }

func (txmp *TxMempool) TxStore() *TxStore { return txmp.txStore }

// Lock obtains a write-lock on the mempool. A caller must be sure to explicitly
// release the lock when finished.
func (txmp *TxMempool) Lock() { txmp.mtx.Lock() }

// Unlock releases a write-lock on the mempool.
func (txmp *TxMempool) Unlock() { txmp.mtx.Unlock() }

// Size returns the number of valid transactions in the mempool. It is
// thread-safe.
func (txmp *TxMempool) Size() int {
	return txmp.NumTxsNotPending() + txmp.PendingSize()
}

func (txmp *TxMempool) utilisation() float64 {
	return float64(txmp.NumTxsNotPending()) / float64(txmp.config.Size)
}

func (txmp *TxMempool) NumTxsNotPending() int {
	return txmp.txStore.Size()
}

func (txmp *TxMempool) BytesNotPending() int64 {
	return txmp.txStore.AllTxsBytes()
}

func (txmp *TxMempool) TotalTxsBytesSize() int64 {
	return txmp.BytesNotPending() + txmp.pendingTxs.SizeBytes()
}

// PendingSize returns the number of pending transactions in the mempool.
func (txmp *TxMempool) PendingSize() int { return txmp.pendingTxs.Size() }

// SizeBytes return the total sum in bytes of all the valid transactions in the
// mempool. It is thread-safe.
func (txmp *TxMempool) SizeBytes() int64 { return txmp.txStore.AllTxsBytes() }

func (txmp *TxMempool) PendingSizeBytes() int64 { return txmp.pendingTxs.SizeBytes() }

// WaitForNextTx waits until the next transaction is available for gossip.
// Returns the next valid transaction to gossip.
func (txmp *TxMempool) WaitForNextTx(ctx context.Context) (*clist.CElement[*WrappedTx], error) {
	return txmp.gossipIndex.WaitFront(ctx)
}

// TxsAvailable returns a channel which fires once for every height, and only
// when transactions are available in the mempool. It is thread-safe.
func (txmp *TxMempool) TxsAvailable() <-chan struct{} {
	return txmp.txsAvailable
}

func (txmp *TxMempool) checkResponseState(res *abci.ResponseCheckTx) error {
	constraints, err := txmp.txConstraintsFetcher()
	if err != nil {
		return err
	}

	if constraints.MaxGas == -1 {
		return nil
	}
	if res.GasWanted < 0 {
		return fmt.Errorf("negative gas wanted: %d", res.GasWanted)
	}
	if res.GasWanted > constraints.MaxGas {
		return fmt.Errorf("gas wanted exceeds max gas: gas wanted %d is greater than max gas %d", res.GasWanted, constraints.MaxGas)
	}

	return nil
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
//   - The transaction fails the consensus-derived mempool checks.
//   - The app fails, e.g. the buffer is full.
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
	txInfo TxInfo,
) (*abci.ResponseCheckTx, error) {
	txmp.mtx.RLock()
	defer txmp.mtx.RUnlock()

	if txSize := len(tx); txSize > txmp.config.MaxTxBytes {
		return nil, fmt.Errorf("%w: max size is %d, but got %d", ErrTxTooLarge, txmp.config.MaxTxBytes, txSize)
	}
	constraints, err := txmp.txConstraintsFetcher()
	if err != nil {
		return nil, fmt.Errorf("txmp.txConstraintsFetcher(): %w", err)
	}
	if txSize := types.ComputeProtoSizeForTxs([]types.Tx{tx}); txSize > constraints.MaxDataBytes {
		return nil, fmt.Errorf("%w: tx size is too big: %d, max: %d", ErrTxTooLarge, txSize, constraints.MaxDataBytes)
	}

	// Reject low priority transactions when the mempool is more than
	// DropUtilisationThreshold full.
	if txmp.config.DropUtilisationThreshold > 0 && txmp.utilisation() >= txmp.config.DropUtilisationThreshold {
		txmp.metrics.CheckTxMetDropUtilisationThreshold.Add(1)

		hint, err := txmp.app.GetTxPriorityHint(ctx, &abci.RequestGetTxPriorityHintV2{Tx: tx})
		if err != nil {
			txmp.metrics.observeCheckTxPriorityDistribution(0, true, txInfo.SenderNodeID, err)
			logger.Error("failed to get tx priority hint", "err", err)
			return nil, err
		}
		txmp.metrics.observeCheckTxPriorityDistribution(hint.Priority, true, txInfo.SenderNodeID, nil)

		cutoff, found := txmp.priorityReservoir.Percentile()
		if found && hint.Priority <= cutoff {
			txmp.metrics.CheckTxDroppedByPriorityHint.Add(1)
			return nil, errors.New("priority not high enough for mempool")
		}
	}
	txHash := tx.Hash()

	// We add the transaction to the mempool's cache and if the
	// transaction is already present in the cache, i.e. false is returned, then we
	// check if we've seen this transaction and error if we have.
	if !txmp.cache.Push(txHash) {
		txmp.txStore.GetOrSetPeerByTxHash(txHash, txInfo.SenderID)
		return nil, ErrTxInCache
	}
	txmp.metrics.CacheSize.Set(float64(txmp.cache.Size()))

	// Check TTL cache to see if we've recently processed this transaction
	// Only execute TTL cache logic if we're using a real TTL cache (not NOP)
	if c, ok := txmp.duplicateTxsCache.Get(); ok {
		c.Increment(txHash)
	}

	res, err := txmp.app.CheckTx(ctx, &abci.RequestCheckTxV2{Tx: tx})
	if err != nil {
		txmp.metrics.NumberOfFailedCheckTxs.Add(1)
		txmp.metrics.observeCheckTxPriorityDistribution(0, false, txInfo.SenderNodeID, err)
	} else {
		txmp.metrics.NumberOfSuccessfulCheckTxs.Add(1)
		txmp.metrics.observeCheckTxPriorityDistribution(res.Priority, false, txInfo.SenderNodeID, nil)
	}
	if len(txInfo.SenderNodeID) == 0 {
		txmp.metrics.NumberOfLocalCheckTx.Add(1)
	}

	// when a transaction is removed/expired/rejected, this should be called
	// The expire tx handler unreserves the pending nonce
	removeHandler := func(removeFromCache bool) {
		if removeFromCache {
			txmp.cache.Remove(txHash)
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
		hashedTx:      newHashedTx(tx),
		timestamp:     time.Now().UTC(),
		height:        txmp.height,
		evmNonce:      res.EVMNonce,
		evmAddress:    res.EVMSenderAddress,
		isEVM:         res.IsEVM,
		removeHandler: removeHandler,
		estimatedGas:  res.GasEstimated,
	}

	if err == nil {
		// only add new transaction if checkTx passes and is not pending
		if !res.IsPendingTransaction {
			// Update transaction priority reservoir with the true Tx priority
			// as determined by the application.
			//
			// NOTE: This is done before potentially rejecting the transaction due to
			// mempool being full. This is to ensure that the reservoir contains a
			// representative sample of all transactions that have been processed by
			// CheckTx.
			//
			// However, this is NOT done if the tx is pending, since a spammer could
			// throw off the correct priority percentiles otherwise.
			//
			// We do not use the priority hint here as it may be misleading and
			// inaccurate. The true priority as determined by the application is the
			// most accurate.
			txmp.priorityReservoir.Add(res.Priority)
			err = txmp.addNewTransaction(wtx, res.ResponseCheckTx, txInfo)
			if err != nil {
				return nil, err
			}
		} else {
			// otherwise add to pending txs store
			if res.Checker == nil {
				return nil, errors.New("no checker available for pending transaction")
			}
			if err := txmp.canAddPendingTx(wtx); err != nil {
				// TODO: eviction strategy for pending transactions
				removeHandler(true)
				return nil, err
			}
			if err := txmp.pendingTxs.Insert(wtx, res, txInfo); err != nil {
				return nil, err
			}
		}

		if res.CheckTxCallback != nil {
			res.CheckTxCallback(res.Priority)
		}
	}

	return res.ResponseCheckTx, nil
}

func (txmp *TxMempool) isInMempool(txHash types.TxHash) bool {
	existingTx := txmp.txStore.GetTxByHash(txHash)
	return existingTx != nil && !existingTx.removed
}

func (txmp *TxMempool) RemoveTxByHash(txHash types.TxHash) error {
	txmp.Lock()
	defer txmp.Unlock()

	// remove the committed transaction from the transaction store and indexes
	if wtx := txmp.txStore.GetTxByHash(txHash); wtx != nil {
		txmp.removeTx(wtx, false, true, true)
		return nil
	}

	return errors.New("transaction not found")
}

func (txmp *TxMempool) HasTx(txHash types.TxHash) bool {
	txmp.Lock()
	defer txmp.Unlock()
	return txmp.txStore.GetTxByHash(txHash) != nil
}

func (txmp *TxMempool) GetTxsForHashes(txHashes []types.TxHash) types.Txs {
	txmp.mtx.RLock()
	defer txmp.mtx.RUnlock()

	txs := make([]types.Tx, 0, len(txHashes))
	for _, txHash := range txHashes {
		wtx := txmp.txStore.GetTxByHash(txHash)
		txs = append(txs, wtx.Tx())
	}
	return txs
}

func (txmp *TxMempool) SafeGetTxsForHashes(txHashes []types.TxHash) (types.Txs, []types.TxHash) {
	txmp.mtx.RLock()
	defer txmp.mtx.RUnlock()

	txs := make([]types.Tx, 0, len(txHashes))
	missing := []types.TxHash{}
	for _, txHash := range txHashes {
		wtx := txmp.txStore.GetTxByHash(txHash)
		if wtx == nil {
			missing = append(missing, txHash)
			continue
		}
		txs = append(txs, wtx.Tx())
	}
	return txs, missing
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

	txmp.expirationIndex.Reset()

	for _, wtx := range txmp.txStore.GetAllTxs() {
		txmp.removeTx(wtx, false, false, true)
	}

	txmp.cache.Reset()
}

// ReapMaxBytesMaxGas returns a list of transactions within the provided size
// and gas constraints. The returned list starts with EVM transactions (in priority order),
// followed by non-EVM transactions (in priority order).
// There are 4 types of constraints.
//  1. maxBytes - stops pulling txs from mempool once maxBytes is hit.
//  2. maxGasWanted - stops pulling txs from mempool once total gas wanted exceeds maxGasWanted.
//     Can be set to -1 to be ignored.
//  3. maxGasEstimated - similar to maxGasWanted but will use the estimated gas used for EVM txs
//     while still using gas wanted for cosmos txs. Can be set to -1 to be ignored.
//
// NOTE:
//   - Transactions returned are not removed from the mempool transaction
//     store or indexes.
func (txmp *TxMempool) ReapMaxBytesMaxGas(maxBytes, maxGasWanted, maxGasEstimated int64) types.Txs {
	txmp.mtx.Lock()
	defer txmp.mtx.Unlock()
	txs, _ := txmp.reapTxs(ReapLimits{
		MaxBytes:        utils.Some(maxBytes),
		MaxGasWanted:    utils.Some(maxGasWanted),
		MaxGasEstimated: utils.Some(maxGasEstimated),
	})
	return txs
}

type ReapLimits struct {
	MaxTxs          utils.Option[uint64]
	MaxBytes        utils.Option[int64]
	MaxGasWanted    utils.Option[int64]
	MaxGasEstimated utils.Option[int64]
}

// ReapMaxTxsBytesMaxGas returns a list of transactions within the provided tx,
// byte, and gas constraints together with the total estimated gas for the
// returned transactions.
//
// NOTE: Gas limits are enforced using int64 running totals. If those totals
// overflow, gas limit enforcement no longer works correctly. This preserves the
// historical behavior for backward compatibility.
func (txmp *TxMempool) reapTxs(l ReapLimits) (types.Txs, int64) {
	maxTxs := l.MaxTxs.Or(utils.Max[uint64]())
	maxBytes := l.MaxBytes.Or(utils.Max[int64]())
	maxGasWanted := l.MaxGasWanted.Or(utils.Max[int64]())
	maxGasEstimated := l.MaxGasEstimated.Or(utils.Max[int64]())
	if maxBytes < 0 {
		maxBytes = utils.Max[int64]()
	}
	if maxGasWanted < 0 {
		maxGasWanted = utils.Max[int64]()
	}
	if maxGasEstimated < 0 {
		maxGasEstimated = utils.Max[int64]()
	}
	var (
		totalGasWanted    int64
		totalGasEstimated int64
		totalSize         int64
	)

	numTxs := uint64(0)
	encounteredGasUnfit := false
	if uint64(txmp.NumTxsNotPending()) < txmp.config.TxNotifyThreshold { //nolint:gosec // NumTxsNotPending returns non-negative value
		// do not reap anything if threshold is not met
		return []types.Tx{}, 0
	}
	totalTxs := txmp.priorityIndex.NumTxs()
	evmTxs := make([]types.Tx, 0, totalTxs)
	nonEvmTxs := make([]types.Tx, 0, totalTxs)
	txmp.priorityIndex.ForEachTx(func(wtx *WrappedTx) bool {
		size := types.ComputeProtoSizeForTxs([]types.Tx{wtx.Tx()})

		// bytes limit is a hard stop
		if totalSize+size > maxBytes || numTxs+1 > maxTxs {
			return false
		}

		// if the tx doesn't have a gas estimate, fallback to gas wanted
		var txGasEstimate int64
		if wtx.estimatedGas >= MinGasEVMTx && wtx.estimatedGas <= wtx.gasWanted {
			txGasEstimate = wtx.estimatedGas
		} else {
			wtx.estimatedGas = wtx.gasWanted
			txGasEstimate = wtx.gasWanted
		}

		// prospective totals
		prospectiveGasWanted := totalGasWanted + wtx.gasWanted
		prospectiveGasEstimated := totalGasEstimated + txGasEstimate

		maxGasWantedExceeded := prospectiveGasWanted > maxGasWanted
		maxGasEstimatedExceeded := prospectiveGasEstimated > maxGasEstimated

		if maxGasWantedExceeded || maxGasEstimatedExceeded {
			// skip this unfit-by-gas tx once and attempt to pull up to 10 smaller ones
			if !encounteredGasUnfit && numTxs < MinTxsToPeek {
				encounteredGasUnfit = true
				return true
			}
			return false
		}

		// include tx and update totals
		numTxs += 1
		totalSize += size
		totalGasWanted = prospectiveGasWanted
		totalGasEstimated = prospectiveGasEstimated

		if wtx.isEVM {
			evmTxs = append(evmTxs, wtx.Tx())
		} else {
			nonEvmTxs = append(nonEvmTxs, wtx.Tx())
		}
		if encounteredGasUnfit && numTxs >= MinTxsToPeek {
			return false
		}
		return true
	})

	return append(evmTxs, nonEvmTxs...), totalGasEstimated
}

// RemoveTxs removes the provided transactions from the mempool if present.
func (txmp *TxMempool) PopTxs(l ReapLimits) (types.Txs, int64) {
	txmp.Lock()
	defer txmp.Unlock()
	txs, gasEstimated := txmp.reapTxs(l)
	for _, tx := range txs {
		if wtx := txmp.txStore.GetTxByHash(tx.Hash()); wtx != nil {
			txmp.removeTx(wtx, false, false, true)
		}
	}
	return txs, gasEstimated
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
		txs = append(txs, wtx.Tx())
	}
	if len(txs) < max {
		// retrieve more from pending txs
		pending := txmp.pendingTxs.Peek(max - len(txs))
		for _, ptx := range pending {
			txs = append(txs, ptx.tx.Tx())
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
	txConstraintsFetcher TxConstraintsFetcher,
	recheck bool,
) error {
	txmp.height = blockHeight
	txmp.notifiedTxsAvailable.Store(false)
	txmp.txConstraintsFetcher = txConstraintsFetcher

	for i, tx := range blockTxs {
		txHash := tx.Hash()
		if execTxResult[i].Code == abci.CodeTypeOK {
			// add the valid committed transaction to the cache (if missing)
			_ = txmp.cache.Push(txHash)
			txmp.blockFailedTxs.Remove(txHash)
		} else if !txmp.config.KeepInvalidTxsInCache {
			if txmp.blockFailedTxs.Push(txHash) {
				// First block failure: allow one retry
				txmp.cache.Remove(txHash)
			}
			// Subsequent failures: leave in cache to prevent infinite re-entry
		}

		// remove the committed transaction from the transaction store and indexes
		if wtx := txmp.txStore.GetTxByHash(txHash); wtx != nil {
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
			logger.Debug(
				"executing re-CheckTx for all remaining transactions",
				"num_txs", txmp.Size(),
				"height", blockHeight,
			)
			txmp.updateReCheckTxs(ctx)
		} else {
			txmp.notifyTxsAvailable()
		}
	}

	txmp.metrics.Size.Set(float64(txmp.NumTxsNotPending()))
	txmp.metrics.TotalTxsSizeBytes.Set(float64(txmp.TotalTxsBytesSize()))
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
// It runs the consensus-derived post-check for the current state snapshot.
// If the CheckTx response code is not OK, or if the post-check
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
	err := txmp.checkResponseState(res)

	if err != nil || res.Code != abci.CodeTypeOK {
		// ignore bad transactions
		logger.Info(
			"rejected bad transaction",
			"priority", wtx.priority,
			"tx", wtx.Hash(),
			"peer_id", txInfo.SenderNodeID,
			"code", res.Code,
			"post_check_err", err,
		)

		txmp.metrics.FailedTxs.Add(1)

		wtx.removeHandler(!txmp.config.KeepInvalidTxsInCache)

		return err
	}

	sender := res.Sender
	priority := res.Priority

	if len(sender) > 0 {
		if wtx := txmp.txStore.GetTxBySender(sender); wtx != nil {
			logger.Error(
				"rejected incoming good transaction; tx already exists for sender",
				"tx", wtx.Hash(),
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
			logger.Error(
				"rejected incoming good transaction; mempool full",
				"tx", wtx.Hash(),
				"err", err,
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
			logger.Debug(
				"evicted existing good transaction; mempool full",
				"old_tx", fmt.Sprintf("%X", toEvict.Hash()),
				"old_priority", toEvict.priority,
				"new_tx", wtx.Hash(),
				"new_priority", wtx.priority,
			)
			txmp.metrics.EvictedTxs.Add(1)
		}
	}

	wtx.gasWanted = res.GasWanted
	wtx.estimatedGas = res.GasEstimated
	wtx.priority = priority
	wtx.sender = sender
	wtx.peers = map[uint16]struct{}{
		txInfo.SenderID: {},
	}

	if txmp.isInMempool(wtx.Hash()) {
		return nil
	}

	if txmp.insertTx(wtx) {
		logger.Debug(
			"inserted good transaction",
			"priority", wtx.priority,
			"tx", wtx.Hash(),
			"height", txmp.height,
			"num_txs", txmp.NumTxsNotPending(),
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

	wtx := txmp.recheckCursor.Value()

	// Search through the remaining list of tx to recheck for a transaction that matches
	// the one we received from the ABCI application.
	for !bytes.Equal(tx, wtx.Tx()) {

		logger.Debug(
			"re-CheckTx transaction mismatch",
			"got", wtx.Hash(),
			"expected", tx.Hash(),
		)

		if txmp.recheckCursor == txmp.recheckEnd {
			// we reached the end of the recheckTx list without finding a tx
			// matching the one we received from the ABCI application.
			// Return without processing any tx.
			txmp.recheckCursor = nil
			return
		}

		txmp.recheckCursor = txmp.recheckCursor.Next()
		wtx = txmp.recheckCursor.Value()
	}

	// Only evaluate transactions that have not been removed. This can happen
	// if an existing transaction is evicted during CheckTx and while this
	// callback is being executed for the same evicted transaction.
	if !txmp.txStore.IsTxRemoved(wtx) {
		err := txmp.checkResponseState(res.ResponseCheckTx)

		// we will treat a transaction that turns pending in a recheck as invalid and evict it
		if res.Code == abci.CodeTypeOK && err == nil && !res.IsPendingTransaction {
			wtx.priority = res.Priority
		} else {
			logger.Debug(
				"existing transaction no longer valid; failed re-CheckTx callback",
				"priority", wtx.priority,
				"tx", wtx.Hash(),
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
		logger.Debug("finished rechecking transactions")

		if txmp.NumTxsNotPending() > 0 {
			txmp.notifyTxsAvailable()
		}
	}

	txmp.metrics.Size.Set(float64(txmp.NumTxsNotPending()))
	txmp.metrics.PendingSize.Set(float64(txmp.PendingSize()))
	txmp.metrics.TotalTxsSizeBytes.Set(float64(txmp.TotalTxsBytesSize()))
}

// updateReCheckTxs updates the recheck cursors using the gossipIndex. For
// each transaction, it executes CheckTx. The global callback defined on
// the app will be executed for each transaction after CheckTx is
// executed.
//
// NOTE:
// - The caller must have a write-lock when executing updateReCheckTxs.
func (txmp *TxMempool) updateReCheckTxs(ctx context.Context) {
	if txmp.Size() == 0 {
		panic("attempted to update re-CheckTx txs when mempool is empty")
	}
	logger.Debug(
		"executing re-CheckTx for all remaining transactions",
		"num_txs", txmp.Size(),
		"height", txmp.height,
	)

	txmp.recheckCursor = txmp.gossipIndex.Front()
	txmp.recheckEnd = txmp.gossipIndex.Back()

	for e := txmp.gossipIndex.Front(); e != nil; e = e.Next() {
		wtx := e.Value()

		// Only execute CheckTx if the transaction is not marked as removed which
		// could happen if the transaction was evicted.
		if !txmp.txStore.IsTxRemoved(wtx) {
			res, err := txmp.app.CheckTx(ctx, &abci.RequestCheckTxV2{
				Tx:   wtx.Tx(),
				Type: abci.CheckTxTypeV2Recheck,
			})
			if err != nil {
				// no need in retrying since the tx will be rechecked after the next block
				logger.Debug("failed to execute CheckTx during recheck", "err", err, "hash", wtx.Hash())
				continue
			}
			txmp.handleRecheckResult(wtx.Tx(), res)
		}
	}
}

// canAddTx returns an error if we cannot insert the provided *WrappedTx into
// the mempool due to mempool configured constraints. If it returns nil,
// the transaction can be inserted into the mempool.
func (txmp *TxMempool) canAddTx(wtx *WrappedTx) error {
	var (
		numTxs    = txmp.NumTxsNotPending()
		sizeBytes = txmp.SizeBytes()
	)

	if numTxs >= txmp.config.Size || int64(wtx.Size())+sizeBytes > txmp.config.MaxTxsBytes {
		return fmt.Errorf("mempool is full: number of txs %d (max: %d), total txs bytes %d (max: %d)",
			numTxs,
			txmp.config.Size,
			sizeBytes,
			txmp.config.MaxTxsBytes,
		)
	}

	return nil
}

func (txmp *TxMempool) canAddPendingTx(wtx *WrappedTx) error {
	var (
		numTxs    = txmp.PendingSize()
		sizeBytes = txmp.PendingSizeBytes()
	)

	if numTxs >= txmp.config.PendingSize || int64(wtx.Size())+sizeBytes > txmp.config.MaxPendingTxsBytes {
		return fmt.Errorf("mempool pending set is full: number of txs %d (max: %d), total txs bytes %d (max: %d)",
			numTxs,
			txmp.config.PendingSize,
			sizeBytes,
			txmp.config.MaxPendingTxsBytes,
		)
	}

	return nil
}

func (txmp *TxMempool) insertTx(wtx *WrappedTx) bool {
	replacedTx, inserted := txmp.priorityIndex.PushTx(wtx)
	if !inserted {
		return false
	}
	txmp.metrics.TxSizeBytes.Add(float64(wtx.Size()))
	txmp.metrics.Size.Set(float64(txmp.NumTxsNotPending()))
	txmp.metrics.PendingSize.Set(float64(txmp.PendingSize()))
	txmp.metrics.TotalTxsSizeBytes.Set(float64(txmp.TotalTxsBytesSize()))

	if replacedTx != nil {
		txmp.removeTx(replacedTx, true, false, false)
	}

	txmp.txStore.SetTx(wtx)
	txmp.expirationIndex.Insert(wtx)

	// Insert the transaction into the gossip index and mark the reference to the
	// linked-list element, which will be needed at a later point when the
	// transaction is removed.
	gossipEl := txmp.gossipIndex.PushBack(wtx)
	wtx.gossipEl = gossipEl

	txmp.metrics.InsertedTxs.Add(1)
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
	txmp.expirationIndex.Remove(wtx)

	// Remove the transaction from the gossip index and cleanup the linked-list
	// element so it can be garbage collected.
	txmp.gossipIndex.Remove(wtx.gossipEl)
	wtx.gossipEl.DetachPrev()

	txmp.metrics.RemovedTxs.Add(1)
	wtx.removeHandler(removeFromCache)

	if shouldReenqueue {
		for _, reenqueue := range toBeReenqueued {
			txmp.removeTx(reenqueue, removeFromCache, false, true)
		}
		for _, reenqueue := range toBeReenqueued {
			rtx := reenqueue.Tx()
			go func() {
				if _, err := txmp.CheckTx(context.Background(), rtx, TxInfo{}); err != nil {
					logger.Error("failed to reenqueue transaction", "tx-hash", rtx.Hash(), "err", err)
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

	logger.Info(
		"transaction expired",
		"priority", wtx.priority,
		"tx", wtx.Hash(),
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

	minHeight := utils.None[int64]()
	if n := txmp.config.TTLNumBlocks; n > 0 && blockHeight > n {
		minHeight = utils.Some(blockHeight - n)
	}
	minTime := utils.None[time.Time]()
	if d := txmp.config.TTLDuration; d > 0 {
		minTime = utils.Some(time.Now().Add(-d))
	}
	expiredTxs := txmp.expirationIndex.Purge(minTime, minHeight)

	for _, wtx := range expiredTxs {
		if txmp.config.RemoveExpiredTxsFromQueue {
			txmp.removeTx(wtx, !txmp.config.KeepInvalidTxsInCache, false, true)
		} else {
			txmp.expire(blockHeight, wtx)
		}
	}

	// remove pending txs that have expired
	txmp.pendingTxs.PurgeExpired(blockHeight, now, func(wtx *WrappedTx) {
		txmp.expire(blockHeight, wtx)
	})
}

func (txmp *TxMempool) notifyTxsAvailable() {
	if txmp.NumTxsNotPending() == 0 || txmp.notifiedTxsAvailable.Swap(true) {
		return
	}
	// channel cap is 1, so this will send once
	select {
	case txmp.txsAvailable <- struct{}{}:
	default:
	}
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
		if err := txmp.addNewTransaction(tx.tx, tx.checkTxResponse.ResponseCheckTx, tx.txInfo); err != nil {
			logger.Error("error adding pending transaction", "err", err)
		}
	}
	for _, tx := range rejected {
		if !txmp.config.KeepInvalidTxsInCache {
			tx.tx.removeHandler(true)
		}
	}
}

// Run executes mempool background tasks.
func (txmp *TxMempool) Run(ctx context.Context) error {
	c, ok := txmp.duplicateTxsCache.Get()
	if !ok {
		return nil
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return c.Run(ctx, time.Minute) })
		for {
			if err := utils.Sleep(ctx, 10*time.Second); err != nil {
				return err
			}
			// TODO(gprusak): instead of actively updating stats,
			// TxMempool should implement prometheus.Collector.
			maxOccurrence, totalOccurrence, duplicateCount, nonDuplicateCount := c.GetForMetrics()
			txmp.metrics.DuplicateTxMaxOccurrences.Set(float64(maxOccurrence))
			txmp.metrics.DuplicateTxTotalOccurrences.Set(float64(totalOccurrence))
			txmp.metrics.NumberOfDuplicateTxs.Set(float64(duplicateCount))
			txmp.metrics.NumberOfNonDuplicateTxs.Set(float64(nonDuplicateCount))
		}
	})
}
