package mempool

import (
	"context"
	"slices"
	"maps"
	"math/big"
	"time"
	"cmp"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/clist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type hashedTx struct {
	tx   types.Tx
	hash types.TxHash
	protoSize int64
}

func newHashedTx(tx types.Tx) hashedTx {
	return hashedTx{tx: tx, hash: tx.Hash(),
		protoSize: types.ComputeProtoSizeForTxs([]types.Tx{tx}),
	}
}

func (ktx *hashedTx) Tx() types.Tx       { return ktx.tx }
func (ktx *hashedTx) Hash() types.TxHash { return ktx.hash }
func (ktx *hashedTx) Size() uint64         { return uint64(len(ktx.tx)) }

// WrappedTx defines a wrapper around a raw transaction with additional metadata
// that is used for indexing.
type WrappedTx struct {
	hashedTx
	height int64 // height defines the height at which the transaction was validated at
	gasWanted int64 // gasWanted defines the amount of gas the transaction sender requires
	estimatedGas int64 // estimatedGas defines the amount of gas that the transaction is estimated to use
	priority int64 // ResponseCheckTx.priority 
	timestamp time.Time // time at which the transaction was received
	evm utils.Option[evmTx] // evm transaction info
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
	balance *big.Int
	firstNonce uint64
	nextNonce uint64
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
func (s txStoreState) PendingCount() int { return s.total.count - s.ready.count }

type txStoreInner struct {
	byHash  map[types.TxHash]*WrappedTx
	byNonce map[evmAddrNonce]*WrappedTx
	accounts map[common.Address]*evmAccount

	softLimit txCounter
	hardLimit txCounter
	state utils.AtomicSend[txStoreState]
}

type txStore struct {
	config *Config
	app *proxy.Proxy
	inner utils.RWMutex[*txStoreInner]
	state utils.AtomicRecv[txStoreState]
	readyTxs *clist.CList[types.Tx]
}

func NewTxStore(config *Config) *txStore {
	softLimit := txCounter{count:config.Size, bytes: utils.Clamp[uint64](config.MaxTxsBytes)}
	hardLimit := txCounter{count:2*softLimit.count, bytes: 2*softLimit.bytes}
	inner := &txStoreInner{
		byHash: map[types.TxHash]*WrappedTx{},
		accounts: map[common.Address]*evmAccount{},
		softLimit: softLimit,
		hardLimit: hardLimit,
		state: utils.NewAtomicSend(txStoreState{}),
	}
	return &txStore{
		config: config,
		inner: utils.NewRWMutex(inner),
		readyTxs: clist.New[types.Tx](),
		state: inner.state.Subscribe(),
	}
}

// Size returns the total number of transactions in the store.
func (txs *txStore) State() txStoreState { return txs.state.Load() }

// WaitForTxs waits until the store becomes non-empty.
func (txs *txStore) WaitForTxs(ctx context.Context) error {
	_, err := txs.state.Wait(ctx, func(s txStoreState) bool { return s.ready.count > 0 })
	return err
}

func (txs *txStore) NextNonce(addr common.Address) uint64 {
	for inner := range txs.inner.RLock() {
		if acc,ok := inner.accounts[addr]; ok {
			return acc.nextNonce
		}
	}
	return txs.app.EvmNonce(addr)	
}

// GetAllTxs returns all the transactions currently in the store.
func (txs *txStore) GetAllTxs() []*WrappedTx {
	for inner := range txs.inner.RLock() {
		return slices.Collect(maps.Values(inner.byHash))
	}
	panic("unreachable")
}

func (txs *txStore) AllReady() []*WrappedTx {
	var ready []*WrappedTx
	for inner := range txs.inner.RLock() {
		for _,wtx := range inner.byHash {
			if inner.isReady(wtx) {
				ready = append(ready,wtx)
			}
		}
	}
	return ready
}

// GetTxByHash returns a *WrappedTx by the transaction's hash.
func (txs *txStore) ByHash(key types.TxHash) *WrappedTx {
	for inner := range txs.inner.RLock() {
		return inner.byHash[key]
	}
	panic("unreachable")
}

func (txs *txStore) insert(inner *txStoreInner, wtx *WrappedTx) bool {
	if _,ok := inner.byHash[wtx.Hash()]; ok { return false }
	state := inner.state.Load()
	if evm,ok := wtx.evm.Get(); ok {
		// Fetch the evm account state.
		account,ok := inner.accounts[evm.address]
		if !ok {
			// TODO(gprusak): consider whether we should move these queries out of the mutex.
			b := txs.app.EvmBalance(evm.address,evm.seiAddress)
			n := txs.app.EvmNonce(evm.address)
			account = &evmAccount{b,n,n}
			inner.accounts[evm.address] = account	
		}
		// Reject transactions with old nonces.
		if evm.nonce < account.firstNonce {
			return false
		}
		an := evmAddrNonce{evm.address,evm.nonce}
		if old,ok := inner.byNonce[an]; ok {
			// If the old tx is ready but the new tx is not, then reject the new tx.
			if old.evm.OrPanic("non-evm tx").nonce < account.nextNonce && account.balance.Cmp(evm.requiredBalance) < 0 {
				return false
			}
			// If the old tx has >= priority, then reject new tx.
			if old.priority >= wtx.priority { return false }
			// Remove the old transaction.
			delete(inner.byHash,old.Hash())
			if el,ok := wtx.readyEl.Get(); ok {
				txs.readyTxs.Remove(el)
			}
			state.ready.Dec(old.Size())
			state.total.Dec(old.Size())
			state.ready.Inc(wtx.Size())
		}
		inner.byNonce[an] = wtx
		// Update account ready txs.	
		for {
			an.Nonce = account.nextNonce
			wtx,ok := inner.byNonce[an]
			if !ok || account.balance.Cmp(wtx.evm.OrPanic("non-evm tx").requiredBalance) < 0 { break }
			account.nextNonce += 1
			state.ready.Inc(wtx.Size())
		}
	} else {
		// Non-evm txs are automatically ready
		state.ready.Inc(wtx.Size())
	}
	state.total.Inc(wtx.Size())
	inner.byHash[wtx.Hash()] = wtx
	if !wtx.readyEl.IsPresent() {
		wtx.readyEl = utils.Some(txs.readyTxs.PushBack(wtx.Tx()))
	}
	inner.state.Store(state)
	return true
}

// WARNING: works only if wtx has been already inserted. 
func (inner *txStoreInner) isReady(wtx *WrappedTx) bool {
	evm,ok := wtx.evm.Get()
	return !ok || evm.nonce < inner.accounts[evm.address].nextNonce
}

// Sorts transactions in inclusion order. Here we effectively simulate the following:
// * find account with the highest priority lowest nonce ready transaction and pop this transaction
// * repeat until no ready transactions are available
// * then repeat the same but for pending transactions (i.e. again in per-account nonce order, high priority first, just ignoring readiness)
// Cosmos transactions are all considered ready and from different accounts, so only priority is relevant.
func (inner *txStoreInner) inInclusionOrder() []*WrappedTx {
	// Split txs into ready and pending.
	// TODO(gprusak): we can precisely preallocate ready and pending in a single array,
	// based on inner.state.total.count and inner.state.ready.count
	var ready,pending []*WrappedTx
	for _,wtx := range inner.byHash {
		if inner.isReady(wtx) {
			ready = append(ready,wtx)
		} else {
			pending = append(pending,wtx)
		}
	}
	for _,txs := range utils.Slice(ready,pending) {
		// Sort by nonce.
		slices.SortFunc(txs,func(a,b *WrappedTx) int { return cmp.Compare(a.EVMNonce(),b.EVMNonce()) })
		// Cap priority to obtain a linear order of txs per account by nonce.
		// NOTE: this precisely emulates the heap behavior described in this functions docstring.
		accPrio := make(map[common.Address]int64,len(inner.accounts))
		txPrio := make(map[*WrappedTx]int64,len(txs))
		for _,tx := range txs {
			if evm,ok := tx.evm.Get(); ok {
				if prio,ok := accPrio[evm.address]; !ok || prio > tx.priority {
					accPrio[evm.address] = tx.priority
				}
				txPrio[tx] = accPrio[evm.address]
			} else { 
				txPrio[tx] = tx.priority
			}
		}
		// Stable sort by capped priority - it preserves the nonce ordering.
		slices.SortStableFunc(txs,func(a,b *WrappedTx) int { return -cmp.Compare(txPrio[a],txPrio[b]) })
	}
	return append(ready,pending...)
}

// SetTx stores a *WrappedTx by its hash.
func (txs *txStore) Insert(wtx *WrappedTx) {
	for inner := range txs.inner.Lock() {
		txs.insert(inner,wtx)
		if total := inner.state.Load().total; !total.LessEqual(&inner.hardLimit) {
			txs.compact(inner, false)
		}
	}
}

func (txs *txStore) compact(inner *txStoreInner, clearAccounts bool) {
	wtxs := inner.inInclusionOrder()
	inner.state.Store(txStoreState{}) 
	inner.byHash = map[types.TxHash]*WrappedTx{}
	inner.byNonce = map[evmAddrNonce]*WrappedTx{}
	if clearAccounts {
		inner.accounts = map[common.Address]*evmAccount{}
	}
	for _, account := range inner.accounts {
		account.nextNonce = account.firstNonce
	}
	for _,wtx := range wtxs {
		total := inner.state.Load().total
		total.Inc(wtx.Size())
		if total.LessEqual(&inner.softLimit) {
			txs.insert(inner,wtx)
		} else {
			if el,ok := wtx.readyEl.Get(); ok {
				txs.readyTxs.Remove(el)
			}
		}
	}
}

type updateSpec struct {
	Now time.Time
	Height int64
	ToRemove map[types.TxHash]struct{}
	Constraints TxConstraints
	NewPriorities map[types.TxHash]int64
}

func (txs *txStore) Update(spec updateSpec) {
	minHeight := utils.None[int64]()
	if ttl,ok := txs.config.TTLNumBlocks.Get(); ok && spec.Height > ttl {
		minHeight = utils.Some(spec.Height - ttl)
	}
	minTime := utils.None[time.Time]()
	if d,ok := txs.config.TTLDuration.Get(); ok {
		minTime = utils.Some(spec.Now.Add(-d))
	}
	for inner := range txs.inner.Lock() {
		toRemove := func(wtx *WrappedTx) bool {
			if _,ok := spec.ToRemove[wtx.Hash()]; ok {
				return true
			}
			if wtx.check(spec.Constraints) != nil {
				return true
			}
			// Consider expiration.
			if inner.isReady(wtx) && !txs.config.RemoveExpiredTxsFromQueue {
				return false
			}
			if t, ok := minTime.Get(); ok && wtx.timestamp.Before(t) {
				return true
			}
			if h, ok := minHeight.Get(); ok && wtx.height < h {
				return true
			}
			return false
		}
		for txHash, wtx := range inner.byHash {
			if toRemove(wtx) {
				delete(inner.byHash,txHash)
				if el,ok := wtx.readyEl.Get(); ok {
					txs.readyTxs.Remove(el)
				}
			} else if newPriority,ok := spec.NewPriorities[wtx.Hash()]; ok {
				wtx.priority = newPriority
			}
		}
		txs.compact(inner,true)
	}
}

type ReapLimits struct {
	MaxTxs          utils.Option[uint64]
	MaxBytes        utils.Option[int64] // Max total bytes in proto representation.
	MaxGasWanted    utils.Option[int64]
	MaxGasEstimated utils.Option[int64]
}

// ReapTxs returns a list of transactions within the provided tx,
// byte, and gas constraints together with the total estimated gas for the
// returned transactions.
func (txs *txStore) ReapTxs(l ReapLimits) (types.Txs, int64) {
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
	for inner := range txs.inner.Lock() {
		if uint64(inner.state.Load().ready.count) >= txs.config.TxNotifyThreshold {
			for _,wtx := range inner.inInclusionOrder() {
				if wtx.protoSize > maxBytes-totalSize || uint64(len(wtxs)) >= maxTxs {
					break	
				}
				if maxGasWanted - totalGasWanted < wtx.gasWanted {
					break
				}
				if maxGasEstimated - totalGasEstimated < wtx.estimatedGas {
					break
				}
				// include tx and update totals
				totalSize += wtx.protoSize
				totalGasWanted += wtx.gasWanted
				totalGasEstimated += wtx.estimatedGas
				wtxs = append(wtxs, wtx)
			}
		}
	}
	// EVM txs go first.
	var evmTxs,nonEvmTxs types.Txs
	for _,wtx := range wtxs {
		if wtx.evm.IsPresent() {
			evmTxs = append(evmTxs, wtx.Tx())
		} else {
			nonEvmTxs = append(nonEvmTxs, wtx.Tx())
		}
	}
	return append(evmTxs,nonEvmTxs...), totalGasEstimated
}
