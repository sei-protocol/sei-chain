package mempool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/clist"
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

	// time after which transaction is removed from mempool.
	TTLDuration utils.Option[time.Duration]

	// number of blocks after which a transaction is removed from mempool.
	TTLNumBlocks utils.Option[int64]

	// TxNotifyThreshold, if non-zero, defines the minimum number of transactions
	// needed to trigger a notification in mempool's Tx notifier
	TxNotifyThreshold uint64

	PendingSize int

	MaxPendingTxsBytes int64

	// Deprecated: pending TTL is not used and this field has no effect.
	PendingTTLDuration time.Duration

	// Deprecated: pending TTL is not used and this field has no effect.
	PendingTTLNumBlocks int64

	// Whether expired READY transactions should be pruned from mempool (PENDING expired are always prunned)
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
		MaxTxBytes:                1024 * 1024,                 // 1MB
		TTLDuration:               utils.Some(5 * time.Second), // prevent stale txs from filling mempool
		TTLNumBlocks:              utils.Some(int64(10)),       // remove txs after 10 blocks
		TxNotifyThreshold:         0,
		PendingSize:               5000,
		MaxPendingTxsBytes:        1024 * 1024 * 1024, // 1GB
		RemoveExpiredTxsFromQueue: true,
		DropPriorityThreshold:     0.1,
		DropUtilisationThreshold:  1.0,
		DropPriorityReservoirSize: 10_240,
	}
}

type lockMap[K comparable] struct{ inner utils.Mutex[map[K]struct{}] }

func newLockMap[K comparable]() *lockMap[K] {
	return &lockMap[K]{inner: utils.NewMutex(map[K]struct{}{})}
}

func (m *lockMap[K]) Lock(k K) bool {
	for inner := range m.inner.Lock() {
		if _, ok := inner[k]; ok {
			return false
		}
		inner[k] = struct{}{}
	}
	return true
}

func (m *lockMap[K]) Unlock(k K) {
	for inner := range m.inner.Lock() {
		delete(inner, k)
	}
}

// TxMempool defines a prioritized mempool data structure used by the v1 mempool
// reactor. It keeps a thread-safe priority queue of transactions that is used
// when a block proposer constructs a block and a thread-safe linked-list that
// is used to gossip transactions to peers in a FIFO manner.
type TxMempool struct {
	metrics *Metrics
	config  *Config
	app     *proxy.Proxy
	txLocks *lockMap[types.TxHash]

	// txsAvailable fires once for each height when the mempool is not empty
	txsAvailable         chan struct{}
	notifiedTxsAvailable atomic.Bool

	// height defines the last block height process during Update()
	height int64

	// A TTL cache which keeps all txs that we have seen before over the TTL window.
	// Currently, this can be used for tracking whether checkTx is always serving the same tx or not.
	duplicateTxsCache utils.Option[*DuplicateTxCache]

	// txStore defines the main storage of valid transactions. Indexes are built
	// on top of this store.
	txStore *txStore

	// A read/write lock is used to safe guard updates, insertions and deletions
	// from the mempool. A read-lock is implicitly acquired when executing CheckTx,
	// however, a caller must explicitly grab a write-lock via Lock when updating
	// the mempool via Update().
	mtx                  sync.RWMutex
	txConstraintsFetcher TxConstraintsFetcher
}

func (txmp *TxMempool) Size() int                 { return txmp.txStore.State().total.count }
func (txmp *TxMempool) SizeBytes() uint64         { return txmp.txStore.State().ready.bytes }
func (txmp *TxMempool) NumTxsNotPending() int     { return txmp.txStore.State().ready.count }
func (txmp *TxMempool) BytesNotPending() uint64   { return txmp.txStore.State().ready.bytes }
func (txmp *TxMempool) TotalTxsBytesSize() uint64 { return txmp.txStore.State().total.bytes }
func (txmp *TxMempool) PendingSize() int          { return txmp.txStore.State().PendingCount() }
func (txmp *TxMempool) PendingSizeBytes() uint64  { return txmp.txStore.State().PendingBytes() }

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
		txLocks:              newLockMap[types.TxHash](),
		height:               -1,
		metrics:              metrics,
		txStore:              NewTxStore(cfg, app, metrics),
		txConstraintsFetcher: txConstraintsFetcher,
	}

	if cfg.DuplicateTxsCacheSize > 0 {
		txmp.duplicateTxsCache = utils.Some(NewDuplicateTxCache(cfg.DuplicateTxsCacheSize, 1*time.Minute, 0))
	}

	return txmp
}

func (txmp *TxMempool) Config() *Config   { return txmp.config }
func (txmp *TxMempool) App() *proxy.Proxy { return txmp.app }
func (txmp *TxMempool) EvmNextPendingNonce(addr common.Address) uint64 {
	return txmp.txStore.NextNonce(addr)
}

func (txmp *TxMempool) EvmTxByHash(hash common.Hash) (types.Tx, bool) {
	return txmp.txStore.ByEvmHash(hash)
}

