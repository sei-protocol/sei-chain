package producer

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var errTooLarge = errors.New("transaction too large")
var errBadNonce = errors.New("bad nonce")
var errMempoolFull = errors.New("mempool is full")

var ErrNotProducing = errors.New("not producing")

type blockSpec struct {
	gasEstimated uint64
	gasWanted    uint64
	sizeBytes    uint64
	txs          [][]byte
	evmHashes    []common.Hash
	// nonces of accounts which are expected to be bumped by this block.
	// They are checked against the app state after the block is executed.
	evmNonces map[common.Address]uint64
}

// mempool is the Watch target on State.mempool. Watch cannot replace its
// value, so the optional mempoolInner lives here (None when not producing).
type mempool struct {
	inner utils.Option[*mempoolInner]
}

// mempoolInner exists only while producing on a local lane.
type mempoolInner struct {
	capacity  uint64
	lane      types.LaneID
	first     types.BlockNumber
	next      types.BlockNumber
	blocks    map[types.BlockNumber]*blockSpec
	nextBlock *blockSpec
	evmNonces map[common.Address]uint64
	evmTxs    map[common.Hash]tmtypes.Tx
}

func newMempoolInner(capacity uint64, lane types.LaneID, n types.BlockNumber) *mempoolInner {
	return &mempoolInner{
		capacity:  capacity,
		lane:      lane,
		first:     n,
		next:      n,
		blocks:    map[types.BlockNumber]*blockSpec{},
		nextBlock: &blockSpec{evmNonces: map[common.Address]uint64{}},
		evmNonces: map[common.Address]uint64{},
		evmTxs:    map[common.Hash]tmtypes.Tx{},
	}
}

func (m *mempoolInner) IsFull() bool {
	return uint64(m.next-m.first) >= m.capacity && len(m.nextBlock.txs) > 0
}

func (m *mempoolInner) CanSealBlock(allowEmpty bool) bool {
	return uint64(m.next-m.first) < m.capacity && (allowEmpty || len(m.nextBlock.txs) > 0)
}

func (m *mempoolInner) SealBlock() {
	m.blocks[m.next] = m.nextBlock
	m.next += 1
	m.nextBlock = &blockSpec{
		evmNonces: map[common.Address]uint64{},
	}
}

// TODO(gprusak): this rpc is probably unused, but if it is
// consider whether unsequenced/unexecuted lane txs should be included here.
func (s *State) UnconfirmedTxs() [][]byte {
	for mp := range s.mempool.Lock() {
		if m, ok := mp.inner.Get(); ok {
			return m.nextBlock.txs
		}
		return nil
	}
	panic("unreachable")
}

func (s *State) EvmNextPendingNonce(addr common.Address) uint64 {
	for mp := range s.mempool.Lock() {
		if m, ok := mp.inner.Get(); ok {
			if nonce, ok := m.evmNonces[addr]; ok {
				return nonce
			}
		}
	}
	return s.app.EvmNonce(addr)
}

func (s *State) EvmTxByHash(hash common.Hash) (tmtypes.Tx, bool) {
	for mp := range s.mempool.Lock() {
		if m, ok := mp.inner.Get(); ok {
			tx, ok := m.evmTxs[hash]
			return tx, ok
		}
		return nil, false
	}
	panic("unreachable")
}

// Removes txs from mempool assigned to lane blocks <n.
// Caller is the sole runMempool session; m is that session's inner.
func (s *State) pruneMempool(m *mempoolInner, n types.BlockNumber) {
	for _, ctrl := range s.mempool.Lock() {
		if n < m.first {
			return
		}
		ctrl.Updated()
		for m.first < min(n, m.next) {
			b := m.blocks[m.first]
			delete(m.blocks, m.first)
			m.first += 1
			for _, hash := range b.evmHashes {
				delete(m.evmTxs, hash)
			}
			for addr, wantNonce := range b.evmNonces {
				if wantNonce == m.evmNonces[addr] {
					// Happy path: all account's txs got executed.
					delete(m.evmNonces, addr)
				} else if gotNonce := s.app.EvmNonce(addr); gotNonce < wantNonce {
					// Some txs have not been executed - reset account tracking.
					// NOTE: app execution is not synchronized with mempool, so nonce could have already
					// proceeded past wantNonce and that is expected.
					delete(m.evmNonces, addr)
					delete(m.nextBlock.evmNonces, addr)
					for _, x := range m.blocks {
						delete(x.evmNonces, addr)
					}
				}
			}
		}
		// n > m.next shouldn't really happen,
		// because local mempool is the only source of local lane blocks,
		// but we handle it gracefully anyway.
		m.next = max(m.next, n)
	}
}

// TryInsertTx inserts tx to the mempool. Returns error if mempool is full.
func (s *State) TryInsertTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTx, error) {
	return s.insertTx(ctx, tx, false)
}

