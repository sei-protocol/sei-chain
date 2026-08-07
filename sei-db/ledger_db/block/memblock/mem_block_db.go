package memblock

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ types.BlockDB = (*blockDB)(nil)

// qcEntry pairs a QC with the half-open range [lower, upper) it covers, as
// derived from the QC itself by coveredRange.
type qcEntry struct {
	qc    *types.FullCommitQC
	lower types.GlobalBlockNumber
	upper types.GlobalBlockNumber
}

// appQCEntry pairs an AppQC with the half-open range [lower, upper) it covers.
type appQCEntry struct {
	appQC *types.AppQC
	lower types.GlobalBlockNumber
	upper types.GlobalBlockNumber
}

// appProposalEntry pairs an AppProposal with the half-open range [lower, upper)
// it covers.
type appProposalEntry struct {
	appProposal *types.AppProposal
	lower       types.GlobalBlockNumber
	upper       types.GlobalBlockNumber
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
	appQCs     map[types.GlobalBlockNumber]appQCEntry
	appProps   map[types.GlobalBlockNumber]appProposalEntry

	// Write-order cursors (see types.BlockDB contract).
	hasBlocks       bool
	lastBlockNumber types.GlobalBlockNumber
	hasQC           bool
	lastQCNext      types.GlobalBlockNumber
	hasAppProposal  bool
	lastAppPropNext types.GlobalBlockNumber
	hasAppQC        bool
	lastAppQCNext   types.GlobalBlockNumber

	// latestQCStartBlock is the most recently written QC's starting block number —
	// the lowest block number in the newest cohort. PruneBefore clamps to it (see
	// littblock).
	latestQCStartBlock types.GlobalBlockNumber

	// latestAppQCStartBlock is the most recently written AppQC's starting
	// block number. When AppQCs exist, PruneBefore clamps to it so the newest
	// AppQC cohort remains readable together with its CommitQC and blocks.
	latestAppQCStartBlock types.GlobalBlockNumber

	// latestAppProposalStartBlock is the most recently written AppProposal's
	// starting block number.
	latestAppProposalStartBlock types.GlobalBlockNumber

	// firstBlockNumber is the lowest block number written. Meaningful only while hasBlocks.
	firstBlockNumber types.GlobalBlockNumber

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
		appQCs:     make(map[types.GlobalBlockNumber]appQCEntry),
		appProps:   make(map[types.GlobalBlockNumber]appProposalEntry),
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
	if !s.hasBlocks {
		s.firstBlockNumber = n
	}
	s.lastBlockNumber = n
	s.hasBlocks = true
	return nil
}

// coveredRange returns the half-open global block number range the QC covers,
// as specified by types.BlockDB.WriteQC: [First, First+len(Headers())). Derived
// identically in littblock — see the comment there for why the bound comes from
// the header count rather than from GlobalRange().Next.
func coveredRange(qc *types.FullCommitQC) (types.GlobalBlockNumber, types.GlobalBlockNumber) {
	first := qc.QC().GlobalRange().First
	return first, first + types.GlobalBlockNumber(len(qc.Headers()))
}

