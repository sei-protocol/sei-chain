package mempool

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/clist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/reservoir"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var errDuplicateTx = errors.New("duplicate tx")
var errOldNonce = errors.New("nonce too old")
var errSameNonce = errors.New("tx with this nonce already in mempool")
var errMempoolFull = errors.New("mempool full")

type evmAddrNonce struct {
	Address common.Address
	Nonce   uint64
}

// hashedTx is used to avoid recomputing values derived from tx.
type hashedTx struct {
	tx types.Tx
	// derived
	hash      types.TxHash
	protoSize int64
}

func newHashedTx(tx types.Tx) hashedTx {
	return hashedTx{tx: tx, hash: tx.Hash(),
		protoSize: types.ComputeProtoSizeForTxs([]types.Tx{tx}),
	}
}

func (ktx *hashedTx) Tx() types.Tx       { return ktx.tx }
func (ktx *hashedTx) Hash() types.TxHash { return ktx.hash }
func (ktx *hashedTx) Size() uint64       { return uint64(len(ktx.tx)) }

// WrappedTx defines a wrapper around a raw transaction with additional metadata
// that is used for indexing.
type WrappedTx struct {
	hashedTx
	height       int64               // height defines the height at which the transaction was validated at
	gasWanted    int64               // gasWanted defines the amount of gas the transaction sender requires
	estimatedGas int64               // estimatedGas defines the amount of gas that the transaction is estimated to use
	priority     int64               // ResponseCheckTx.priority
	timestamp    time.Time           // time at which the transaction was received
	evm          utils.Option[evmTx] // evm transaction info

	readyEl utils.Option[*clist.CElement[types.Tx]]
}

func (wtx *WrappedTx) check(c TxConstraints) error {
	if wtx.gasWanted < 0 {
		return fmt.Errorf("negative gas wanted: %d", wtx.gasWanted)
	}
	if c.MaxGas >= 0 && wtx.gasWanted > c.MaxGas {
		return fmt.Errorf("gas wanted exceeds max gas: gas wanted %d is greater than max gas %d", wtx.gasWanted, c.MaxGas)
	}
	return nil
}

type evmTx struct {
	address    common.Address
	seiAddress []byte
	hash       common.Hash
	nonce      uint64
	// requiredBalance is the sender balance threshold for this EVM tx to become ready.
	requiredBalance *big.Int
}

// IsBefore returns true if the WrappedTx is before the given WrappedTx
// this applies to EVM transactions only
func (wtx *WrappedTx) EVMNonce() uint64 {
	if evm, ok := wtx.evm.Get(); ok {
		return evm.nonce
	}
	return 0
}

type evmAccount struct {
	balance    *big.Int
	firstNonce uint64
	nextNonce  uint64
}

type txCounter struct {
	count int
	bytes uint64
}

func (c *txCounter) Inc(bytes uint64) {
	c.count += 1
	c.bytes += bytes
}

func (c *txCounter) Dec(bytes uint64) {
	c.count -= 1
	c.bytes -= bytes
}

type txStoreState struct {
	ready txCounter
	total txCounter
}

// Partial order.
func (c *txCounter) LessEqual(b *txCounter) bool {
	return c.count <= b.count && c.bytes <= b.bytes
}

func (s txStoreState) PendingBytes() uint64 { return s.total.bytes - s.ready.bytes }
func (s txStoreState) PendingCount() int    { return s.total.count - s.ready.count }

type txStoreInner struct {
	byHash    map[types.TxHash]*WrappedTx
	byEvmHash map[common.Hash]*WrappedTx
	byNonce   map[evmAddrNonce]*WrappedTx
	accounts  map[common.Address]*evmAccount

	softLimit txCounter
	hardLimit txCounter
	state     utils.AtomicSend[txStoreState]

	// Snapshot of the mempool state: transactions in inclusion order.
	// Recomputed every time inInclusionOrder is called.
	snapshot types.Txs
	// Cache of already seen txs, reducess pressure on app.
	// It is a superset of transactions in txStore.
	// * successfully inserted transactions are automatically added to cache.
	// * txs which fail Insert() are NOT added to cache and can be reattempted later.
	// * invalid transactions can be recorded via CachePush.
	// * txs dropped due to pruning are removed from cache.
	// * txs successfully executed are kept in cache to avoid reinsert
	// * txs failed execution are eligible to be reexecuted once (iff !config.KeepInvalidTxsInCache).
	cache *lruCache[types.TxHash,struct{}]
	// Tracks transactions which already failed execution once
	// but are eligible for reexecution (not added yet to cache)
	failedTxs *lruCache[types.TxHash,struct{}]
}

