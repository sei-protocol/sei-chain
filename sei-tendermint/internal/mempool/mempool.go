package mempool

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/clist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/reservoir"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
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

type evmAddrNonce struct {
	Address common.Address
	Nonce   uint64
}

// TxMempool defines a prioritized mempool data structure used by the v1 mempool
// reactor. It keeps a thread-safe priority queue of transactions that is used
// when a block proposer constructs a block and a thread-safe linked-list that
// is used to gossip transactions to peers in a FIFO manner.
type TxMempool struct {
	metrics *Metrics
	config  *Config
	app     *proxy.Proxy

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
	txStore *txStoreV2

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
	app *proxy.Proxy,
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

func (txmp *TxMempool) App() *proxy.Proxy { return txmp.app }

func (txmp *TxMempool) EvmNextPendingNonce(addr common.Address) uint64 {
	return txmp.txStore.NextNonce(addr)
}

func (txmp *TxMempool) TxStore() *txStoreV2 { return txmp.txStore }

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

func (txmp *TxMempool) NumTxsNotPending() int { return txmp.txStore.Size() }
func (txmp *TxMempool) BytesNotPending() uint64 { return txmp.txStore.AllTxsBytes() }
func (txmp *TxMempool) TotalTxsBytesSize() uint64 { return txmp.txStore.TotalBytes() }

// PendingSize returns the number of pending transactions in the mempool.
func (txmp *TxMempool) PendingSize() int        { return txmp.txStore.PendingSize() }
func (txmp *TxMempool) PendingSizeBytes() uint64 { return txmp.txStore.PendingBytes() }

// SizeBytes return the total sum in bytes of all the valid transactions in the
// mempool. It is thread-safe.
func (txmp *TxMempool) SizeBytes() uint64 { return txmp.txStore.AllTxsBytes() }

// WaitForNextTx waits until the next transaction is available for gossip.
// Returns the next valid transaction to gossip.
func (txmp *TxMempool) WaitForNextTx(ctx context.Context) (*clist.CElement[*WrappedTx], error) {
	return txmp.txStore.readyTxs.WaitFront(ctx)
}

// TxsAvailable returns a channel which fires once for every height, and only
// when transactions are available in the mempool. It is thread-safe.
func (txmp *TxMempool) TxsAvailable() <-chan struct{} { return txmp.txsAvailable }

func (txmp *TxMempool) removeTx(txHash types.TxHash) {
	if txmp.txStore.Remove(txHash) {
		txmp.metrics.RemovedTxs.Add(1)
	}
}

func (txmp *TxMempool) checkTxConstraints(wtx *WrappedTx) error {
	constraints, err := txmp.txConstraintsFetcher()
	if err != nil {
		return err
	}

	if constraints.MaxGas == -1 {
		return nil
	}
	if wtx.gasWanted < 0 {
		return fmt.Errorf("negative gas wanted: %d", wtx.gasWanted)
	}
	if wtx.gasWanted > constraints.MaxGas {
		return fmt.Errorf("gas wanted exceeds max gas: gas wanted %d is greater than max gas %d", wtx.gasWanted, constraints.MaxGas)
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
func (txmp *TxMempool) CheckTx(ctx context.Context, tx types.Tx, txInfo TxInfo) (*abci.ResponseCheckTx, error) {
	txmp.mtx.RLock()
	defer txmp.mtx.RUnlock()

	// Early exit if tx is too large.
	if txSize := len(tx); txSize > txmp.config.MaxTxBytes {
		return nil, fmt.Errorf("%w: max size is %d, but got %d", ErrTxTooLarge, txmp.config.MaxTxBytes, txSize)
	}
	hTx := newHashedTx(tx)
	constraints, err := txmp.txConstraintsFetcher()
	if err != nil {
		return nil, fmt.Errorf("txmp.txConstraintsFetcher(): %w", err)
	}
	if hTx.protoSize > constraints.MaxDataBytes {
		return nil, fmt.Errorf("%w: tx size is too big: %d, max: %d", ErrTxTooLarge, hTx.protoSize, constraints.MaxDataBytes)
	}

	// Reject low priority transactions when the mempool is more than
	// DropUtilisationThreshold full.
	if txmp.config.DropUtilisationThreshold > 0 && txmp.utilisation() >= txmp.config.DropUtilisationThreshold {
		txmp.metrics.CheckTxMetDropUtilisationThreshold.Add(1)

		hint, err := txmp.app.GetTxPriorityHint(ctx, &abci.RequestGetTxPriorityHintV2{Tx: tx})
		if err != nil {
			txmp.metrics.observeCheckTxPriorityDistribution(0, true, txInfo.SenderNodeID, true)
			logger.Error("failed to get tx priority hint", "err", err)
			return nil, err
		}
		txmp.metrics.observeCheckTxPriorityDistribution(hint.Priority, true, txInfo.SenderNodeID, false)

		cutoff, found := txmp.priorityReservoir.Percentile()
		if found && hint.Priority <= cutoff {
			txmp.metrics.CheckTxDroppedByPriorityHint.Add(1)
			return nil, errors.New("priority not high enough for mempool")
		}
	}

	// We add the transaction to the mempool's cache and if the
	// transaction is already present in the cache, i.e. false is returned, then we
	// check if we've seen this transaction and error if we have.
	if !txmp.cache.Push(hTx.Hash()) {
		txmp.txStore.GetOrSetPeerByTxHash(hTx.Hash(), txInfo.SenderID)
		return nil, ErrTxInCache
	}
	txmp.metrics.CacheSize.Set(float64(txmp.cache.Size()))

	// Check TTL cache to see if we've recently processed this transaction
	// Only execute TTL cache logic if we're using a real TTL cache (not NOP)
	if c, ok := txmp.duplicateTxsCache.Get(); ok {
		c.Increment(hTx.Hash())
	}

	if len(txInfo.SenderNodeID) == 0 {
		txmp.metrics.NumberOfLocalCheckTx.Add(1)
	}
	res, err := txmp.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: tx})
	if err != nil || !res.IsOK() {
		txmp.metrics.NumberOfFailedCheckTxs.Add(1)
		txmp.metrics.observeCheckTxPriorityDistribution(0, false, txInfo.SenderNodeID, true)
		txmp.cache.Remove(hTx.Hash())
	}
	if err != nil {
		return nil, err
	}
	if !res.IsOK() {
		return res.ResponseCheckTx, nil
	}
	txmp.metrics.NumberOfSuccessfulCheckTxs.Add(1)
	txmp.metrics.observeCheckTxPriorityDistribution(res.Priority, false, txInfo.SenderNodeID, false)

	wtx := &WrappedTx{
		hashedTx:     hTx,
		timestamp:    time.Now().UTC(),
		height:       txmp.height,
		priority:     res.Priority,
		estimatedGas: res.GasEstimated,
		gasWanted:    res.GasWanted,
		peers:        map[uint16]struct{}{txInfo.SenderID: {}},
	}
	if res.IsEVM {
		wtx.evm = utils.Some(evmTx{
			address:         res.EVMSenderAddress,
			seiAddress:      res.SeiSenderAddress,
			nonce:           res.EVMNonce,
			requiredBalance: res.EVMRequiredBalance,
		})
	}
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
	txmp.priorityReservoir.Add(wtx.priority)
	
	if err := txmp.checkTxConstraints(wtx); err != nil {
		// ignore bad transactions
		logger.Info("rejected bad transaction", "priority", wtx.priority, "tx", wtx.Hash(), "post_check_err", err)
		txmp.metrics.FailedTxs.Add(1)
		return nil, err
	}
	
	txmp.txStore.Insert(wtx)
	
	txmp.metrics.InsertedTxs.Add(1)	
	txmp.metrics.TxSizeBytes.Add(float64(wtx.Size()))
	txmp.metrics.Size.Set(float64(txmp.NumTxsNotPending()))
	txmp.metrics.PendingSize.Set(float64(txmp.PendingSize()))
	txmp.metrics.TotalTxsSizeBytes.Set(float64(txmp.TotalTxsBytesSize()))
	
	txmp.notifyTxsAvailable()
	return res.ResponseCheckTx, nil
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
	txmp.mtx.Lock()
	defer txmp.mtx.Unlock()
	for _, wtx := range txmp.txStore.GetAllTxs() {
		txmp.removeTx(wtx.Hash())
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
	totalGasWanted := int64(0)
	totalGasEstimated := int64(0)
	totalSize := int64(0)
	numTxs := uint64(0)
	encounteredGasUnfit := false

	if uint64(txmp.NumTxsNotPending()) < txmp.config.TxNotifyThreshold { //nolint:gosec // NumTxsNotPending returns non-negative value
		// do not reap anything if threshold is not met
		return []types.Tx{}, 0
	}
	var evmTxs []types.Tx
	var nonEvmTxs []types.Tx
	for wtx := range txmp.txStore.IterByPriority() {
		// bytes limit is a hard stop
		if wtx.protoSize > maxBytes-totalSize || numTxs >= maxTxs {
			break	
		}

		// if the tx doesn't have a gas estimate, fallback to gas wanted
		var txGasEstimate int64
		if wtx.estimatedGas >= MinGasEVMTx && wtx.estimatedGas <= wtx.gasWanted {
			txGasEstimate = wtx.estimatedGas
		} else {
			wtx.estimatedGas = wtx.gasWanted
			txGasEstimate = wtx.gasWanted
		}

		limitExceeded := (maxGasWanted - totalGasWanted < wtx.gasWanted) ||
			(maxGasEstimated - totalGasEstimated < txGasEstimate)

		if limitExceeded {
			// skip this unfit-by-gas tx once and attempt to pull up to 10 smaller ones
			if !encounteredGasUnfit && numTxs < MinTxsToPeek {
				encounteredGasUnfit = true
				continue
			}
			break	
		}

		// include tx and update totals
		numTxs += 1
		totalSize += wtx.protoSize
		totalGasWanted += wtx.gasWanted
		totalGasEstimated += txGasEstimate

		if wtx.evm.IsPresent() {
			evmTxs = append(evmTxs, wtx.Tx())
		} else {
			nonEvmTxs = append(nonEvmTxs, wtx.Tx())
		}
		if encounteredGasUnfit && numTxs >= MinTxsToPeek {
			break	
		}
	}

	return append(evmTxs, nonEvmTxs...), totalGasEstimated
}

// RemoveTxs removes the provided transactions from the mempool if present.
func (txmp *TxMempool) PopTxs(l ReapLimits) (types.Txs, int64) {
	txmp.Lock()
	defer txmp.Unlock()
	txs, gasEstimated := txmp.reapTxs(l)
	for _, tx := range txs {
		txmp.removeTx(tx.Hash())
	}
	return txs, gasEstimated
}

// Update iterates over all the transactions provided by the block producer,
// removes them from the cache (if applicable), and removes
// the transactions from the main transaction store and associated indexes.
// If there are transactions remaining in the mempool, we initiate a
// re-CheckTx for them (if applicable), otherwise, we notify the caller more
// transactions are available.
//
// WARNING: callers should almost always pass recheck=false. recheck=true
// re-runs CheckTx on every tx still in the mempool after each block, and
// handleRecheckResult treats a "now pending" response as terminal: it
// evicts the tx and async-re-CheckTx-es it, which lands it back in
// pendingTxs. For chains whose antehandler returns pending for any
// ahead-of-nonce EVM tx (Sei), this evicts perfectly-valid queued txs.
//
// Example. txA (nonce 3), txB (nonce 2), txC (nonce 1) on the same sender.
//
//  1. txA, txB, txC are submitted in this order.
//  2. txA and txB enter pendingTxs (their nonce is ahead of the sender's
//     expected nonce at CheckTx time so the EVM antehandler marks them
//     pending). txC enters the priority index (its nonce matches expected).
//  3. Block 1 reaps and mines txC. The sender's expected nonce becomes 2.
//  4. handlePendingTransactions promotes txA and txB into the priority
//     index. The per-sender evmQueue is now [txB (head), txA (tail)].
//
// From step 5 onwards the recheck flag matters:
//
// recheck=false (correct):
//
//  5. updateReCheckTxs is skipped. The priority index keeps txB and txA.
//  6. Block 2 reaps the whole evmQueue. Both txB and txA mine.
//
// All 3 txs mine in 2 blocks, regardless of how out-of-order they arrived.
//
// recheck=true (broken):
//
//  5. updateReCheckTxs re-runs CheckTx on each tx in the priority index:
//     - txB: nonce 2 == expected 2 → not pending → stays.
//     - txA: nonce 3  > expected 2 → pending again. handleRecheckResult
//     evicts it and async-re-CheckTx-es it, which lands it back in
//     pendingTxs.
//  6. Block 2 reaps txB only (txA is no longer in the priority index).
//     handlePendingTransactions re-promotes txA. txA's nonce now matches
//     expected, so it survives the recheck this time.
//  7. Block 3 mines txA.
//
// All 3 txs take 3 blocks. With many out-of-order sequential nonces from
// one sender, this stalls the chain to 1-tx-per-block-per-sender throughput.
//
// CometBFT's default for ConsensusParams.ABCI.RecheckTx is false. Recheck
// primarily defended against state-dependent invalidation that modern
// chains catch in ProcessProposal/DeliverTx anyway.
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
		// Remove transaction from the mempool, no matter if it succeeded, or not.
		txmp.removeTx(txHash)
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
	}
	txmp.txStore.UpdateHeight(blockHeight)

	// If there any uncommitted transactions left in the mempool, we either
	// initiate re-CheckTx per remaining transaction or notify that remaining
	// transactions are left.
	if recheck {	
		txmp.updateReCheckTxs(ctx)
	}

	txmp.notifyTxsAvailable()
	txmp.metrics.Size.Set(float64(txmp.NumTxsNotPending()))
	txmp.metrics.TotalTxsSizeBytes.Set(float64(txmp.TotalTxsBytesSize()))
	txmp.metrics.PendingSize.Set(float64(txmp.PendingSize()))
	return nil
}