func (s *blockDB) WriteQC(qc *types.FullCommitQC) error {
	first, next := coveredRange(qc)
	if first >= next {
		return fmt.Errorf("QC at %d covers no blocks: %w", first, types.ErrQCNonContiguous)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasQC && first != s.lastQCNext {
		return fmt.Errorf("QC starts at %d, expected %d: %w",
			first, s.lastQCNext, types.ErrQCNonContiguous)
	}
	s.qcsByLower[first] = qcEntry{qc: qc, lower: first, upper: next}
	s.latestQCStartBlock = first
	s.lastQCNext = next
	s.hasQC = true
	return nil
}

func appQCRange(appQC *types.AppQC) (types.GlobalBlockNumber, types.GlobalBlockNumber) {
	return appProposalRange(appQC.Proposal())
}

func appProposalRange(appProposal *types.AppProposal) (types.GlobalBlockNumber, types.GlobalBlockNumber) {
	gr := appProposal.GlobalRange()
	return gr.First, gr.Next
}

func (s *blockDB) WriteAppProposal(appProposal *types.AppProposal) error {
	first, next := appProposalRange(appProposal)
	if first >= next {
		return fmt.Errorf("AppProposal at %d covers no blocks: %w", first, types.ErrAppProposalNonContiguous)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasQC {
		return fmt.Errorf("AppProposal [%d,%d) has no matching QC: %w", first, next, types.ErrAppProposalMissingQC)
	}
	if s.hasAppProposal {
		if first != s.lastAppPropNext {
			return fmt.Errorf("AppProposal starts at %d, expected %d: %w",
				first, s.lastAppPropNext, types.ErrAppProposalNonContiguous)
		}
	} else {
		entries := s.sortedQCsLocked()
		if len(entries) == 0 {
			return fmt.Errorf("AppProposal [%d,%d) has no retained QC floor: %w", first, next, types.ErrAppProposalMissingQC)
		}
		if first != entries[0].lower {
			return fmt.Errorf("first AppProposal starts at %d, expected retained QC floor %d: %w",
				first, entries[0].lower, types.ErrAppProposalNonContiguous)
		}
	}
	if !s.hasBlocks || next > s.lastBlockNumber+1 {
		return fmt.Errorf("AppProposal [%d,%d) is not covered by written blocks: %w", first, next, types.ErrAppProposalMissingQC)
	}
	qc, ok := s.qcsByLower[first]
	if !ok || qc.upper != next {
		return fmt.Errorf("AppProposal [%d,%d) has no exact matching QC: %w",
			first, next, types.ErrAppProposalMissingQC)
	}
	if err := appProposal.Verify(qc.qc.QC()); err != nil {
		return fmt.Errorf("AppProposal [%d,%d) does not verify against matching QC: %w", first, next, err)
	}
	s.appProps[first] = appProposalEntry{appProposal: appProposal, lower: first, upper: next}
	s.latestAppProposalStartBlock = first
	s.lastAppPropNext = next
	s.hasAppProposal = true
	return nil
}

func (s *blockDB) WriteAppQC(appQC *types.AppQC) error {
	first, next := appQCRange(appQC)
	if first >= next {
		return fmt.Errorf("AppQC at %d covers no blocks: %w", first, types.ErrAppQCNonContiguous)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasQC {
		return fmt.Errorf("AppQC [%d,%d) has no matching QC: %w", first, next, types.ErrAppQCMissingQC)
	}
	if s.hasAppQC {
		if first != s.lastAppQCNext {
			return fmt.Errorf("AppQC starts at %d, expected %d: %w",
				first, s.lastAppQCNext, types.ErrAppQCNonContiguous)
		}
	} else {
		entries := s.sortedQCsLocked()
		if len(entries) == 0 {
			return fmt.Errorf("AppQC [%d,%d) has no retained QC floor: %w", first, next, types.ErrAppQCMissingQC)
		}
		if first != entries[0].lower {
			return fmt.Errorf("first AppQC starts at %d, expected retained QC floor %d: %w",
				first, entries[0].lower, types.ErrAppQCNonContiguous)
		}
	}
	if !s.hasAppProposal || next > s.lastAppPropNext {
		return fmt.Errorf("AppQC [%d,%d) is not covered by written AppProposals: %w", first, next, types.ErrAppQCMissingQC)
	}
	qc, ok := s.qcsByLower[first]
	if !ok || qc.upper != next {
		return fmt.Errorf("AppQC [%d,%d) has no exact matching QC: %w",
			first, next, types.ErrAppQCMissingQC)
	}
	s.appQCs[first] = appQCEntry{appQC: appQC, lower: first, upper: next}
	s.latestAppQCStartBlock = first
	s.lastAppQCNext = next
	s.hasAppQC = true
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
	n = min(n, s.statusLocked().Or(types.DBStatus{
		First:           0,
		NextBlock:       0,
		NextQC:          0,
		NextAppQC:       0,
		NextAppProposal: 0,
	}).First)
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
	for lower, e := range s.appQCs {
		if e.upper <= s.watermark {
			delete(s.appQCs, lower)
		}
	}
	for lower, e := range s.appProps {
		if e.upper <= s.watermark {
			delete(s.appProps, lower)
		}
	}
	return nil
}

func (s *blockDB) Flush() error { return nil }

func (s *blockDB) Status() types.DBStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusLocked().Or(types.DBStatus{
		First:           0,
		NextBlock:       0,
		NextQC:          0,
		NextAppQC:       0,
		NextAppProposal: 0,
	})
}

func (s *blockDB) statusLocked() utils.Option[types.DBStatus] {
	entries := s.sortedQCsLocked()
	if len(entries) == 0 {
		return utils.None[types.DBStatus]()
	}
	oldestQCStart := entries[0].lower
	first := max(oldestQCStart, s.watermark)
	if blockNumbers := s.sortedBlockNumbersLocked(); len(blockNumbers) > 0 {
		first = max(first, blockNumbers[0])
	}
	status := types.DBStatus{
		First:           first,
		NextBlock:       first,
		NextQC:          s.lastQCNext,
		NextAppQC:       first,
		NextAppProposal: first,
	}
	if s.hasBlocks {
		status.NextBlock = s.lastBlockNumber + 1
	}
	if s.hasAppQC {
		status.NextAppQC = s.lastAppQCNext
		status.First = status.NextAppQC - 1
	}
	if s.hasAppProposal {
		status.NextAppProposal = s.lastAppPropNext
	}
	return utils.Some(status)
}

func (s *blockDB) ReadRecent() (types.RecentData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := s.statusLocked()
	floor := status.Or(types.DBStatus{
		First:           0,
		NextBlock:       0,
		NextQC:          0,
		NextAppQC:       0,
		NextAppProposal: 0,
	}).First
	recent := types.RecentData{Status: status}

	for _, e := range s.sortedQCsLocked() {
		if e.upper <= s.watermark {
			continue
		}
		if e.upper <= floor {
			continue
		}
		recent.CommitQCs = append(recent.CommitQCs, e.qc)
	}
	for _, e := range s.sortedAppQCsLocked() {
		if e.upper <= floor {
			continue
		}
		recent.AppQCs = append(recent.AppQCs, e.appQC)
	}
	for _, e := range s.sortedAppProposalsLocked() {
		if e.upper <= floor {
			continue
		}
		recent.AppProposals = append(recent.AppProposals, e.appProposal)
	}
	for _, n := range s.sortedBlockNumbersLocked() {
		if n < floor {
			continue
		}
		recent.Blocks = append(recent.Blocks, types.RecentBlock{Number: n, Block: s.byNumber[n]})
	}
	return recent, nil
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

func (s *blockDB) sortedAppProposalsLocked() []appProposalEntry {
	entries := make([]appProposalEntry, 0, len(s.appProps))
	for _, e := range s.appProps {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].lower < entries[j].lower })
	return entries
}

func (s *blockDB) sortedAppQCsLocked() []appQCEntry {
	entries := make([]appQCEntry, 0, len(s.appQCs))
	for _, e := range s.appQCs {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].lower < entries[j].lower })
	return entries
}