// Properties:
//   - tx is ready if all txs with lower nonces are ready or executed AND
//     balance >= tx.requiredBalance
//   - we keep at most 1 tx per nonce
//   - we prefer ready tx to pending tx (then tx with the higher priority) for the same nonce
//   - we don't store txs below account nonce.
//   - account nonces are evaluated once per height
//   - we keep at least capacity and up to 2*capacity txs
//   - we reap by highest prio, while respecting nonces.
//   - non-evm txs are always ready
type txStore struct {
	config  *Config
	app     *proxy.Proxy
	metrics *Metrics

	inner utils.RWMutex[*txStoreInner]
	state utils.AtomicRecv[txStoreState]
	// List of transactions that were ready now OR at some point in the past.
	// It is used for gossip and has to be stable - we cannot afford removing and reinserting transactions to this list,
	// because it would cause them to be regossiped.
	readyTxs *clist.CList[types.Tx]
	// Sampler of priorites of all inserted READY txs.
	// Used by TxMempool to damp re-gossiping of transactions.
	priorityReservoir *reservoir.Sampler[int64]
}

func NewTxStore(cfg *Config, app *proxy.Proxy, metrics *Metrics) *txStore {
	softLimit := txCounter{count: cfg.Size + cfg.PendingSize, bytes: utils.Clamp[uint64](cfg.MaxTxsBytes + cfg.MaxPendingTxsBytes)}
	hardLimit := txCounter{count: 2 * softLimit.count, bytes: 2 * softLimit.bytes}
	inner := &txStoreInner{
		byHash:    map[types.TxHash]*WrappedTx{},
		byEvmHash: map[common.Hash]*WrappedTx{},
		byNonce:   map[evmAddrNonce]*WrappedTx{},
		accounts:  map[common.Address]*evmAccount{},
		softLimit: softLimit,
		hardLimit: hardLimit,
		state:     utils.NewAtomicSend(txStoreState{}),
		cache:     newLRUCache[types.TxHash,struct{}](cfg.CacheSize),
		failedTxs: newLRUCache[types.TxHash,struct{}](cfg.CacheSize),
	}
	return &txStore{
		config:            cfg,
		app:               app,
		metrics:           metrics,
		inner:             utils.NewRWMutex(inner),
		state:             inner.state.Subscribe(),
		readyTxs:          clist.New[types.Tx](),
		priorityReservoir: reservoir.New[int64](cfg.DropPriorityReservoirSize, cfg.DropPriorityThreshold, nil), // Use non-deterministic RNG
	}
}

func (s *txStore) Clear() {
	for inner := range s.inner.Lock() {
		inner.cache.Reset()
		s.metrics.CacheSize.Set(float64(inner.cache.Size()))
		inner.failedTxs.Reset()
		inner.byHash = map[types.TxHash]*WrappedTx{}
		inner.byEvmHash = map[common.Hash]*WrappedTx{}
		inner.byNonce = map[evmAddrNonce]*WrappedTx{}
		inner.accounts = map[common.Address]*evmAccount{}
		inner.state.Store(txStoreState{})
		s.readyTxs.Clear()
	}
}

// Checks if cache contains a given hash.
func (s *txStore) CacheHas(txHash types.TxHash) bool {
	for inner := range s.inner.RLock() {
		_,ok := inner.cache.Get(txHash)
		return ok
	}
	panic("unreachable")
}

