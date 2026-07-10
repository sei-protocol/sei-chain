package memblock

import (
	"fmt"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ types.BlockDB = (*blockDB)(nil)

// qcEntry pairs a QC with the half-open range [lower, upper) it covers, as
// supplied by the caller to WriteQC.
type qcEntry struct {
	qc    *types.FullCommitQC
	lower types.GlobalBlockNumber
	upper types.GlobalBlockNumber
}

// hashEntry pairs a block with its GlobalBlockNumber so ReadBlockByHash can
// return the number, mirroring the littblock implementation which embeds it in
// the stored value.
type hashEntry struct {
	blk *types.Block
	n   types.GlobalBlockNumber
}

// blockDB is an in-memory types.BlockDB. It holds blocks and QCs by pointer (no
// marshaling) and is intended as a test/benchmark fixture, not a durable
// implementation.
type blockDB struct {
	mu         sync.RWMutex
	byNumber   map[types.GlobalBlockNumber]*types.Block
	byHash     map[types.BlockHeaderHash]hashEntry
	qcsByLower map[types.GlobalBlockNumber]qcEntry

	// Write-order cursors (see types.BlockDB contract).
	hasBlocks       bool
	lastBlockNumber types.GlobalBlockNumber
	hasQC           bool
	lastQCNext      types.GlobalBlockNumber

	// latestQCStartBlock is the most recently written QC's starting block number —
	// the lowest block number in the newest cohort. PruneBefore clamps to it (see
	// littblock).
	latestQCStartBlock types.GlobalBlockNumber
}

// NewBlockDB returns an in-memory types.BlockDB.
func NewBlockDB() types.BlockDB {
	return &blockDB{
		byNumber:   make(map[types.GlobalBlockNumber]*types.Block),
		byHash:     make(map[types.BlockHeaderHash]hashEntry),
		qcsByLower: make(map[types.GlobalBlockNumber]qcEntry),
	}
}

func (s *blockDB) WriteBlock(n types.GlobalBlockNumber, blk *types.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasBlocks && n <= s.lastBlockNumber {
		return fmt.Errorf("block number %d not greater than last written %d: %w",
			n, s.lastBlockNumber, types.ErrBlockOutOfOrder)
	}
	// A covering QC must already be written. QCs are contiguous and blocks
	// strictly ascending, so n is covered iff n < lastQCNext.
	if !s.hasQC || n >= s.lastQCNext {
		return fmt.Errorf("block number %d not covered by any written QC (next QC bound %d): %w",
			n, s.lastQCNext, types.ErrBlockMissingQC)
	}
	s.byNumber[n] = blk
	s.byHash[blk.Header().Hash()] = hashEntry{blk: blk, n: n}
	s.lastBlockNumber = n
	s.hasBlocks = true
	return nil
}

func (s *blockDB) WriteQC(
	lowerBound types.GlobalBlockNumber,
	upperBound types.GlobalBlockNumber,
	qc *types.FullCommitQC,
) error {
	if lowerBound >= upperBound {
		return fmt.Errorf("QC lowerBound %d >= upperBound %d", lowerBound, upperBound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasQC && lowerBound != s.lastQCNext {
		return fmt.Errorf("QC lowerBound %d != expected %d: %w",
			lowerBound, s.lastQCNext, types.ErrQCNonContiguous)
	}
	s.qcsByLower[lowerBound] = qcEntry{qc: qc, lower: lowerBound, upper: upperBound}
	s.latestQCStartBlock = lowerBound
	s.lastQCNext = upperBound
	s.hasQC = true
	return nil
}

func (s *blockDB) PruneBefore(n types.GlobalBlockNumber) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasBlocks {
		// No blocks yet: nothing to prune, and deleting QCs here would strand a
		// future block whose coverage check still passes. Mirrors littblock.
		return nil
	}
	// Never let the watermark enter the newest block's cohort: clamp its ceiling
	// at the cohort's first block (latestQCStartBlock), guarded by lastBlockNumber
	// for a QC written ahead of its blocks. Keeps the newest cohort whole and
	// pruning monotonic. See littblock and the BlockDB PruneBefore contract.
	if ceiling := min(s.latestQCStartBlock, s.lastBlockNumber); n > ceiling {
		n = ceiling
	}
	for num, blk := range s.byNumber {
		if num < n {
			delete(s.byNumber, num)
			delete(s.byHash, blk.Header().Hash())
		}
	}
	for lower, e := range s.qcsByLower {
		if e.upper <= n {
			delete(s.qcsByLower, lower)
		}
	}
	return nil
}

func (s *blockDB) Flush() error { return nil }

func (s *blockDB) Blocks(reverse bool) (types.BlockIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nums := make([]types.GlobalBlockNumber, 0, len(s.byNumber))
	for n := range s.byNumber {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool {
		if reverse {
			return nums[i] > nums[j]
		}
		return nums[i] < nums[j]
	})
	blocks := make([]*types.Block, len(nums))
	for i, n := range nums {
		blocks[i] = s.byNumber[n]
	}
	return &memBlockIterator{nums: nums, blocks: blocks, idx: -1}, nil
}

func (s *blockDB) QCs(reverse bool) (types.QCIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	lowers := make([]types.GlobalBlockNumber, 0, len(s.qcsByLower))
	for l := range s.qcsByLower {
		lowers = append(lowers, l)
	}
	sort.Slice(lowers, func(i, j int) bool {
		if reverse {
			return lowers[i] > lowers[j]
		}
		return lowers[i] < lowers[j]
	})
	qcs := make([]*types.FullCommitQC, len(lowers))
	for i, l := range lowers {
		qcs[i] = s.qcsByLower[l].qc
	}
	return &memQCIterator{qcs: qcs, idx: -1}, nil
}

var (
	_ types.BlockIterator = (*memBlockIterator)(nil)
	_ types.QCIterator    = (*memQCIterator)(nil)
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
	hash types.BlockHeaderHash,
) (utils.Option[types.BlockWithNumber], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.byHash[hash]; ok {
		return utils.Some(types.BlockWithNumber{Block: e.blk, Number: e.n}), nil
	}
	return utils.None[types.BlockWithNumber](), nil
}

func (s *blockDB) ReadQCByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.FullCommitQC], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.qcsByLower {
		if e.lower <= n && n < e.upper {
			return utils.Some(e.qc), nil
		}
	}
	return utils.None[*types.FullCommitQC](), nil
}

func (s *blockDB) Close() error { return nil }