// InsertTx inserts tx to the mempool. Blocks if mempool is full.
// The blocked calls are effectively the "unsequenced" part of the mempool.
// After InsertTx returns, the sequence is already scheduled to be included in a lane.
// TODO(gprusak): we might need some prioritization mechanism in case our node can handle more InsertTx calls/s
// than the lane throughput.
func (s *State) InsertTx(ctx context.Context, tx tmtypes.Tx) (*abci.ResponseCheckTx, error) {
	return s.insertTx(ctx, tx, true)
}

// Inserts transaction. Blocks until there is capacity in the mempool.
// NOTE: we currently don't do any tx filtering, which would prevent expensive CheckTxSafe calls.
// It has to be added after testnet launch.
func (s *State) insertTx(ctx context.Context, tx tmtypes.Tx, waitIfFull bool) (*abci.ResponseCheckTx, error) {
	if uint64(len(tx)) > types.MaxTxsBytesPerBlock {
		return nil, errTooLarge
	}
	resp, err := s.app.CheckTxSafe(ctx, &abci.RequestCheckTxV2{Tx: tx})
	if err != nil {
		return nil, err
	}
	if !resp.IsOK() {
		return resp.ResponseCheckTx, nil
	}
	gasWanted := utils.Clamp[uint64](resp.GasWanted)
	if gasWanted > s.cfg.MaxGasWantedPerBlock {
		return nil, errTooLarge
	}
	// Normalize the gas estimate.
	gasEstimated := utils.Clamp[uint64](resp.GasEstimated)
	if gasEstimated < minTxGas || gasEstimated > gasWanted {
		gasEstimated = gasWanted
	}
	if gasEstimated > s.cfg.MaxGasEstimatedPerBlock {
		return nil, errTooLarge
	}

	for mp, ctrl := range s.mempool.Lock() {
		var m *mempoolInner
		for {
			local, ok := s.consensus.Avail().LocalLane().Get()
			if !ok {
				return nil, ErrNotProducing
			}
			m, ok = mp.inner.Get()
			if !ok || m.lane != local {
				// Session not aligned yet (startup / after clear). InsertTx waits;
				// TryInsertTx fails fast.
				if !waitIfFull {
					return nil, ErrNotProducing
				}
				if err := ctrl.WaitUntil(ctx, func() bool {
					loc, ok := s.consensus.Avail().LocalLane().Get()
					if !ok {
						return true
					}
					m, ok := mp.inner.Get()
					return ok && m.lane == loc
				}); err != nil {
					return nil, err
				}
				continue
			}
			if !m.IsFull() {
				break
			}
			if !waitIfFull {
				return nil, errMempoolFull
			}
			// mempool is constructed as a FIFO - we do not delay insertions of large txs (going over cap)
			// in favor of waiting for smaller txs. This simple algorithm allows us to cap
			// pending txs to size of a single block. We can refine this rule later if needed.
			// NOTE: in case there are N concurrent InsertTx calls, this condition is reevaluated N times
			// every time mempool is updated. Depending on proportion of N to the block size it might get too
			// expensive.
			// Also wake when LocalLane clears so InsertTx cannot stall on IsFull forever.
			if err := ctrl.WaitUntil(ctx, func() bool {
				loc, ok := s.consensus.Avail().LocalLane().Get()
				if !ok {
					return true
				}
				m, ok := mp.inner.Get()
				return !ok || m.lane != loc || !m.IsFull()
			}); err != nil {
				return nil, err
			}
		}
		if resp.IsEVM {
			addr := resp.EVMSenderAddress
			nonce, ok := m.evmNonces[addr]
			if !ok {
				nonce = s.app.EvmNonce(addr)
			}
			if nonce != resp.EVMNonce {
				return nil, fmt.Errorf("%w: got %v, want %v", errBadNonce, resp.EVMNonce, nonce)
			}
			m.evmNonces[addr] = nonce + 1
		}
		// If any limit would be exceeded, then construct a payload.
		// Note that we use subtraction in a way avoiding arithmetic overflows.
		ok := s.cfg.maxTxsPerBlock()-uint64(len(m.nextBlock.txs)) >= 1
		ok = ok && types.MaxTxsBytesPerBlock-m.nextBlock.sizeBytes >= uint64(len(tx))
		ok = ok && s.cfg.MaxGasWantedPerBlock-m.nextBlock.gasWanted >= gasWanted
		ok = ok && s.cfg.MaxGasEstimatedPerBlock-m.nextBlock.gasEstimated >= gasEstimated
		if !ok {
			m.SealBlock()
		}
		if len(m.nextBlock.txs) == 0 {
			// We notify that we start a new block.
			ctrl.Updated()
		}

		b := m.nextBlock
		b.gasEstimated += utils.Clamp[uint64](gasEstimated)
		b.gasWanted += utils.Clamp[uint64](resp.GasWanted)
		b.sizeBytes += uint64(len(tx))
		b.txs = append(b.txs, tx)
		if resp.IsEVM {
			addr := resp.EVMSenderAddress
			b.evmNonces[addr] = m.evmNonces[addr]
			b.evmHashes = append(b.evmHashes, resp.EVMHash)
			m.evmTxs[resp.EVMHash] = tx
		}
	}
	return resp.ResponseCheckTx, nil
}
