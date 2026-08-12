package memblock

import (
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ types.BlockDB = (*blockDB)(nil)

type hashEntry struct {
	blk *types.Block
	n   types.GlobalBlockNumber
}

// blockDB is an in-memory types.BlockDB. It mirrors littblock's write/status
// rules, but stores already-decoded values in maps instead of LittDB records.
type blockDB struct {
	mu sync.RWMutex

	blocksByNumber map[types.GlobalBlockNumber]*types.Block
	blocksByHash   map[types.BlockHeaderHash]hashEntry

	qcsByBlock map[types.GlobalBlockNumber]*types.FullCommitQC

	appProposalsByBlock map[types.GlobalBlockNumber]*types.AppProposal

	appQCsByBlock map[types.GlobalBlockNumber]*types.AppQC

	watermark types.GlobalBlockNumber
	status    utils.Option[types.SuffixRange]
}

// NewBlockDB returns an in-memory types.BlockDB.
func NewBlockDB() types.BlockDB {
	return &blockDB{
		blocksByNumber:      make(map[types.GlobalBlockNumber]*types.Block),
		blocksByHash:        make(map[types.BlockHeaderHash]hashEntry),
		qcsByBlock:          make(map[types.GlobalBlockNumber]*types.FullCommitQC),
		appProposalsByBlock: make(map[types.GlobalBlockNumber]*types.AppProposal),
		appQCsByBlock:       make(map[types.GlobalBlockNumber]*types.AppQC),
	}
}

func (s *blockDB) WriteBlock(n types.GlobalBlockNumber, blk *types.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	status, ok := s.status.Get()
	if !ok || n >= status.NextQC {
		return fmt.Errorf("block number %d not covered by any written QC: %w", n, types.ErrBlockMissingQC)
	}
	if n != status.NextBlock {
		return fmt.Errorf("block number %d not contiguous with last written %d: %w",
			n, status.NextBlock-1, types.ErrBlockOutOfOrder)
	}

	s.blocksByNumber[n] = blk
	s.blocksByHash[blk.Header().Hash()] = hashEntry{blk: blk, n: n}
	status.NextBlock = n + 1
	s.status = utils.Some(status)
	return nil
}

func (s *blockDB) WriteQC(qc *types.FullCommitQC) error {
	gr := qc.QC().GlobalRange()
	if gr.Len() == 0 {
		return fmt.Errorf("QC at %d covers no blocks: %w", gr.First, types.ErrQCNonContiguous)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	status, ok := s.status.Get()
	if ok && status.NextQC != gr.First {
		return fmt.Errorf("QC starts at %d, expected %d: %w",
			gr.First, status.NextQC, types.ErrQCNonContiguous)
	}

	for n := gr.First; n < gr.Next; n++ {
		s.qcsByBlock[n] = qc
	}

	if !ok {
		status = types.SuffixRange{
			First:           gr.First,
			NextAppQC:       gr.First,
			NextAppProposal: gr.First,
			NextBlock:       gr.First,
			NextQC:          gr.Next,
		}
	} else {
		status.NextQC = gr.Next
	}
	s.status = utils.Some(status)
	return nil
}

func (s *blockDB) WriteAppProposal(appProposal *types.AppProposal) error {
	gr := appProposal.GlobalRange()
	if gr.Len() == 0 {
		return fmt.Errorf("AppProposal at %d covers no blocks: %w", gr.First, types.ErrAppProposalNonContiguous)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	status, ok := s.status.Get()
	if !ok {
		return fmt.Errorf("AppProposal [%d,%d) has no matching QC: %w", gr.First, gr.Next, types.ErrAppProposalMissingQC)
	}
	if status.NextAppProposal != gr.First {
		return fmt.Errorf("AppProposal starts at %d, expected %d: %w",
			gr.First, status.NextAppProposal, types.ErrAppProposalNonContiguous)
	}
	if gr.Next > status.NextBlock {
		return fmt.Errorf("AppProposal [%d,%d) is not covered by written blocks: %w", gr.First, gr.Next, types.ErrAppProposalMissingQC)
	}

	for n := gr.First; n < gr.Next; n++ {
		s.appProposalsByBlock[n] = appProposal
	}
	status.NextAppProposal = gr.Next
	s.status = utils.Some(status)
	return nil
}

func (s *blockDB) WriteAppQC(appQC *types.AppQC) error {
	gr := appQC.Proposal().GlobalRange()
	if gr.Len() == 0 {
		return fmt.Errorf("AppQC at %d covers no blocks: %w", gr.First, types.ErrAppQCNonContiguous)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	status, ok := s.status.Get()
	if !ok {
		return fmt.Errorf("AppQC [%d,%d) has no matching QC: %w", gr.First, gr.Next, types.ErrAppQCMissingQC)
	}
	if status.NextAppQC != gr.First {
		return fmt.Errorf("AppQC starts at %d, expected %d: %w",
			gr.First, status.NextAppQC, types.ErrAppQCNonContiguous)
	}
	if gr.Next > status.NextAppProposal {
		return fmt.Errorf("AppQC [%d,%d) is not covered by written AppProposals: %w", gr.First, gr.Next, types.ErrAppQCMissingQC)
	}

	for n := gr.First; n < gr.Next; n++ {
		s.appQCsByBlock[n] = appQC
	}
	status.NextAppQC = gr.Next
	status.First = status.NextAppQC - 1
	s.status = utils.Some(status)
	return nil
}

func (s *blockDB) PruneBefore(n types.GlobalBlockNumber) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	status, ok := s.status.Get()
	if !ok {
		return nil
	}

	n = min(n, status.First)
	if qc, ok := s.qcsByBlock[n]; ok {
		n = qc.QC().GlobalRange().First
	}
	if n <= s.watermark {
		return nil
	}

	s.watermark = n
	for num, blk := range s.blocksByNumber {
		if num < s.watermark {
			delete(s.blocksByNumber, num)
			delete(s.blocksByHash, blk.Header().Hash())
		}
	}
	pruneRanges(s.watermark, s.qcsByBlock, func(qc *types.FullCommitQC) types.GlobalRange {
		return qc.QC().GlobalRange()
	})
	pruneRanges(s.watermark, s.appProposalsByBlock, func(appProposal *types.AppProposal) types.GlobalRange {
		return appProposal.GlobalRange()
	})
	pruneRanges(s.watermark, s.appQCsByBlock, func(appQC *types.AppQC) types.GlobalRange {
		return appQC.Proposal().GlobalRange()
	})
	return nil
}

func (s *blockDB) First() types.GlobalBlockNumber {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watermark
}

func pruneRanges[T any](
	watermark types.GlobalBlockNumber,
	byBlock map[types.GlobalBlockNumber]T,
	globalRange func(T) types.GlobalRange,
) {
	for n, value := range byBlock {
		if globalRange(value).Next <= watermark {
			delete(byBlock, n)
		}
	}
}

func (s *blockDB) Flush() error { return nil }

func (s *blockDB) Status() utils.Option[types.SuffixRange] {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *blockDB) ReadSuffix() (types.Suffix, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status, ok := s.status.Get()
	if !ok {
		return types.Suffix{}, nil
	}

	suffix := types.Suffix{Status: utils.Some(status)}
	suffix.CommitQCs = appendSuffixRanges(
		s.qcsByBlock,
		status.First,
		status.NextQC,
		func(qc *types.FullCommitQC) types.GlobalRange { return qc.QC().GlobalRange() },
	)
	suffix.AppProposals = appendSuffixRanges(
		s.appProposalsByBlock,
		status.First,
		status.NextAppProposal,
		func(appProposal *types.AppProposal) types.GlobalRange { return appProposal.GlobalRange() },
	)
	suffix.AppQCs = appendSuffixRanges(
		s.appQCsByBlock,
		status.First,
		status.NextAppQC,
		func(appQC *types.AppQC) types.GlobalRange { return appQC.Proposal().GlobalRange() },
	)
	for n := status.First; n < status.NextBlock; n++ {
		if block, ok := s.blocksByNumber[n]; ok {
			suffix.Blocks = append(suffix.Blocks, types.SuffixBlock{Number: n, Block: block})
		}
	}
	return suffix, nil
}

func appendSuffixRanges[T any](
	byBlock map[types.GlobalBlockNumber]T,
	floor types.GlobalBlockNumber,
	next types.GlobalBlockNumber,
	globalRange func(T) types.GlobalRange,
) []T {
	if next <= floor {
		return nil
	}
	values := make([]T, 0)
	var lastFirst types.GlobalBlockNumber
	haveLast := false
	for n := floor; n < next; n++ {
		value, ok := byBlock[n]
		if !ok {
			continue
		}
		gr := globalRange(value)
		if gr.Next <= floor || haveLast && gr.First == lastFirst {
			continue
		}
		values = append(values, value)
		lastFirst = gr.First
		haveLast = true
	}
	return values
}

func (s *blockDB) ReadBlockByNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.Block], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n < s.watermark {
		return utils.None[*types.Block](), types.ErrPruned
	}
	if blk, ok := s.blocksByNumber[n]; ok {
		return utils.Some(blk), nil
	}
	return utils.None[*types.Block](), nil
}

func (s *blockDB) ReadBlockByHash(
	hash types.BlockHeaderHash,
) (utils.Option[types.BlockWithNumber], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.blocksByHash[hash]; ok && e.n >= s.watermark {
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
	if qc, ok := s.qcsByBlock[n]; ok {
		return utils.Some(qc), nil
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
	if appProposal, ok := s.appProposalsByBlock[n]; ok {
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
	if appQC, ok := s.appQCsByBlock[n]; ok {
		return utils.Some(appQC), nil
	}
	return utils.None[*types.AppQC](), nil
}

func (s *blockDB) Close() error { return nil }
