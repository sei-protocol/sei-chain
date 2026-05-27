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

func (q *queue[I, T]) Prune(i I) (res T, ok bool) {
	for q.first < min(i,q.next) {
		res,ok = q.q[q.first]
		delete(q.q,q.first)
		q.first += 1
	}
	q.first = max(q.first,i)
	q.next = max(q.next,i)
	return
}

type evmAccount struct {
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

type mempool struct {
	gasEstimated uint64
	gasWanted    uint64
	sizeBytes    uint64
	txs [][]byte
	nextPayload utils.Option[*types.Payload]

	nextToProduce types.BlockNumber
	nextToExecute types.BlockNumber
	evmAccounts  map[common.Address]*evmAccount
}

func (m *mempool) buildPayload() {
	if m.nextPayload.IsPresent() {
		return
	}
	// Snapshot evm state.
	for _,acc := range m.evmAccounts {
		acc.nonceByBlock.PushBack(m.nextToProduce,acc.nextNonce)
	}
	m.nextToProduce += 1
	// Construct a payload.
	payload, err := types.PayloadBuilder{
		CreatedAt: time.Now(),
		TotalGas:  uint64(m.gasEstimated), // nolint:gosec // always non-negative
		Txs:       m.txs,
	}.Build()	
	if err != nil {
		// This should never happen: we construct the payload from correctly sized data.
		panic(fmt.Errorf("PayloadBuilder{}.Build(): %w", err))
	}
	m.nextPayload = utils.Some(payload)	
	// Clear the mempool.
	m.txs = nil
	m.gasEstimated = 0
	m.gasWanted = 0
	m.sizeBytes = 0	
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

func (s *State) EvmNextPendingNonce(addr common.Address) uint64 {
	for m := range s.mempool.Lock() {
		if acc,ok := m.evmAccounts[addr]; ok {
			return acc.nextNonce
		}
	}
	return s.app.EvmNonce(addr)
}

var errTooLarge = errors.New("transaction too large")
var errFull = errors.New("mempool is full")

// Blocking insert.
func (s *State) Insert(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTx, error) {
	if uint64(len(tx)) > types.MaxTxsBytesPerBlock {
		return nil, errTooLarge
	}
	resp, err := s.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: tx})
	if err!=nil { return nil, err }
	if !resp.IsOK() { return resp.ResponseCheckTx, nil }
	gasWanted := utils.Clamp[uint64](resp.GasWanted)
	if gasWanted > s.cfg.MaxGasPerBlock { return nil, errTooLarge }
	
	for m,ctrl := range s.mempool.Lock() {
		if err:=ctrl.WaitUntil(ctx, func() bool { return !m.nextPayload.IsPresent() }); err!=nil {
			return nil, err
		}
		if resp.IsEVM {
			addr := resp.EVMSenderAddress
			acc,ok := m.evmAccounts[addr]
			if !ok {
				acc = &evmAccount { nextNonce: s.app.EvmNonce(addr) }
			}
			if acc.nextNonce != resp.EVMNonce {
				return nil, fmt.Errorf("bad nonce: got %v, want %v", resp.EVMNonce, acc.nextNonce)
			}
			acc.nextNonce += 1
			m.evmAccounts[addr] = acc
		}
		// If any limit would be exceeded, then construct a payload.
		ok := uint64(len(m.txs)) + 1 <= s.cfg.maxTxsPerBlock()
		ok = ok && m.sizeBytes + uint64(len(tx)) <= types.MaxTxsBytesPerBlock
		ok = ok && m.gasWanted + gasWanted <= s.cfg.MaxGasPerBlock
		if !ok {
			m.buildPayload()	
			ctrl.Updated()
		}

		// Normalize the gas estimate.
		gasEstimated := resp.GasEstimated
		if gasEstimated < minTxGas || gasEstimated > resp.GasWanted {
			gasEstimated = resp.GasWanted
		}
		m.gasEstimated += utils.Clamp[uint64](gasEstimated)
		m.gasWanted += utils.Clamp[uint64](resp.GasWanted)
		m.sizeBytes += uint64(len(tx))
		m.txs = append(m.txs,tx)
		return resp.ResponseCheckTx,nil
	}
	panic("unreachable")
}
