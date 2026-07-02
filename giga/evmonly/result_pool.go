package evmonly

import (
	"context"
	"sync"
	"sync/atomic"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type blockResultPool struct {
	free chan *BlockResult
}

type blockResultLease struct {
	pool   *blockResultPool
	result *BlockResult
	refs   atomic.Int32
}

func newBlockResultPool(size int) *blockResultPool {
	if size <= 0 {
		return nil
	}
	p := &blockResultPool{free: make(chan *BlockResult, size)}
	for range size {
		p.free <- &BlockResult{}
	}
	return p
}

func (p *blockResultPool) acquire(ctx context.Context, txCapacity int) (*BlockResult, error) {
	if p == nil {
		result := &BlockResult{}
		result.prepareForBlock(txCapacity)
		return result, nil
	}
	select {
	case result := <-p.free:
		result.prepareForBlock(txCapacity)
		lease := &blockResultLease{pool: p, result: result}
		lease.refs.Store(1)
		result.lease = lease
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *blockResultLease) retain() func() {
	if l == nil {
		return func() {}
	}
	l.refs.Add(1)
	var once sync.Once
	return func() {
		once.Do(l.release)
	}
}

func (l *blockResultLease) release() {
	if l == nil {
		return
	}
	if l.refs.Add(-1) != 0 {
		return
	}
	result := l.result
	result.resetForPool()
	l.pool.free <- result
}

func (r *BlockResult) retain() func() {
	if r == nil || r.lease == nil {
		return func() {}
	}
	return r.lease.retain()
}

func (r *BlockResult) prepareForBlock(txCapacity int) {
	r.resetForPool()
	if cap(r.Txs) < txCapacity {
		r.Txs = make([]TxResult, 0, txCapacity)
	}
	if cap(r.Receipts) < txCapacity {
		r.Receipts = make(ethtypes.Receipts, 0, txCapacity)
	}
}

func (r *BlockResult) prepareIndexedResults(txCount int) {
	if cap(r.Txs) < txCount {
		r.Txs = make([]TxResult, txCount)
	} else {
		r.Txs = r.Txs[:txCount]
		clear(r.Txs)
	}
	if cap(r.Receipts) < txCount {
		r.Receipts = make(ethtypes.Receipts, txCount)
	} else {
		r.Receipts = r.Receipts[:txCount]
		clear(r.Receipts)
	}
}

func (r *BlockResult) resetForPool() {
	r.ChangeSet.resetForReuse()
	clear(r.Txs)
	r.Txs = r.Txs[:0]
	clear(r.Receipts)
	r.Receipts = r.Receipts[:0]
	r.GasUsed = 0
	r.OCCStats = OCCStats{}
	r.lease = nil
}

func (cs *StateChangeSet) resetForReuse() {
	clear(cs.Balances)
	cs.Balances = cs.Balances[:0]
	clear(cs.Nonces)
	cs.Nonces = cs.Nonces[:0]
	clear(cs.Code)
	cs.Code = cs.Code[:0]
	clear(cs.StorageClears)
	cs.StorageClears = cs.StorageClears[:0]
	clear(cs.Storage)
	cs.Storage = cs.Storage[:0]
}