// Pushes a tx to cache, effectively blocking it from being inserted.
func (s *txStore) CachePush(txHash types.TxHash) {
	if s.config.KeepInvalidTxsInCache {
		for inner := range s.inner.Lock() {
			inner.cache.Push(txHash,struct{}{})
			s.metrics.CacheSize.Set(float64(inner.cache.Size()))
		}
	}
}

// Removes a tx from cache.
func (s *txStore) CacheRemove(txHash types.TxHash) {
	for inner := range s.inner.Lock() {
		inner.cache.Remove(txHash)
		s.metrics.CacheSize.Set(float64(inner.cache.Size()))
	}
}

// Size returns the total number of transactions in the store.
func (s *txStore) State() txStoreState { return s.state.Load() }

// Recent snapshot of the mempool.
func (s *txStore) RecentSnapshot() types.Txs {
	for inner := range s.inner.RLock() {
		return inner.snapshot
	}
	panic("unreachable")
}

// WaitForTxs waits until there is >0 ready txs.
func (s *txStore) WaitForTxs(ctx context.Context) error {
	_, err := s.state.Wait(ctx, func(state txStoreState) bool { return state.ready.count > 0 })
	return err
}

// Nonce for the next tx of the given account to insert to mempool.
// It takes into consideration the account nonce at the last executed block
// and all the txs currently queued in the mempool.
func (s *txStore) NextNonce(addr common.Address) uint64 {
	for inner := range s.inner.RLock() {
		if acc, ok := inner.accounts[addr]; ok {
			return acc.nextNonce
		}
	}
	return s.app.EvmNonce(addr)
}

// Returns all ready txs.
func (s *txStore) ReadyTxs() []*WrappedTx {
	var res []*WrappedTx
	for inner := range s.inner.RLock() {
		for _, wtx := range inner.byHash {
			if inner.isReady(wtx) {
				res = append(res, wtx)
			}
		}
	}
	return res
}

func (s *txStore) ByHash(key types.TxHash) (types.Tx, bool) {
	for inner := range s.inner.RLock() {
		if wtx, ok := inner.byHash[key]; ok {
			return wtx.Tx(), true
		}
	}
	return nil, false
}

func (s *txStore) ByEvmHash(key common.Hash) (types.Tx, bool) {
	for inner := range s.inner.RLock() {
		if wtx, ok := inner.byEvmHash[key]; ok {
			return wtx.Tx(), true
		}
	}
	return nil, false
}

func (s *txStore) SafeGetTxsForHashes(txHashes []types.TxHash) (types.Txs, []types.TxHash) {
	got := make([]types.Tx, 0, len(txHashes))
	missing := make([]types.TxHash, 0)
	for inner := range s.inner.RLock() {
		for _, txHash := range txHashes {
			if wtx, ok := inner.byHash[txHash]; ok {
				got = append(got, wtx.Tx())
			} else {
				missing = append(missing, txHash)
			}
		}
	}
	return got, missing
}

