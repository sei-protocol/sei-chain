package producer
	
import (
	"context"
	"errors"
	"fmt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/ethereum/go-ethereum/common"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

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
	nonceByBlock map[types.BlockNumber]uint64
}

type blockSpec struct {
	gasEstimated uint64
	gasWanted    uint64
	sizeBytes    uint64
	txs [][]byte
	evmNonces map[common.Address]uint64
}

type mempool struct {
	capacity uint64
	first types.BlockNumber
	next types.BlockNumber
	blocks map[types.BlockNumber]*blockSpec
	nextBlock *blockSpec
	evmNonces map[common.Address]uint64
}

func (m *mempool) IsFull() bool {
	return uint64(m.next-m.first) >= m.capacity && len(m.nextBlock.txs) > 0
}

func (m *mempool) CanPushBlock() bool {
	return uint64(m.next-m.first) < m.capacity && len(m.nextBlock.txs) > 0
}

func (m *mempool) PushBlock() {
	m.blocks[m.next] = m.nextBlock
	m.nextBlock = &blockSpec{
		evmNonces: map[common.Address]uint64{},
	}
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
func (s *State) EvmNextPendingNonce(addr common.Address) uint64 {
	for m := range s.mempool.Lock() {
		if nonce,ok := m.evmNonces[addr]; ok {
			return nonce
		}
	}
	return s.app.EvmNonce(addr)
}

var errTooLarge = errors.New("transaction too large")
var errFull = errors.New("mempool is full")
var errBadNonce = errors.New("bad nonce")

func (s *State) mempoolFirst() types.BlockNumber {
	for m := range s.mempool.Lock() {
		return m.first
	}
	panic("unreachable")
}

func (s *State) pruneMempool(n types.BlockNumber) {
	for m,ctrl := range s.mempool.Lock() {
		if n < m.first { return }
		ctrl.Updated()
		for m.first < min(n,m.next) {
			b := m.blocks[m.first]
			delete(m.blocks,m.first)
			m.first += 1
			for addr,wantNonce := range b.evmNonces {
				if wantNonce == m.evmNonces[addr] {
					// Happy path: all account's txs got executed.
					delete(m.evmNonces,addr)
				} else if gotNonce := s.app.EvmNonce(addr); gotNonce < wantNonce {
					// Some txs have not been executed - reset account tracking.
					// NOTE: app execution is not synchronized with mempool, so nonce could have already
					// proceeded past wantNonce and that is expected.
					delete(m.evmNonces,addr)
					for _, x := range m.blocks {
						delete(x.evmNonces,addr)
					}
				}
			}
		}
		// n > m.next shouldn't really happen,
		// because local mempool is the only source of local lane blocks,
		// but we handle it gracefully anyway.
		m.next = max(m.next,n)
	}
}

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
		if err:=ctrl.WaitUntil(ctx, func() bool { return !m.IsFull() }); err!=nil {
			return nil, err
		}
		if resp.IsEVM {
			addr := resp.EVMSenderAddress
			nonce,ok := m.evmNonces[addr]
			if !ok {
				nonce = s.app.EvmNonce(addr)
			}
			if nonce != resp.EVMNonce {
				return nil, fmt.Errorf("%w: got %v, want %v", errBadNonce, resp.EVMNonce, nonce)
			}
			m.nextBlock.evmNonces[addr] = nonce + 1
			m.evmNonces[addr] = nonce + 1
		}
		// If any limit would be exceeded, then construct a payload.
		ok := uint64(len(m.nextBlock.txs)) + 1 <= s.cfg.maxTxsPerBlock()
		ok = ok && m.nextBlock.sizeBytes + uint64(len(tx)) <= types.MaxTxsBytesPerBlock
		ok = ok && m.nextBlock.gasWanted + gasWanted <= s.cfg.MaxGasPerBlock
		if !ok {
			m.PushBlock()	
			ctrl.Updated()
		}

		// Normalize the gas estimate.
		gasEstimated := resp.GasEstimated
		if gasEstimated < minTxGas || gasEstimated > resp.GasWanted {
			gasEstimated = resp.GasWanted
		}
		b := m.nextBlock
		b.gasEstimated += utils.Clamp[uint64](gasEstimated)
		b.gasWanted += utils.Clamp[uint64](resp.GasWanted)
		b.sizeBytes += uint64(len(tx))
		b.txs = append(b.txs,tx)
	}
	return resp.ResponseCheckTx,nil
}
