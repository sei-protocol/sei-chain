package mempool

import (
	"context"
	"slices"
	"maps"
	"math/big"
	"time"
	"cmp"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/clist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// TxInfo are parameters that get passed when attempting to add a tx to the
// mempool.
type TxInfo struct {
	// SenderID is the internal peer ID used in the mempool to identify the
	// sender, storing two bytes with each transaction instead of 20 bytes for
	// the types.NodeID.
	SenderID uint16

	// SenderNodeID is the actual types.NodeID of the sender.
	SenderNodeID types.NodeID
}

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
	readyEl utils.Option[*clist.CElement[*WrappedTx]] // linked-list element in the gossip index
	evm utils.Option[evmTx] // evm transaction info
}

type evmTx struct {
	address    common.Address
	seiAddress []byte
	nonce      uint64
	// evmRequiredBalance is the sender balance threshold for this EVM tx to become ready.
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
	// list of ready transactions that can be gossiped. 
	readyTxs *clist.CList[*WrappedTx]
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
		readyTxs: clist.New[*WrappedTx](),
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

// GetTxByHash returns a *WrappedTx by the transaction's hash.
func (txs *txStore) GetTxByHash(key types.TxHash) *WrappedTx {
	for inner := range txs.inner.RLock() {
		return inner.byHash[key]
	}
	panic("unreachable")
}

func (txs *txStore) insert(inner *txStoreInner, wtx *WrappedTx) {
	if _,ok := inner.byHash[wtx.Hash()]; ok { return }
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
		an := evmAddrNonce{evm.address,evm.nonce}
		if old,ok := inner.byNonce[an]; ok {
			// If the old tx is ready but the new tx is not, then reject the new tx.
			if old.evm.OrPanic("non-evm tx").nonce < account.nextNonce && account.balance.Cmp(evm.requiredBalance) < 0 {
				return	
			}
			// If the old tx has >= priority, then reject new tx.
			if old.priority >= wtx.priority { return }
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
		wtx.readyEl = utils.Some(txs.readyTxs.PushBack(wtx))
	}
	inner.state.Store(state)	
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
	total := txCounter{}
	for _,wtx := range wtxs {
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

func (txs *txStore) UpdateHeight(now time.Time, blockHeight int64, blockTxs []types.TxHash, recheck bool) {
	minHeight := utils.None[int64]()
	if ttl,ok := txs.config.TTLNumBlocks.Get(); ok && blockHeight > ttl {
		minHeight = utils.Some(blockHeight - ttl)
	}
	minTime := utils.None[time.Time]()
	if d,ok := txs.config.TTLDuration.Get(); ok {
		minTime = utils.Some(now.Add(-d))
	}
	toPrune := func(wtx *WrappedTx) bool {
		if t, ok := minTime.Get(); ok && wtx.timestamp.Before(t) {
			return true
		}
		if h, ok := minHeight.Get(); ok && wtx.height < h {
			return true
		}
		return false
	}
	for inner := range txs.inner.Lock() {
		// Remove included.
		for _, txHash := range blockTxs {
			if wtx,ok := inner.byHash[txHash]; ok {
				delete(inner.byHash,txHash)
				if el,ok := wtx.readyEl.Get(); ok {
					txs.readyTxs.Remove(el)
				}
			}
		}
		// Prune old.
		for txHash, wtx := range inner.byHash {
			if toPrune(wtx) && (!inner.isReady(wtx) || txs.config.RemoveExpiredTxsFromQueue) {
				delete(inner.byHash,txHash)
				if el,ok := wtx.readyEl.Get(); ok {
					txs.readyTxs.Remove(el)
				}
			}
		}
		// TODO: if recheck { ... }
		txs.compact(inner,true)
	}
}