func (s *txStore) insert(inner *txStoreInner, wtx *WrappedTx) error {
	if _, ok := inner.byHash[wtx.Hash()]; ok {
		return errDuplicateTx
	}
	state := inner.state.Load()
	if evm, ok := wtx.evm.Get(); ok {
		if _, ok := inner.byEvmHash[evm.hash]; ok {
			return errDuplicateTx
		}
		// Fetch the evm account state.
		account, ok := inner.accounts[evm.address]
		if !ok {
			// TODO(gprusak): consider whether we should move these queries out of the mutex.
			b := s.app.EvmBalance(evm.address, evm.seiAddress)
			n := s.app.EvmNonce(evm.address)
			account = &evmAccount{b, n, n}
			inner.accounts[evm.address] = account
		}
		// Reject transactions with old nonces.
		if evm.nonce < account.firstNonce {
			inner.cache.Push(wtx.Hash(),struct{}{})
			return errOldNonce
		}
		an := evmAddrNonce{evm.address, evm.nonce}
		if old, ok := inner.byNonce[an]; ok {
			oldEvm := old.evm.OrPanic("non-evm tx")
			oldReady := oldEvm.nonce < account.nextNonce
			// If the old tx is ready but the new tx is not, then reject the new tx.
			if oldReady && account.balance.Cmp(evm.requiredBalance) < 0 {
				return errSameNonce
			}
			// If the old tx has >= priority, then reject new tx.
			if old.priority >= wtx.priority {
				return errSameNonce
			}
			// Remove the old transaction.
			inner.cache.Remove(old.Hash()) // evicted txs are not cached
			s.metrics.CacheSize.Set(float64(inner.cache.Size()))
			delete(inner.byHash, old.Hash())
			delete(inner.byEvmHash, oldEvm.hash)
			s.metrics.RemovedTxs.Add(1)
			state.total.Dec(old.Size())
			if el, ok := old.readyEl.Get(); ok {
				s.readyTxs.Remove(el)
			}
			if oldReady {
				state.ready.Dec(old.Size())
				state.ready.Inc(wtx.Size())
				s.priorityReservoir.Add(wtx.priority)
				wtx.readyEl = utils.Some(s.readyTxs.PushBack(wtx.Tx()))
			}
		}
		state.total.Inc(wtx.Size())
		inner.byEvmHash[evm.hash] = wtx
		inner.byNonce[an] = wtx
		// Update account ready txs.
		for {
			an.Nonce = account.nextNonce
			wtx, ok := inner.byNonce[an]
			if !ok || account.balance.Cmp(wtx.evm.OrPanic("non-evm tx").requiredBalance) < 0 {
				break
			}
			account.nextNonce += 1
			state.ready.Inc(wtx.Size())
			if !wtx.readyEl.IsPresent() {
				s.priorityReservoir.Add(wtx.priority)
				wtx.readyEl = utils.Some(s.readyTxs.PushBack(wtx.Tx()))
			}
		}
	} else {
		// Non-evm txs are automatically ready
		state.total.Inc(wtx.Size())
		state.ready.Inc(wtx.Size())
		if !wtx.readyEl.IsPresent() {
			s.priorityReservoir.Add(wtx.priority)
			wtx.readyEl = utils.Some(s.readyTxs.PushBack(wtx.Tx()))
		}
	}
	inner.byHash[wtx.Hash()] = wtx
	inner.state.Store(state)
	return nil
}

// WARNING: works only if wtx has been already inserted.
func (inner *txStoreInner) isReady(wtx *WrappedTx) bool {
	evm, ok := wtx.evm.Get()
	return !ok || evm.nonce < inner.accounts[evm.address].nextNonce
}

// Sorts transactions in inclusion order. Here we effectively simulate the following:
// * find account with the highest priority lowest nonce ready transaction and pop this transaction
// * repeat until no ready transactions are available
// * then repeat the same but for pending transactions (i.e. again in per-account nonce order, high priority first, just ignoring readiness)
// Cosmos transactions are all considered ready and from different accounts, so only priority is relevant.
func (inner *txStoreInner) inInclusionOrder() []*WrappedTx {
	// Split txs into ready and pending.
	var ready, pending []*WrappedTx
	for _, wtx := range inner.byHash {
		if inner.isReady(wtx) {
			ready = append(ready, wtx)
		} else {
			pending = append(pending, wtx)
		}
	}
	// To achieve the desired txs order, we assign a new priority to each transaction:
	//   new priority of tx = min over old priorities of all txs with lower or equal nonces of this account
	// If we just sort all txs by (new priority, nonce) we will obtain the desired ordering.
	// To compute the new priority we first sort all txs by nonce.
	// We use accPrio to accumulate min priority of all txs of each account occurred so far.
	accPrio := make(map[common.Address]int64, len(inner.accounts))
	for _, txs := range utils.Slice(ready, pending) {
		slices.SortFunc(txs, func(a, b *WrappedTx) int { return cmp.Compare(a.EVMNonce(), b.EVMNonce()) })
		txPrio := make(map[*WrappedTx]int64, len(txs))
		for _, tx := range txs {
			if evm, ok := tx.evm.Get(); ok {
				if prio, ok := accPrio[evm.address]; !ok || prio > tx.priority {
					accPrio[evm.address] = tx.priority
				}
				txPrio[tx] = accPrio[evm.address]
			} else {
				txPrio[tx] = tx.priority
			}
		}
		// Stable sort by capped priority - it preserves the nonce ordering.
		slices.SortStableFunc(txs, func(a, b *WrappedTx) int { return -cmp.Compare(txPrio[a], txPrio[b]) })
	}
	res := append(ready, pending...)
	// Update the snapshot.
	inner.snapshot = make(types.Txs, len(res))
	for i := range inner.snapshot {
		inner.snapshot[i] = res[i].Tx()
	}
	return res
}

