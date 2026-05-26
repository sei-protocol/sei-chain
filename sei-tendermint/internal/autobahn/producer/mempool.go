package producer
	
import (
	"context"
	"errors"
	"fmt"
	"time"
	"golang.org/x/exp/constraints"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/ethereum/go-ethereum/common"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// queue is a collection of objects of type T, indexed by type I in range [first,next).
// Supports pushing new items to the back and popping items from the front.
type queue[I constraints.Integer, T any] struct {
	q     map[I]T
	first I
	next  I
}

func newQueue[I ~uint64, T any]() *queue[I, T] {
	return &queue[I, T]{q: map[I]T{}, first: 0, next: 0}
}

func (q *queue[I, T]) First() I { return q.first }
func (q *queue[I, T]) Next() I { return q.next }
func (q *queue[I, T]) Get(i I) T { return q.q[i] }
func (q *queue[I, T]) Len() I { return q.next - q.first }

func (q *queue[I, T]) PushBack(i I, t T) {
	if q.next <= i {
		q.q[i] = t
		if q.first == q.next {
			q.first = i	
		}
		q.next = i+1
	}
}

func (q *queue[I, T]) PopFront(i I) (res T, ok bool) {
	for q.first < min(i+1,q.next) {
		res,ok = q.q[q.first]
		delete(q.q,q.first)
		q.first += 1
	}
	q.next = max(i+1,q.next)
	return
}

type extTx struct {
	tx           tmtypes.Tx
	hash         tmtypes.TxHash
	gasEstimated int64
	gasWanted    int64
}

type evmAccount struct {
	// List of the txs ready to be sequenced.
	readyTxs []*extTx
	// nonce that the account will have after the readyTxs are executed.
	nextNonce uint64
	// Nonces that this account is expected to be at after executing the given block.
	// Since autobahn has asynchronous execution, there is no guarantee that the account nonce will
	// be incremented at the time of constructing the lane block.
	// On the other hand we need to be able to sequence many account txs before the first one is executed.
	// To achieve that we track the expected per-account nonces after each block of the local lane.
	// If after execution the nonce is below the expectation, it means that execution failed and the
	// same will happen with all the subsequent txs (because of the nonce gap). In such a case we 
	// drop whole account from the mempool, because user needs to submit the txs again.
	nonceByBlock queue[types.BlockNumber,uint64]
}

func (a *evmAccount) IsEmpty() bool {
	return len(a.readyTxs)==0 && a.nonceByBlock.Len()==0
}

type mempoolInner struct {
	count uint64
	cosmosTxs []*extTx
	evmAccounts map[common.Address]*evmAccount
}

// (addr,nonce) -> tx
// tracking of what is in progress
// on startup
// * read data.State and avail.State from executed until the end (even across gaps)
// * parse all of these transactions
// * consider only our lane blocks (we are guaranteed to have all of our lane blocks)
// * we are interested only in evm nonces - ignore txs with nonces after a gap
// every time execution progresses
// * we check if nonces progressed as expected.
// * if not - just drop all the non-included txs of the given address
// for testnet
// * accept only ready txs
// * don't drop ready txs (unless some tx was unexpectedly dropped)
// * drop over capacity.
// TODO: make sure that we query nonce at height > expected height
//   this way our check will be an approximation from below
type Mempool struct {
	app     *proxy.Proxy
	cfg *Config
	maxCount uint64
	maxBytes uint64
	inner utils.Watch[*mempoolInner]	
}

func NewMempool(cfg *Config, app *proxy.Proxy) *Mempool {
	return &Mempool {
		app: app,
		cfg: cfg,
		inner: utils.NewWatch(&mempoolInner {
			evmAccounts: map[common.Address]*evmAccount{},
		}),
	}
}

type ReapLimits struct {
	MaxTxs          uint64
	MaxBytes        uint64
	MaxGasWanted    uint64
}

func (m *Mempool) EvmNextPendingNonce(addr common.Address) uint64 {
	for inner := range m.inner.Lock() {
		if acc,ok := inner.evmAccounts[addr]; ok {
			return acc.nextNonce
		}
	}
	return m.app.EvmNonce(addr)
}

var errTooLarge = errors.New("transaction too large")
var errFull = errors.New("mempool is full")

func (m *Mempool) Insert(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTx, error) {
	if uint64(len(tx)) > types.MaxTxsBytesPerBlock {
		return nil, errTooLarge
	}
	resp, err := m.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: tx})
	if err!=nil { return nil, err }
	if !resp.IsOK() { return resp.ResponseCheckTx, nil }	
	etx := &extTx {
		tx: tx,
		hash: tx.Hash(), 
		gasEstimated: resp.GasEstimated,
		gasWanted: resp.GasWanted,
	}
	if etx.gasEstimated < minTxGas || etx.gasEstimated > etx.gasWanted {
		etx.gasEstimated = etx.gasWanted
	}
	for inner,ctrl := range m.inner.Lock() {
		if inner.count+1 > m.cfg.MempoolSize { return nil, errFull }
		// TODO: byte capacity
		if resp.IsEVM {
			addr := resp.EVMSenderAddress
			acc,ok := inner.evmAccounts[addr]
			if !ok {
				acc = &evmAccount { nextNonce: m.app.EvmNonce(addr) }
			}
			if acc.nextNonce != resp.EVMNonce {
				return nil, fmt.Errorf("bad nonce: got %v, want %v", resp.EVMNonce, acc.nextNonce)
			}
			acc.readyTxs = append(acc.readyTxs,etx)
			acc.nextNonce += 1
			inner.evmAccounts[addr] = acc
		} else {
			inner.cosmosTxs = append(inner.cosmosTxs,etx)
		}
		inner.count += 1
		ctrl.Updated()
		return resp.ResponseCheckTx,nil
	}
	panic("unreachable")
}

// Reaps a non-empty set of ready txs for constructing block n.
func (m *Mempool) ReapTxs(ctx context.Context, n types.BlockNumber) (*types.Payload, error) {
	limits := ReapLimits{
		MaxTxs:          m.cfg.maxTxsPerBlock(),
		MaxBytes:        types.MaxTxsBytesPerBlock,
		MaxGasWanted:    m.cfg.MaxGasPerBlock,
	}
	for inner,ctrl := range m.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return inner.count > 0 }); err!=nil { return nil,err }
	}
	payloadTxs := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		payloadTxs = append(payloadTxs, tx)
	}
	payload, err := types.PayloadBuilder{
		CreatedAt: time.Now(),
		TotalGas:  uint64(gasEstimated), // nolint:gosec // always non-negative
		Txs:       payloadTxs,
	}.Build()
	if err != nil {
		// This should never happen: we construct the payload from correctly sized data.
		panic(fmt.Errorf("PayloadBuilder{}.Build(): %w", err))
	}
	return payload, nil
}

func (m *Mempool) MarkExecuted(n types.BlockNumber) {
	for inner := range m.inner.Lock() {
		for addr,acc := range inner.evmAccounts {
			if wantMin,ok := acc.nonceByBlock.PopFront(n); acc.IsEmpty() || (ok && m.app.EvmNonce(addr) < wantMin) {
				delete(inner.evmAccounts,addr)
			}
		}
	}
}
