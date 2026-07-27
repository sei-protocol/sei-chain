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

	// watermark is the (clamped) retention floor set by PruneBefore. Reads
	// strictly below it are refused with types.ErrPruned; because pruned entries
	// are deleted eagerly, this is the only record of where the floor sits and
	// so the only way to tell a pruned block from one never written.
	watermark types.GlobalBlockNumber
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
	if s.hasBlocks && n != s.lastBlockNumber+1 {
		return fmt.Errorf("block number %d not contiguous with last written %d: %w",
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
	// Round the watermark down to the covering QC's First. A QC's cohort of
	// blocks changes readability atomically, so the watermark must never fall
	// strictly inside a QC's range (see littblock): otherwise a read would
	// refuse the cohort's low blocks while still serving its high blocks (which
	// pruning must retain).
	for _, e := range s.qcsByLower {
		if e.lower <= n && n < e.upper {
			n = e.lower
			break
		}
	}
	s.watermark = max(s.watermark, n)
	for num, blk := range s.byNumber {
		if num < s.watermark {
			delete(s.byNumber, num)
			delete(s.byHash, blk.Header().Hash())
		}
	}
	for lower, e := range s.qcsByLower {
		if e.upper <= s.watermark {
			delete(s.qcsByLower, lower)
		}
	}
	return nil
}

func (s *blockDB) Flush() error { return nil }

func (s *blockDB) Status() types.DBStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var tips types.DBStatus
	if s.hasBlocks {
		tips.NextBlock = s.lastBlockNumber + 1
	}
	if s.hasQC {
		tips.NextQC = s.lastQCNext
	}
	return tips
}

func (s *blockDB) Iterator(n types.GlobalBlockNumber) (types.BlockDBIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	start := max(n, s.watermark)
	entries := s.sortedQCsLocked()
	if len(entries) == 0 || start >= entries[len(entries)-1].upper {
		// Nothing is covered at or above the (clamped) start.
		return &memBlockDBIterator{idx: -1}, nil
	}
	return s.iteratorLocked(entries, start), nil
}

// sortedQCsLocked returns the retained QC entries ascending by lower bound. Caller holds mu.
func (s *blockDB) sortedQCsLocked() []qcEntry {
	entries := make([]qcEntry, 0, len(s.qcsByLower))
	for _, e := range s.qcsByLower {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].lower < entries[j].lower })
	return entries
}

// iteratorLocked snapshots every covered number from start upward (clamping up to the first
// covered number when start falls below all coverage), pairing each with its covering QC and
// (possibly absent) block. Caller holds mu and guarantees some entry's range ends above start.
func (s *blockDB) iteratorLocked(entries []qcEntry, start types.GlobalBlockNumber) *memBlockDBIterator {
	it := &memBlockDBIterator{idx: -1}
	for _, e := range entries {
		for num := max(e.lower, start); num < e.upper; num++ {
			it.nums = append(it.nums, num)
			it.qcs = append(it.qcs, e.qc)
			it.blocks = append(it.blocks, s.byNumber[num])
		}
	}
	return it
}

var _ types.BlockDBIterator = (*memBlockDBIterator)(nil)

// memBlockDBIterator steps through a snapshot of covered numbers captured at creation.
type memBlockDBIterator struct {
	// nums holds every covered number, ascending.
	nums []types.GlobalBlockNumber

	// qcs holds the covering QC per position.
	qcs []*types.FullCommitQC

	// blocks holds the block per position; nil where no block is persisted.
	blocks []*types.Block

	// idx is the current position; -1 before the first Next.
	idx int
}

func (it *memBlockDBIterator) Next() (bool, error) {
	it.idx++
	return it.idx < len(it.nums), nil
}

func (it *memBlockDBIterator) Number() types.GlobalBlockNumber { return it.nums[it.idx] }

func (it *memBlockDBIterator) QC() (*types.FullCommitQC, error) { return it.qcs[it.idx], nil }

func (it *memBlockDBIterator) Block() (utils.Option[*types.Block], error) {
	if it.blocks[it.idx] == nil {
		return utils.None[*types.Block](), nil
	}
	return utils.Some(it.blocks[it.idx]), nil
}

func (it *memBlockDBIterator) Close() error { return nil }

func (s *blockDB) ReadBlockByNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.Block], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n < s.watermark {
		return utils.None[*types.Block](), types.ErrPruned
	}
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
	if n < s.watermark {
		return utils.None[*types.FullCommitQC](), types.ErrPruned
	}
	for _, e := range s.qcsByLower {
		if e.lower <= n && n < e.upper {
			return utils.Some(e.qc), nil
		}
	}
	return utils.None[*types.FullCommitQC](), nil
}

func (s *blockDB) Close() error { return nil }