// Relatively fresh snapshot of the mempool.
// NOTE: it is NOT the current state of the mempool most of the time.
func (txmp *TxMempool) RecentSnapshot() types.Txs { return txmp.txStore.RecentSnapshot() }

func (txmp *TxMempool) WaitForTxs(ctx context.Context) error {
	return txmp.txStore.WaitForTxs(ctx)
}

// Lock obtains a write-lock on the mempool. A caller must be sure to explicitly
// release the lock when finished.
func (txmp *TxMempool) Lock() { txmp.mtx.Lock() }

// Unlock releases a write-lock on the mempool.
func (txmp *TxMempool) Unlock() { txmp.mtx.Unlock() }

func (txmp *TxMempool) utilisation() float64 {
	return float64(txmp.Size()) / float64(max(txmp.config.Size+txmp.config.PendingSize, 1))
}

// WaitForNextTx waits until the next transaction is available for gossip.
// Returns the next valid transaction to gossip.
func (txmp *TxMempool) WaitForReadyTx(ctx context.Context) (*clist.CElement[types.Tx], error) {
	return txmp.txStore.readyTxs.WaitFront(ctx)
}

// TxsAvailable returns a channel which fires once for every height, and only
// when transactions are available in the mempool. It is thread-safe.
func (txmp *TxMempool) TxsAvailable() <-chan struct{} { return txmp.txsAvailable }

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
func (txmp *TxMempool) CheckTx(ctx context.Context, tx types.Tx) (*abci.ResponseCheckTx, error) {
	txmp.mtx.RLock()
	defer txmp.mtx.RUnlock()

	// Early exit if tx is too large.
	if txSize := len(tx); txSize > txmp.config.MaxTxBytes {
		return nil, fmt.Errorf("%w: max size is %d, but got %d", ErrTxTooLarge, txmp.config.MaxTxBytes, txSize)
	}
	hTx := newHashedTx(tx)

	// Avoid processing same transaction in parallel.
	if !txmp.txLocks.Lock(hTx.Hash()) {
		// ErrTxInCache is returned for backward compatibility.
		return nil, ErrTxInCache
	}
	defer txmp.txLocks.Unlock(hTx.Hash())

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
		txmp.metrics.CheckTxMetDropUtilisationThresholdAt().Add(1)

		hint, err := txmp.app.GetTxPriorityHint(ctx, &abci.RequestGetTxPriorityHintV2{Tx: tx})
		if err != nil {
			txmp.metrics.observeCheckTxPriorityDistribution(0, true, "", true)
			logger.Error("failed to get tx priority hint", "err", err)
			return nil, err
		}
		txmp.metrics.observeCheckTxPriorityDistribution(hint.Priority, true, "", false)

		cutoff, found := txmp.txStore.priorityReservoir.Percentile()
		if found && hint.Priority <= cutoff {
			txmp.metrics.CheckTxDroppedByPriorityHintAt().Add(1)
			return nil, errors.New("priority not high enough for mempool")
		}
	}

	// Check if the tx is known to be bad.
	if txmp.txStore.ShouldReject(hTx.Hash()) {
		return nil, ErrTxInCache
	}

	if c, ok := txmp.duplicateTxsCache.Get(); ok {
		c.Increment(hTx.Hash())
	}
	res, err := txmp.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: tx})
	if err != nil || !res.IsOK() {
		txmp.metrics.NumberOfFailedCheckTxsAt().Add(1)
		txmp.metrics.observeCheckTxPriorityDistribution(0, false, "", true)
	}
	if err != nil {
		return nil, err
	}
	if !res.IsOK() {
		return res.ResponseCheckTx, nil
	}
	txmp.metrics.NumberOfSuccessfulCheckTxsAt().Add(1)
	txmp.metrics.observeCheckTxPriorityDistribution(res.Priority, false, "", false)

	// Normalize the estimate.
	estimatedGas := res.GasEstimated
	if estimatedGas < MinGasEVMTx || estimatedGas > res.GasWanted {
		estimatedGas = res.GasWanted
	}
	wtx := &WrappedTx{
		hashedTx:     hTx,
		timestamp:    time.Now().UTC(),
		height:       txmp.height,
		priority:     res.Priority,
		estimatedGas: estimatedGas,
		gasWanted:    res.GasWanted,
	}
	if res.IsEVM {
		wtx.evm = utils.Some(evmTx{
			address:         res.EVMSenderAddress,
			seiAddress:      res.SeiSenderAddress,
			hash:            res.EVMHash,
			nonce:           res.EVMNonce,
			requiredBalance: res.EVMRequiredBalance,
		})
	}

	if err := wtx.check(constraints); err != nil {
		// ignore bad transactions
		logger.Info("rejected bad transaction", "priority", wtx.priority, "tx", wtx.Hash(), "post_check_err", err)
		txmp.txStore.MarkInvalid(hTx.Hash())
		txmp.metrics.FailedTxsAt().Add(1)
		return nil, err
	}

	if err := txmp.txStore.Insert(wtx); err != nil {
		txmp.metrics.RejectedTxsAt().Add(1)
		return nil, err
	}

	txmp.metrics.InsertedTxsAt().Add(1)
	txmp.metrics.TxSizeBytesAt().Add(int64(wtx.Size()))
	txmp.metrics.SizeAt().Set(int64(txmp.NumTxsNotPending()))
	txmp.metrics.PendingSizeAt().Set(int64(txmp.PendingSize()))
	txmp.metrics.TotalTxsSizeBytesAt().Set(int64(txmp.TotalTxsBytesSize()))

	txmp.notifyTxsAvailable()
	return res.ResponseCheckTx, nil
}

