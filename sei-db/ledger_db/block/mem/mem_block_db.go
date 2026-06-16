package mem

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ block.BlockDB = (*blockDB)(nil)

// blockDB is an in-memory block.BlockDB. It holds blocks and QCs by pointer (no
// marshaling) and is intended as a test/benchmark fixture, not a durable
// implementation.
type blockDB struct {
	committee *types.Committee

	mu         sync.RWMutex
	byNumber   map[types.GlobalBlockNumber]*types.Block
	byHash     map[types.BlockHeaderHash]*types.Block
	qcsByFirst map[types.GlobalBlockNumber]*types.FullCommitQC

	// Write-order cursors (see block.BlockDB contract).
	hasBlocks       bool
	lastBlockNumber types.GlobalBlockNumber
	hasQC           bool
	lastQCNext      types.GlobalBlockNumber
}

// NewBlockDB returns an in-memory block.BlockDB. committee is used to compute
// each QC's GlobalRange.
func NewBlockDB(committee *types.Committee) block.BlockDB {
	return &blockDB{
		committee:  committee,
		byNumber:   make(map[types.GlobalBlockNumber]*types.Block),
		byHash:     make(map[types.BlockHeaderHash]*types.Block),
		qcsByFirst: make(map[types.GlobalBlockNumber]*types.FullCommitQC),
	}
}

func (s *blockDB) WriteBlock(_ context.Context, n types.GlobalBlockNumber, blk *types.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasBlocks && n <= s.lastBlockNumber {
		return fmt.Errorf("block number %d not greater than last written %d: %w",
			n, s.lastBlockNumber, block.ErrBlockOutOfOrder)
	}
	s.byNumber[n] = blk
	s.byHash[blk.Header().Hash()] = blk
	s.lastBlockNumber = n
	s.hasBlocks = true
	return nil
}

func (s *blockDB) WriteQC(_ context.Context, qc *types.FullCommitQC) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := qc.QC().GlobalRange(s.committee)
	if s.hasQC && r.First != s.lastQCNext {
		return fmt.Errorf("QC GlobalRange().First %d != expected %d: %w",
			r.First, s.lastQCNext, block.ErrQCNonContiguous)
	}
	s.qcsByFirst[r.First] = qc
	s.lastQCNext = r.Next
	s.hasQC = true
	return nil
}

func (s *blockDB) PruneBefore(_ context.Context, n types.GlobalBlockNumber) error {
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

func (s *blockDB) Flush(_ context.Context) error { return nil }

func (s *blockDB) Blocks(_ context.Context) (block.BlockIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nums := make([]types.GlobalBlockNumber, 0, len(s.byNumber))
	for n := range s.byNumber {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	blocks := make([]*types.Block, len(nums))
	for i, n := range nums {
		blocks[i] = s.byNumber[n]
	}
	return &memBlockIterator{nums: nums, blocks: blocks, idx: -1}, nil
}

func (s *blockDB) QCs(_ context.Context) (block.QCIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	firsts := make([]types.GlobalBlockNumber, 0, len(s.qcsByFirst))
	for f := range s.qcsByFirst {
		firsts = append(firsts, f)
	}
	sort.Slice(firsts, func(i, j int) bool { return firsts[i] < firsts[j] })
	qcs := make([]*types.FullCommitQC, len(firsts))
	for i, f := range firsts {
		qcs[i] = s.qcsByFirst[f]
	}
	return &memQCIterator{qcs: qcs, idx: -1}, nil
}

var (
	_ block.BlockIterator = (*memBlockIterator)(nil)
	_ block.QCIterator    = (*memQCIterator)(nil)
)

// memBlockIterator iterates over a snapshot of blocks captured at creation.
type memBlockIterator struct {
	nums   []types.GlobalBlockNumber
	blocks []*types.Block
	idx    int
}

func (it *memBlockIterator) Next() (bool, error) {
	it.idx++
	return it.idx < len(it.nums), nil
}

func (it *memBlockIterator) Number() types.GlobalBlockNumber { return it.nums[it.idx] }
func (it *memBlockIterator) Block() (*types.Block, error)    { return it.blocks[it.idx], nil }
func (it *memBlockIterator) Close() error                    { return nil }

// memQCIterator iterates over a snapshot of QCs captured at creation.
type memQCIterator struct {
	qcs []*types.FullCommitQC
	idx int
}

func (it *memQCIterator) Next() (bool, error) {
	it.idx++
	return it.idx < len(it.qcs), nil
}

func (it *memQCIterator) QC() (*types.FullCommitQC, error) { return it.qcs[it.idx], nil }
func (it *memQCIterator) Close() error                     { return nil }

func (s *blockDB) ReadBlockByNumber(
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

func (s *blockDB) ReadBlockByHash(
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

func (s *blockDB) ReadQCByBlockNumber(
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

func (s *blockDB) Close(_ context.Context) error { return nil }
