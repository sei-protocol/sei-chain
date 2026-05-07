package mempool

import (
	"context"
	"iter"
	"math/big"
	"time"

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
func (ktx *hashedTx) Size() int         { return len(ktx.tx) }

// WrappedTx defines a wrapper around a raw transaction with additional metadata
// that is used for indexing.
type WrappedTx struct {
	// hashedTx represents the raw binary transaction data and its memoized hash.
	hashedTx

	// height defines the height at which the transaction was validated at
	height int64

	// gasWanted defines the amount of gas the transaction sender requires
	gasWanted int64

	// estimatedGas defines the amount of gas that the transaction is estimated to use
	estimatedGas int64

	// priority defines the transaction's priority as specified by the application
	// in the ResponseCheckTx response.
	priority int64

	// timestamp is the time at which the node first received the transaction from
	// a peer. It is used as a second dimension is prioritizing transactions when
	// two transactions have the same priority.
	timestamp time.Time

	// peers records a mapping of all peers that sent a given transaction
	peers map[uint16]struct{}

	// heapIndex defines the index of the item in the heap
	heapIndex int

	// gossipEl references the linked-list element in the gossip index
	readyEl utils.Option[*clist.CElement[*WrappedTx]]

	// removed marks the transaction as removed from the mempool. This is set
	// during RemoveTx and is needed due to the fact that a given existing
	// transaction in the mempool can be evicted when it is simultaneously having
	// a reCheckTx callback executed.
	removed bool

	// evm properties that aid in prioritization
	evm utils.Option[evmTx]
}

type evmTx struct {
	address common.Address
	nonce   uint64
	// evmRequiredBalance is the sender balance threshold for this EVM tx to become active.
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
	nonce uint64
	nextReadyNonce uint64
	byNonce map[uint64]*WrappedTx
}

type txStoreState struct {
	readyCount int
	readyBytes uint64
	pendingCount int
	pendingBytes uint64
}

type txStoreV2Inner struct {
	byHash  map[types.TxHash]*WrappedTx
	accounts map[common.Address]*evmAccount
	
	state utils.AtomicSend[txStoreState]
}

type txStoreV2 struct {
	config *Config
	proxy *proxy.Proxy
	inner utils.RWMutex[*txStoreV2Inner]
	state utils.AtomicRecv[txStoreState]
	// gossipIndex defines the gossiping index of valid transactions via a
	// thread-safe linked-list. We also use the gossip index as a cursor for
	// rechecking transactions already in the mempool.
	readyTxs *clist.CList[*WrappedTx]
}

func NewTxStore() *txStoreV2 {
	inner := &txStoreV2Inner{
		byHash: map[types.TxHash]*WrappedTx{},
		accounts: map[common.Address]*evmAccount{},
		state: utils.NewAtomicSend(txStoreState{}),
	}
	return &txStoreV2{
		inner: utils.NewRWMutex(inner),
		readyTxs: clist.New[*WrappedTx](),
		state: inner.state.Subscribe(),
	}
}

// Size returns the total number of transactions in the store.
func (txs *txStoreV2) Size() int { return txs.state.Load().readyCount }

// AllTxsBytes returns the total size in bytes of all transactions in the store.
func (txs *txStoreV2) AllTxsBytes() uint64 { return txs.state.Load().readyBytes }
func (txs *txStoreV2) TotalBytes() uint64 {
	state := txs.state.Load()
	return state.pendingBytes + state.readyBytes
}

// WaitForTxs waits until the store becomes non-empty.
func (txs *txStoreV2) WaitForTxs(ctx context.Context) error {
	_, err := txs.state.Wait(ctx, func(s txStoreState) bool { return s.readyCount > 0 })
	return err
}

// GetAllTxs returns all the transactions currently in the store.
func (txs *txStoreV2) GetAllTxs() []*WrappedTx {
	for inner := range txs.inner.RLock() {
		wTxs := make([]*WrappedTx, len(inner.byHash))
		i := 0
		for _, wtx := range inner.byHash {
			wTxs[i] = wtx
			i++
		}
		return wTxs
	}
	panic("unreachable")
}

// GetOlderThan have older timestamp than minTime OR lower height than minHeight.
func (txs *txStoreV2) PruneOlderThan(minTime utils.Option[time.Time], minHeight utils.Option[int64]) {
	for inner := range txs.inner.Lock() {
		for _, wtx := range inner.byHash {
			isOlder := func() bool {
				if t, ok := minTime.Get(); ok && wtx.timestamp.Before(t) {
					return true
				}
				if h, ok := minHeight.Get(); ok && wtx.height < h {
					return true
				}
				return false
			}()
			if isOlder && (pending || txs.config.RemoveExpiredTxsFromQueue) {
				// TODO: remove
			}
		}
	}
}

// GetTxByHash returns a *WrappedTx by the transaction's hash.
func (txs *txStoreV2) GetTxByHash(key types.TxHash) *WrappedTx {
	for inner := range txs.inner.RLock() {
		return inner.byHash[key]
	}
	panic("unreachable")
}

// SetTx stores a *WrappedTx by its hash.
func (txs *txStoreV2) Insert(wtx *WrappedTx) {
	for inner := range txs.inner.Lock() {
		if _,ok := inner.byHash[wtx.Hash()]; ok { return }
		if evm,ok := wtx.evm.Get(); ok {
			account,ok := inner.accounts[evm.address]
			if !ok {
				account = &evmAccount{
					nonce: txs.proxy.EvmNonce(evm.address),
					balance: txs.proxy.EvmBalance(evm.address),
					byNonce:map[uint64]*WrappedTx{},
				}
				account.nextReadyNonce = account.nonce
				inner.accounts[evm.address] = account
			}
			if old,ok := account.byNonce[evm.nonce]; ok {
				if old.priority >= wtx.priority { return }
				inner.Remove(old.Hash())
			}
			account.byNonce[evm.nonce] = wtx
			// TODO: recompute "ready" txs. 
		}
		// TODO: if readySize exceeds limit, remove lowest prio READY txs
		// in O(n log n) - txs with higher nonces may become pending
		inner.byHash[wtx.Hash()] = wtx
		wtx.readyEl = utils.Some(txs.readyTxs.PushBack(wtx))
		state := inner.state.Load()
		state.readyCount += 1
		state.readyBytes += uint64(wtx.Size())
		inner.state.Store(state)
		// TODO: handle pending computation
		// TODO: enforce max-pending limit
	}
}

// RemoveTx removes a *WrappedTx from the transaction store. It deletes all
// indexes of the transaction.
func (txs *txStoreV2) Remove(txHash types.TxHash) bool {
	for inner := range txs.inner.Lock() {
		wtx, ok := inner.byHash[txHash]
		if !ok { return false }
		wtx.removed = true
		delete(inner.byHash, txHash)
		if evm,ok := wtx.evm.Get(); ok {
			delete(inner.accounts[evm.address].byNonce, evm.nonce)
		}
		state := inner.state.Load()
		state.readyCount -= 1
		state.readyBytes -= uint64(wtx.Size())
		inner.state.Store(state)
		if el,ok := wtx.readyEl.Get(); ok {
			txs.readyTxs.Remove(el)
		}
	}
	return true
}

// TxHasPeer returns true if a transaction by hash has a given peer ID and false
// otherwise. If the transaction does not exist, false is returned.
func (txs *txStoreV2) TxHasPeer(txHash types.TxHash, peerID uint16) bool {
	for inner := range txs.inner.RLock() {
		if wtx,ok := inner.byHash[txHash]; ok {
			_, ok := wtx.peers[peerID]
			return ok
		}
	}
	return false
}

// GetOrSetPeerByTxHash looks up a WrappedTx by transaction hash and adds the
// given peerID to the WrappedTx's set of peers that sent us this transaction.
// We return true if we've already recorded the given peer for this transaction
// and false otherwise. If the transaction does not exist by hash, we return
// (nil, false).
func (txs *txStoreV2) GetOrSetPeerByTxHash(hash types.TxHash, peerID uint16) (*WrappedTx, bool) {
	for inner := range txs.inner.Lock() {
		if wtx,ok := inner.byHash[hash]; ok {
			if _, ok := wtx.peers[peerID]; ok {
				return wtx, true
			}
			wtx.peers[peerID] = struct{}{}
			return wtx, false
		}
	}
	return nil, false
}

func (txs *txStoreV2) UpdateHeight(blockHeight int64) {
	for inner := range txs.inner.Lock() {
		for addr,account := range inner.accounts {
			// Nonce is required to be monotone. 
			newNonce := max(account.nonce,txs.proxy.EvmNonce(addr))
			n1 := min(newNonce,account.nextReadyNonce)
			for n := account.nonce; n<n1; n += 1 {
				inner.Remove(account.byNonce[n])
			}
			account.balance = txs.proxy.EvmBalance(addr)
			account.nonce = newNonce
			account.nextReadyNonce = max(newNonce,account.nextReadyNonce)
			for {
				wtx,ok := account.byNonce[account.nextReadyNonce]
				if !ok || wtx.evm.OrPanic("unexpected non-evm tx").requiredBalance.Cmp(account.balance) > 0 { break }
				wtx.readyEl = utils.Some(txs.readyTxs.PushBack(wtx))
				account.nextReadyNonce += 1
			}
		}
	}
	// pruning
	minHeight := utils.None[int64]()
	if n := txs.config.TTLNumBlocks; n > 0 && blockHeight > n {
		minHeight = utils.Some(blockHeight - n)
	}
	minTime := utils.None[time.Time]()
	if d := txs.config.TTLDuration; d > 0 {
		minTime = utils.Some(time.Now().Add(-d))
	}
	txs.PruneOlderThan(minTime, minHeight)
	panic("unreachable")
}

func (txs *txStoreV2) PendingBytes() uint64 { return txs.state.Load().pendingBytes }
func (txs *txStoreV2) PendingSize() int { return txs.state.Load().pendingCount }

// Priority is (max priority, min timestamp)
func (txs *txStoreV2) IterByPriority() iter.Seq[*WrappedTx] {
	panic("unreachable")
}

