package blocksim

import (
	"context"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ block.BlockDB = (*memBlockDB)(nil)

// memBlockDB is an in-memory block.BlockDB, used as the "mem" blocksim backend. It
// holds blocks and QCs by pointer (no marshaling) and is only intended as a
// harness fixture, not a durable implementation.
type memBlockDB struct {
	committee *types.Committee

	mu         sync.RWMutex
	byNumber   map[types.GlobalBlockNumber]*types.Block
	byHash     map[types.BlockHeaderHash]*types.Block
	qcsByFirst map[types.GlobalBlockNumber]*types.FullCommitQC
}

func newMemBlockDB(committee *types.Committee) *memBlockDB {
	return &memBlockDB{
		committee:  committee,
		byNumber:   make(map[types.GlobalBlockNumber]*types.Block),
		byHash:     make(map[types.BlockHeaderHash]*types.Block),
		qcsByFirst: make(map[types.GlobalBlockNumber]*types.FullCommitQC),
	}
}

func (s *memBlockDB) WriteBlock(_ context.Context, n types.GlobalBlockNumber, blk *types.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byNumber[n] = blk
	s.byHash[blk.Header().Hash()] = blk
	return nil
}

func (s *memBlockDB) WriteQC(_ context.Context, qc *types.FullCommitQC) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.qcsByFirst[qc.QC().GlobalRange(s.committee).First] = qc
	return nil
}

func (s *memBlockDB) PruneBefore(_ context.Context, n types.GlobalBlockNumber) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for num, blk := range s.byNumber {
		if num < n {
			delete(s.byNumber, num)
			delete(s.byHash, blk.Header().Hash())
		}
	}
	for first, qc := range s.qcsByFirst {
		if qc.QC().GlobalRange(s.committee).Next <= n {
			delete(s.qcsByFirst, first)
		}
	}
	return nil
}

func (s *memBlockDB) Flush(_ context.Context) error { return nil }

func (s *memBlockDB) ReadAll(_ context.Context) (*block.Loaded, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nums := make([]types.GlobalBlockNumber, 0, len(s.byNumber))
	for n := range s.byNumber {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	blocks := make([]block.BlockEntry, 0, len(nums))
	for _, n := range nums {
		blocks = append(blocks, block.BlockEntry{Number: n, Block: s.byNumber[n]})
	}

	firsts := make([]types.GlobalBlockNumber, 0, len(s.qcsByFirst))
	for f := range s.qcsByFirst {
		firsts = append(firsts, f)
	}
	sort.Slice(firsts, func(i, j int) bool { return firsts[i] < firsts[j] })
	qcs := make([]*types.FullCommitQC, 0, len(firsts))
	for _, f := range firsts {
		qcs = append(qcs, s.qcsByFirst[f])
	}

	return &block.Loaded{Blocks: blocks, QCs: qcs}, nil
}

func (s *memBlockDB) ReadBlockByNumber(
	_ context.Context,
	n types.GlobalBlockNumber,
) (utils.Option[*types.Block], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if blk, ok := s.byNumber[n]; ok {
		return utils.Some(blk), nil
	}
	return utils.None[*types.Block](), nil
}

func (s *memBlockDB) ReadBlockByHash(
	_ context.Context,
	hash types.BlockHeaderHash,
) (utils.Option[*types.Block], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if blk, ok := s.byHash[hash]; ok {
		return utils.Some(blk), nil
	}
	return utils.None[*types.Block](), nil
}

func (s *memBlockDB) ReadQCByBlockNumber(
	_ context.Context,
	n types.GlobalBlockNumber,
) (utils.Option[*types.FullCommitQC], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, qc := range s.qcsByFirst {
		if qc.QC().GlobalRange(s.committee).Has(n) {
			return utils.Some(qc), nil
		}
	}
	return utils.None[*types.FullCommitQC](), nil
}

func (s *memBlockDB) Close(_ context.Context) error { return nil }
