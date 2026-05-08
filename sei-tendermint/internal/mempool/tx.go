package mempool

import (
	"context"
	"errors"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
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
}

func newHashedTx(tx types.Tx) hashedTx {
	return hashedTx{tx: tx, hash: tx.Hash()}
}

func (ktx *hashedTx) Tx() types.Tx       { return ktx.tx }
func (ktx *hashedTx) Hash() types.TxHash { return ktx.hash }
func (ktx *WrappedTx) Size() int         { return len(ktx.tx) }

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
	gossipEl *clist.CElement[*WrappedTx]

	// removed marks the transaction as removed from the mempool. This is set
	// during RemoveTx and is needed due to the fact that a given existing
	// transaction in the mempool can be evicted when it is simultaneously having
	// a reCheckTx callback executed.
	removed bool

	// evm properties that aid in prioritization
	evm utils.Option[evmTx]
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

type txStoreInner struct {
	byHash    map[types.TxHash]*WrappedTx // primary index
	sizeBytes utils.AtomicSend[int64]
}

// TxStore implements a thread-safe mapping of valid transaction(s).
//
// NOTE:
//   - Concurrent read-only access to a *WrappedTx object is OK. However, mutative
//     access is not allowed. Regardless, it is not expected for the mempool to
//     need mutative access.
type TxStore struct {
	inner     utils.RWMutex[*txStoreInner]
	sizeBytes utils.AtomicRecv[int64]
}

func NewTxStore() *TxStore {
	inner := &txStoreInner{
		byHash:    make(map[types.TxHash]*WrappedTx),
		sizeBytes: utils.NewAtomicSend[int64](0),
	}
	return &TxStore{
		inner:     utils.NewRWMutex(inner),
		sizeBytes: inner.sizeBytes.Subscribe(),
	}
}

// Size returns the total number of transactions in the store.
func (txs *TxStore) Size() int {
	for inner := range txs.inner.RLock() {
		return len(inner.byHash)
	}
	panic("unreachable")
}

// AllTxsBytes returns the total size in bytes of all transactions in the store.
func (txs *TxStore) AllTxsBytes() int64 {
	return txs.sizeBytes.Load()
}

// WaitForTxs waits until the store becomes non-empty.
func (txs *TxStore) WaitForTxs(ctx context.Context) error {
	_, err := txs.sizeBytes.Wait(ctx, func(sizeBytes int64) bool { return sizeBytes > 0 })
	return err
}

// GetAllTxs returns all the transactions currently in the store.
func (txs *TxStore) GetAllTxs() []*WrappedTx {
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
func (txs *TxStore) GetOlderThan(minTime utils.Option[time.Time], minHeight utils.Option[int64]) []*WrappedTx {
	var older []*WrappedTx
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
			if isOlder {
				older = append(older, wtx)
			}
		}
	}
	return older
}

// GetTxByHash returns a *WrappedTx by the transaction's hash.
func (txs *TxStore) GetTxByHash(key types.TxHash) *WrappedTx {
	for inner := range txs.inner.RLock() {
		return inner.byHash[key]
	}
	panic("unreachable")
}

// IsTxRemoved returns true if a transaction by hash is marked as removed and
// false otherwise.
func (txs *TxStore) IsTxRemoved(wtx *WrappedTx) bool {
	for inner := range txs.inner.RLock() {
		// if this instance has already been marked, return true
		if wtx.removed {
			return true
		}
		// otherwise if the same hash exists, return its state
		wtx, ok := inner.byHash[wtx.Hash()]
		if ok {
			return wtx.removed
		}
	}
	// otherwise we haven't seen this tx
	return false
}

// SetTx stores a *WrappedTx by its hash.
func (txs *TxStore) SetTx(wtx *WrappedTx) {
	for inner := range txs.inner.Lock() {
		existing := inner.byHash[wtx.Hash()]
		inner.byHash[wtx.Hash()] = wtx
		if existing == nil {
			inner.sizeBytes.Store(inner.sizeBytes.Load() + int64(wtx.Size()))
		}
	}
}

// RemoveTx removes a *WrappedTx from the transaction store. It deletes all
// indexes of the transaction.
func (txs *TxStore) RemoveTx(wtx *WrappedTx) {
	for inner := range txs.inner.Lock() {
		if _, ok := inner.byHash[wtx.Hash()]; ok {
			delete(inner.byHash, wtx.Hash())
			inner.sizeBytes.Store(inner.sizeBytes.Load() - int64(wtx.Size()))
		}
		wtx.removed = true
	}
}

// TxHasPeer returns true if a transaction by hash has a given peer ID and false
// otherwise. If the transaction does not exist, false is returned.
func (txs *TxStore) TxHasPeer(key types.TxHash, peerID uint16) bool {
	for inner := range txs.inner.RLock() {
		wtx := inner.byHash[key]
		if wtx == nil {
			return false
		}
		_, ok := wtx.peers[peerID]
		return ok
	}
	panic("unreachable")
}