// updateReCheckTxs updates the recheck cursors using the gossipIndex. For
// each transaction, it executes CheckTx. The global callback defined on
// the app will be executed for each transaction after CheckTx is
// executed.
//
// NOTE:
// - The caller must have a write-lock when executing updateReCheckTxs.
func (txmp *TxMempool) updateReCheckTxs(ctx context.Context) {
	logger.Debug(
		"executing re-CheckTx for all remaining transactions",
		"num_txs", txmp.Size(),
		"height", txmp.height,
	)

	for e := txmp.txStore.readyTxs.Front(); e != nil; e = e.Next() {
		wtx := e.Value()
		res, err := txmp.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{
			Tx:   wtx.Tx(),
			Type: abci.CheckTxTypeV2Recheck,
		})
		if err == nil {
			err = res.Err()
		}
		if err != nil {
			// no need in retrying since the tx will be rechecked after the next block
			logger.Debug("failed to execute CheckTx during recheck", "err", err, "hash", wtx.Hash())
			continue
		}
		txmp.metrics.RecheckTimes.Add(1)

		// we will treat a transaction that turns pending in a recheck as invalid and evict it
		if err := txmp.checkTxConstraints(wtx); err != nil || res.Code != abci.CodeTypeOK {
			logger.Debug(
				"existing transaction no longer valid; failed re-CheckTx callback",
				"priority", wtx.priority,
				"tx", wtx.Hash(),
				"err", err,
				"code", res.Code,
			)
			txmp.removeTx(wtx.Hash())
		}

		wtx.priority = res.Priority
		if evm, ok := wtx.evm.Get(); ok {
			evm.requiredBalance = new(big.Int).Set(res.EVMRequiredBalance)
			wtx.evm = utils.Some(evm)
		}	
	}
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