// Inserts a new transaction to txStore.
// txStore takes ownership of wtx.
func (s *txStore) Insert(wtx *WrappedTx) error {
	for inner := range s.inner.Lock() {
		if err := s.insert(inner, wtx); err != nil {
			return err
		}
		if total := inner.state.Load().total; !total.LessEqual(&inner.hardLimit) {
			start := time.Now()
			s.compact(inner, false)
			otelMetrics.compactTotal.Add(context.Background(), 1, triggerInsertOverflowAttr)
			otelMetrics.compactDurationSeconds.Record(context.Background(), time.Since(start).Seconds())
			if _, ok := inner.byHash[wtx.Hash()]; !ok {
				return errMempoolFull
			}
		}
		// Cache is a superset of byHash.
		inner.cache.Push(wtx.Hash(),struct{}{})
		s.metrics.CacheSize.Set(float64(inner.cache.Size()))
	}
	return nil
}

// O(m log m), prunes transactions above softLimit and recomputes all the indices.
func (s *txStore) compact(inner *txStoreInner, clearAccounts bool) {
	// Order all txs by priority.
	wtxs := inner.inInclusionOrder()
	// Reset internal state.
	inner.state.Store(txStoreState{})
	inner.byHash = map[types.TxHash]*WrappedTx{}
	inner.byEvmHash = map[common.Hash]*WrappedTx{}
	inner.byNonce = map[evmAddrNonce]*WrappedTx{}
	if clearAccounts {
		inner.accounts = map[common.Address]*evmAccount{}
	}
	for _, account := range inner.accounts {
		account.nextNonce = account.firstNonce
	}
	for _, wtx := range wtxs {
		total := inner.state.Load().total
		total.Inc(wtx.Size())
		limitOk := total.LessEqual(&inner.softLimit)
		// NOTE: insertion is lazily evaluated here.
		if !limitOk || s.insert(inner, wtx) != nil {
			// NOTE: evicted txs are not cached unconditionally
			if !limitOk || !s.config.KeepInvalidTxsInCache {
				inner.cache.Remove(wtx.Hash())
			}
			s.metrics.RemovedTxs.Add(1)
			s.metrics.EvictedTxs.Add(1)
			if el, ok := wtx.readyEl.Get(); ok {
				s.readyTxs.Remove(el)
			}
		}
	}
	s.metrics.CacheSize.Set(float64(inner.cache.Size()))
}

type updateSpec struct {
	Now           time.Time
	Height        int64
	TxResults     map[types.TxHash]bool // true - success, false - failed, missing - not executed
	Constraints   TxConstraints
	NewPriorities map[types.TxHash]int64
	InvalidTxs    map[types.TxHash]bool
}