// GetOrSetPeerByTxHash looks up a WrappedTx by transaction hash and adds the
// given peerID to the WrappedTx's set of peers that sent us this transaction.
// We return true if we've already recorded the given peer for this transaction
// and false otherwise. If the transaction does not exist by hash, we return
// (nil, false).
func (txs *TxStore) GetOrSetPeerByTxHash(hash types.TxHash, peerID uint16) (*WrappedTx, bool) {
	for inner := range txs.inner.Lock() {
		wtx := inner.byHash[hash]
		if wtx == nil {
			return nil, false
		}

		if wtx.peers == nil {
			wtx.peers = make(map[uint16]struct{})
		}

		if _, ok := wtx.peers[peerID]; ok {
			return wtx, true
		}

		wtx.peers[peerID] = struct{}{}
		return wtx, false
	}
	panic("unreachable")
}

type PendingTxs struct {
	inner     utils.RWMutex[*pendingTxsInner]
	config    *Config
	sizeBytes atomic.Int64
}

type pendingTxsInner struct {
	txs []*WrappedTx
}

func NewPendingTxs(conf *Config) *PendingTxs {
	return &PendingTxs{
		inner:  utils.NewRWMutex(&pendingTxsInner{}),
		config: conf,
	}
}

func (p *PendingTxs) EvaluatePendingTransactions(
	evaluate func(*WrappedTx) abci.PendingTxCheckerResponse,
) (
	acceptedTxs []*WrappedTx,
	rejectedTxs []*WrappedTx,
) {
	poppedIndices := []int{}
	for inner := range p.inner.Lock() {
		for i := 0; i < len(inner.txs); i++ {
			result := evaluate(inner.txs[i])
			switch result {
			case abci.Accepted:
				acceptedTxs = append(acceptedTxs, inner.txs[i])
				poppedIndices = append(poppedIndices, i)
			case abci.Rejected:
				rejectedTxs = append(rejectedTxs, inner.txs[i])
				poppedIndices = append(poppedIndices, i)
			}
		}
		p.popTxsAtIndices(inner, poppedIndices)
		return
	}
	panic("unreachable")
}

// Assumes the pending tx store is already write-locked.
func (p *PendingTxs) popTxsAtIndices(inner *pendingTxsInner, indices []int) {
	if len(indices) == 0 {
		return
	}
	newTxs := make([]*WrappedTx, 0, max(0, len(inner.txs)-len(indices)))
	start := 0
	for _, idx := range indices {
		if idx <= start-1 {
			panic("indices popped from pending tx store should be sorted without duplicate")
		}
		if idx >= len(inner.txs) {
			panic("indices popped from pending tx store out of range")
		}
		p.sizeBytes.Add(int64(-inner.txs[idx].Size()))
		newTxs = append(newTxs, inner.txs[start:idx]...)
		start = idx + 1
	}
	newTxs = append(newTxs, inner.txs[start:]...)
	inner.txs = newTxs
}

func (p *PendingTxs) Insert(tx *WrappedTx) error {
	for inner := range p.inner.Lock() {
		if len(inner.txs) >= p.config.PendingSize || int64(tx.Size())+p.sizeBytes.Load() > p.config.MaxPendingTxsBytes {
			return errors.New("pending store is full")
		}
		inner.txs = append(inner.txs, tx)
		p.sizeBytes.Add(int64(tx.Size()))
		return nil
	}
	panic("unreachable")
}

func (p *PendingTxs) SizeBytes() int64 { return p.sizeBytes.Load() }

func (p *PendingTxs) Peek(max int) []*WrappedTx {
	for inner := range p.inner.RLock() {
		// priority is fifo
		if max > len(inner.txs) {
			return inner.txs
		}
		return inner.txs[:max]
	}
	panic("unreachable")
}

func (p *PendingTxs) Size() int {
	for inner := range p.inner.RLock() {
		return len(inner.txs)
	}
	panic("unreachable")
}

func (p *PendingTxs) PurgeExpired(blockHeight int64, now time.Time, cb func(wtx *WrappedTx)) {
	for inner := range p.inner.Lock() {
		if len(inner.txs) == 0 {
			return
		}

		// txs retains the ordering of insertion
		if p.config.TTLNumBlocks > 0 {
			idxFirstNotExpiredTx := len(inner.txs)
			for i, ptx := range inner.txs {
				// once found, we can break because these are ordered
				if (blockHeight - ptx.height) <= p.config.TTLNumBlocks {
					idxFirstNotExpiredTx = i
					break
				}
				cb(ptx)
				p.sizeBytes.Add(int64(-ptx.Size()))
			}
			inner.txs = inner.txs[idxFirstNotExpiredTx:]
		}

		if len(inner.txs) == 0 {
			return
		}

		if p.config.TTLDuration > 0 {
			idxFirstNotExpiredTx := len(inner.txs)
			for i, ptx := range inner.txs {
				// once found, we can break because these are ordered
				if now.Sub(ptx.timestamp) <= p.config.TTLDuration {
					idxFirstNotExpiredTx = i
					break
				}
				cb(ptx)
				p.sizeBytes.Add(int64(-ptx.Size()))
			}
			inner.txs = inner.txs[idxFirstNotExpiredTx:]
		}
		return
	}
	panic("unreachable")
}