func (s *blockDB) appQCCoveringLocked(n types.GlobalBlockNumber) *types.AppQC {
	for _, e := range s.appQCs {
		if e.lower <= n && n < e.upper {
			return e.appQC
		}
	}
	return nil
}

func (s *blockDB) appProposalCoveringLocked(n types.GlobalBlockNumber) *types.AppProposal {
	for _, e := range s.appProps {
		if e.lower <= n && n < e.upper {
			return e.appProposal
		}
	}
	return nil
}

func (s *blockDB) sortedBlockNumbersLocked() []types.GlobalBlockNumber {
	nums := make([]types.GlobalBlockNumber, 0, len(s.byNumber))
	for n := range s.byNumber {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	return nums
}

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

func (s *blockDB) ReadAppProposalByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.AppProposal], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n < s.watermark {
		return utils.None[*types.AppProposal](), types.ErrPruned
	}
	if appProposal := s.appProposalCoveringLocked(n); appProposal != nil {
		return utils.Some(appProposal), nil
	}
	return utils.None[*types.AppProposal](), nil
}

func (s *blockDB) ReadAppQCByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.AppQC], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n < s.watermark {
		return utils.None[*types.AppQC](), types.ErrPruned
	}
	if appQC := s.appQCCoveringLocked(n); appQC != nil {
		return utils.Some(appQC), nil
	}
	return utils.None[*types.AppQC](), nil
}

func (s *blockDB) Close() error { return nil }