func (s *txStore) Update(spec updateSpec) {
	minHeight := utils.None[int64]()
	if ttl, ok := s.config.TTLNumBlocks.Get(); ok && spec.Height > ttl {
		minHeight = utils.Some(spec.Height - ttl)
	}
	minTime := utils.None[time.Time]()
	if d, ok := s.config.TTLDuration.Get(); ok {
		minTime = utils.Some(spec.Now.Add(-d))
	}
	isExpired := func(wtx *WrappedTx) bool {
		if t, ok := minTime.Get(); ok && wtx.timestamp.Before(t) {
			return true
		}
		if h, ok := minHeight.Get(); ok && wtx.height < h {
			return true
		}
		return false
	}
	for inner := range s.inner.Lock() {
		// Insert all executed txs into cache.
		for hash := range spec.TxResults {
			inner.cache.Push(hash,struct{}{})
		}
		for txHash, wtx := range inner.byHash {
			expired := isExpired(wtx)
			if expired {
				s.metrics.ExpiredTxs.Add(1)
			}
			invalid := spec.InvalidTxs[wtx.Hash()] || wtx.check(spec.Constraints) != nil
			success, executed := spec.TxResults[wtx.Hash()]
			remove := invalid || executed || (expired && (s.config.RemoveExpiredTxsFromQueue || !inner.isReady(wtx)))
			if remove {
				// KeepInvalidTxsInCache decides whether we give just 1 chance to each inserted transaction.
				// In particular expired transactions caching depends on it.
				// If not set, we just cache executed transactions (and txs invalidated pre-insertion)
				if !s.config.KeepInvalidTxsInCache {
					// Cleanup the cache.
					// We keep executed txs in cache, unless they failed
					// in which case we give them a second attempt.
					// NOTE: failedTxs.Push is executed lazily.
					if !executed || (!success && inner.failedTxs.Push(txHash,struct{}{})) {
						inner.cache.Remove(txHash)
					} else {
						inner.failedTxs.Remove(txHash)
					}
				}
				delete(inner.byHash, txHash)
				s.metrics.RemovedTxs.Add(1)
				if el, ok := wtx.readyEl.Get(); ok {
					s.readyTxs.Remove(el)
				}
			} else if newPriority, ok := spec.NewPriorities[wtx.Hash()]; ok {
				wtx.priority = newPriority
			}
		}
		start := time.Now()
		s.compact(inner, true)
		otelMetrics.compactTotal.Add(context.Background(), 1, triggerUpdateAttr)
		otelMetrics.compactDurationSeconds.Record(context.Background(), time.Since(start).Seconds())
	}
}

type ReapLimits struct {
	MaxTxs          utils.Option[uint64]
	MaxBytes        utils.Option[int64] // Max total bytes in proto representation.
	MaxGasWanted    utils.Option[int64]
	MaxGasEstimated utils.Option[int64]
}

// Reap returns a list of transactions within the provided tx,
// byte, and gas constraints together with the total estimated gas for the
// returned transactions. Reaped txs are removed iff remove == true.
// O(m log m) where m is the size of the txStore.
func (s *txStore) Reap(l ReapLimits, remove bool) (types.Txs, int64) {
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

	var wtxs []*WrappedTx
	for inner := range s.inner.Lock() {
		if uint64(inner.state.Load().ready.count) >= s.config.TxNotifyThreshold { //nolint:gosec // count is non-negative
			for _, wtx := range inner.inInclusionOrder() {
				if uint64(len(wtxs)) >= maxTxs || !inner.isReady(wtx) {
					break
				}
				if maxBytes-totalSize < wtx.protoSize {
					break
				}
				if maxGasWanted-totalGasWanted < wtx.gasWanted {
					break
				}
				if maxGasEstimated-totalGasEstimated < wtx.estimatedGas {
					break
				}
				// include tx and update totals
				totalSize += wtx.protoSize
				totalGasWanted += wtx.gasWanted
				totalGasEstimated += wtx.estimatedGas
				wtxs = append(wtxs, wtx)
			}
		}
		if remove {
			for _, wtx := range wtxs {
				delete(inner.byHash, wtx.Hash())
				s.metrics.RemovedTxs.Add(1)
				if el, ok := wtx.readyEl.Get(); ok {
					s.readyTxs.Remove(el)
				}
			}
			start := time.Now()
			s.compact(inner, false)
			otelMetrics.compactTotal.Add(context.Background(), 1, triggerReapAttr)
			otelMetrics.compactDurationSeconds.Record(context.Background(), time.Since(start).Seconds())
		}
	}

	// EVM txs go first.
	var evmTxs, nonEvmTxs types.Txs
	for _, wtx := range wtxs {
		if wtx.evm.IsPresent() {
			evmTxs = append(evmTxs, wtx.Tx())
		} else {
			nonEvmTxs = append(nonEvmTxs, wtx.Tx())
		}
	}
	return append(evmTxs, nonEvmTxs...), totalGasEstimated
}