func (txmp *TxMempool) SafeGetTxsForHashes(txHashes []types.TxHash) (types.Txs, []types.TxHash) {
	return txmp.txStore.SafeGetTxsForHashes(txHashes)
}

// Flush empties the mempool.
func (txmp *TxMempool) Flush() {
	txmp.txStore.Clear()
	txmp.metrics.SizeAt().Set(0)
	txmp.metrics.PendingSizeAt().Set(0)
	txmp.metrics.TotalTxsSizeBytesAt().Set(0)
}

// ReapTxs returns a list of transactions within the provided constraints and their total gas estimate.
// The returned list starts with EVM transactions (in priority order),
// followed by non-EVM transactions (in priority order).
// There are 4 types of constraints.
//  1. maxBytes - stops pulling txs from mempool once maxBytes is hit.
//  2. maxGasWanted - stops pulling txs from mempool once total gas wanted exceeds maxGasWanted.
//     Can be set to -1 to be ignored.
//  3. maxGasEstimated - similar to maxGasWanted but will use the estimated gas used for EVM txs
//     while still using gas wanted for cosmos txs. Can be set to -1 to be ignored.
//
// NOTE: Transactions are removed from the mempool iff remove == true.
// Either way, the transactions stay in the LRU cache.
func (txmp *TxMempool) ReapTxs(limits ReapLimits, remove bool) (types.Txs, int64) {
	txs, gasEstimate := txmp.txStore.Reap(limits, remove)
	if remove {
		txmp.metrics.SizeAt().Set(int64(txmp.NumTxsNotPending()))
		txmp.metrics.PendingSizeAt().Set(int64(txmp.PendingSize()))
		txmp.metrics.TotalTxsSizeBytesAt().Set(int64(txmp.TotalTxsBytesSize()))
	}
	return txs, gasEstimate
}

// Update iterates over all the transactions provided by the block producer,
// removes them from the cache (if applicable), and removes
// the transactions from the main transaction store and associated indexes.
// If recheck = true, CheckTx is called on all remaining transactions.
//
// NOTE:
// - The caller must explicitly acquire a write-lock.
func (txmp *TxMempool) Update(
	ctx context.Context,
	blockHeight int64,
	blockTxs types.Txs,
	execTxResult []*abci.ExecTxResult,
	txConstraints TxConstraints,
	recheck bool,
) error {
	if blockHeight <= txmp.height {
		return fmt.Errorf("blockHeight = %v, want > %v", blockHeight, txmp.height)
	}
	txmp.height = blockHeight
	txmp.notifiedTxsAvailable.Store(false)
	txmp.txConstraintsFetcher = func() (TxConstraints, error) {
		return txConstraints, nil
	}

	txResults := map[types.TxHash]bool{}
	for i, tx := range blockTxs {
		txResults[tx.Hash()] = execTxResult[i].Code == abci.CodeTypeOK
	}
	newPriorities := map[types.TxHash]int64{}
	invalidTxs := map[types.TxHash]bool{}
	if recheck {
		for _, wtx := range txmp.txStore.ReadyTxs() {
			if _, ok := txResults[wtx.Hash()]; ok {
				continue
			}
			txmp.metrics.RecheckTimesAt().Add(1)
			res, err := txmp.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{
				Tx:   wtx.Tx(),
				Type: abci.CheckTxTypeV2Recheck,
			})
			if err != nil || !res.IsOK() {
				invalidTxs[wtx.Hash()] = true
			} else {
				// If succeeds, we just care about the new priority.
				newPriorities[wtx.Hash()] = res.Priority
			}
		}
	}
	txmp.txStore.Update(updateSpec{
		Now:           time.Now(),
		Height:        blockHeight,
		TxResults:     txResults,
		NewPriorities: newPriorities,
		InvalidTxs:    invalidTxs,
		Constraints:   txConstraints,
	})
	txmp.notifyTxsAvailable()
	txmp.metrics.SizeAt().Set(int64(txmp.NumTxsNotPending()))
	txmp.metrics.TotalTxsSizeBytesAt().Set(int64(txmp.TotalTxsBytesSize()))
	txmp.metrics.PendingSizeAt().Set(int64(txmp.PendingSize()))
	return nil
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
			txmp.metrics.DuplicateTxMaxOccurrencesAt().Set(int64(maxOccurrence))
			txmp.metrics.DuplicateTxTotalOccurrencesAt().Set(int64(totalOccurrence))
			txmp.metrics.NumberOfDuplicateTxsAt().Set(int64(duplicateCount))
			txmp.metrics.NumberOfNonDuplicateTxsAt().Set(int64(nonDuplicateCount))
		}
	})
}
