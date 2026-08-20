// Package blockstore implements types.BlockStore, the durable backing store for
// the consensus state machine, over any sei-db block database.
package blockstore

import (
	"fmt"
	"slices"
	"sync"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ types.BlockStore = (*Store)(nil)

// Store implements the consensus block store contract on top of any
// blocktypes.BlockDB. It owns what the storage layer is deliberately ignorant
// of: the value codec, the write-ordering contract, the recovery suffix, and the
// retention policy the storage garbage collector drives.
type Store struct {
	db blocktypes.BlockDB

	// status is the explicit write-order/recovery suffix cursor (see the
	// types.BlockStore contract). None means the store is empty. Guarded by mu.
	mu     sync.Mutex
	status utils.Option[types.SuffixRange]
}

// New returns a block store backed by db, recovering its write cursors from the
// records already in db. The store takes ownership of db: Close closes it. New
// does not close db when it returns an error.
func New(db blocktypes.BlockDB) (*Store, error) {
	s := &Store{db: db}
	suffix, err := s.ReadSuffix()
	if err != nil {
		return nil, fmt.Errorf("ReadSuffix(): %w", err)
	}
	s.status = suffix.Status
	return s, nil
}

func (s *Store) WriteBlock(n types.GlobalBlockNumber, blk *types.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.status.Get()
	// A covering QC must already be written. Since QCs are contiguous and blocks
	// strictly ascending, n is covered iff n < status.NextQC. This guard also fixes
	// the QC-before-block write order: the covering QC's write has already issued
	// under this mutex, so on a crash a surviving block implies a surviving FullCommitQC.
	if !ok || n >= status.NextQC {
		return fmt.Errorf("block number %d not covered by any written QC: %w", n, types.ErrBlockMissingQC)
	}
	if n != status.NextBlock {
		return fmt.Errorf("block number %d not contiguous with last written %d: %w",
			n, status.NextBlock-1, types.ErrBlockOutOfOrder)
	}

	hash := blk.Header().Hash()
	if err := s.db.PutBlock(uint64(n), hash.Bytes(), encodeBlock(n, blk)); err != nil {
		return fmt.Errorf("failed to put block %d: %w", n, err)
	}
	status.NextBlock = n + 1
	s.status = utils.Some(status)
	return nil
}

func (s *Store) WriteQC(qc *types.FullCommitQC) error {
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

	if err := s.db.PutRecord(blocktypes.KindQC, uint64(gr.First), uint64(gr.Next), encodeQC(qc)); err != nil {
		return fmt.Errorf("failed to put QC [%d,%d): %w", gr.First, gr.Next, err)
	}

	if !ok {
		// The first QC may start anywhere its caller allows, and nothing below it will ever
		// be written. Record where coverage begins so Iterator can clamp to it without
		// discovering it by scanning; a reopen re-derives the same value.
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

func (s *Store) WriteAppQC(appQC *types.AppQC) error {
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

	if err := s.db.PutRecord(blocktypes.KindAppQC, uint64(gr.First), uint64(gr.Next), encodeAppQC(appQC)); err != nil {
		return fmt.Errorf("failed to put AppQC [%d,%d): %w", gr.First, gr.Next, err)
	}
	status.NextAppQC = gr.Next
	status.First = status.NextAppQC - 1
	s.status = utils.Some(status)
	return nil
}

func (s *Store) WriteAppProposal(appProposal *types.AppProposal) error {
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

	if err := s.db.PutRecord(
		blocktypes.KindAppProposal, uint64(gr.First), uint64(gr.Next), encodeAppProposal(appProposal),
	); err != nil {
		return fmt.Errorf("failed to put AppProposal [%d,%d): %w", gr.First, gr.Next, err)
	}
	status.NextAppProposal = gr.Next
	s.status = utils.Some(status)
	return nil
}

func (s *Store) PruneBefore(blockHeight types.GlobalBlockNumber) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	status, ok := s.status.Get()
	if !ok {
		// Ignore prune requests if we've not got any data yet. Simplifies several edge cases
		// and is technically a legal implementation of the contract in the godocs.
		return nil
	}

	// Round the watermark down to the start of a QC's range, to avoid pruning a QC before its blocks.
	blockHeight, err := s.clampPruneBoundary(min(blockHeight, status.First))
	if err != nil {
		return err
	}

	// SetPruneWatermark only ever raises the floor, so a request that lands at or
	// below where it already sits is a no-op.
	s.db.SetPruneWatermark(uint64(blockHeight))
	return nil
}

func (s *Store) First() types.GlobalBlockNumber {
	return types.GlobalBlockNumber(s.db.PruneWatermark())
}

func (s *Store) Flush() error {
	if err := s.db.Flush(); err != nil {
		return fmt.Errorf("failed to flush block db: %w", err)
	}
	return nil
}

func (s *Store) Status() utils.Option[types.SuffixRange] {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// ReadSuffix reads the materialized startup-recovery suffix.
func (s *Store) ReadSuffix() (types.Suffix, error) {
	// Suffix is computed under lock, so that GC cannot malform it:
	// locked => no new data can be appended => watermark doesn't move => GC doesn't consume data of the suffix.
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.db.Scan(true)
	if err != nil {
		return types.Suffix{}, fmt.Errorf("failed to open suffix iterator: %w", err)
	}
	defer func() { _ = it.Close() }()
	var suffix types.Suffix
	var status types.SuffixRange
	var oldestQC *types.FullCommitQC
	var gotBlock, gotQC, gotAppProposal, gotAppQC bool
	for !gotAppQC || !gotQC || status.NextAppQC <= oldestQC.QC().GlobalRange().Next {
		ok, err := it.Next()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("failed to advance suffix iterator: %w", err)
		}
		if !ok {
			break
		}
		value, err := it.Value()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("failed to read suffix value: %w", err)
		}
		switch it.Kind() {
		case blocktypes.KindBlock:
			n, block, err := decodeBlock(value)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to decode suffix block: %w", err)
			}
			if !gotBlock {
				status.NextBlock = n + 1
				gotBlock = true
			}
			suffix.Blocks = append(suffix.Blocks, types.SuffixBlock{Number: n, Block: block})
		case blocktypes.KindAppQC:
			appQC, err := decodeAppQC(value)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to decode suffix AppQC: %w", err)
			}
			gr := appQC.Proposal().GlobalRange()
			if !gotAppQC {
				status.NextAppQC = gr.Next
				status.First = gr.Next - 1
				gotAppQC = true
			}
			suffix.AppQCs = append(suffix.AppQCs, appQC)
		case blocktypes.KindAppProposal:
			appProposal, err := decodeAppProposal(value)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to decode suffix AppProposal: %w", err)
			}
			gr := appProposal.GlobalRange()
			if !gotAppProposal {
				status.NextAppProposal = gr.Next
				gotAppProposal = true
			}
			suffix.AppProposals = append(suffix.AppProposals, appProposal)
		case blocktypes.KindQC:
			qc, err := decodeQC(value)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to decode suffix CommitQC: %w", err)
			}
			gr := qc.QC().GlobalRange()
			if !gotQC {
				status.NextQC = gr.Next
				gotQC = true
			}
			oldestQC = qc
			suffix.CommitQCs = append(suffix.CommitQCs, qc)
		}
	}
	if !gotQC {
		// Empty db.
		return types.Suffix{}, nil
	}
	// Set fields for missing resources.
	first := oldestQC.QC().GlobalRange().First
	status.NextQC = max(status.NextQC, first)
	status.NextBlock = max(status.NextBlock, first)
	status.NextAppProposal = max(status.NextAppProposal, first)
	status.NextAppQC = max(status.NextAppQC, first)
	status.First = max(status.First, first)
	suffix.Status = utils.Some(status)

	// Prune resources fully below status.First.
	suffix.CommitQCs = slices.DeleteFunc(suffix.CommitQCs, func(qc *types.FullCommitQC) bool {
		return qc.QC().GlobalRange().Next <= status.First
	})
	suffix.Blocks = slices.DeleteFunc(suffix.Blocks, func(block types.SuffixBlock) bool {
		return block.Number < status.First
	})
	suffix.AppProposals = slices.DeleteFunc(suffix.AppProposals, func(appProposal *types.AppProposal) bool {
		return appProposal.GlobalRange().Next <= status.First
	})
	suffix.AppQCs = slices.DeleteFunc(suffix.AppQCs, func(appQC *types.AppQC) bool {
		return appQC.Proposal().GlobalRange().Next <= status.First
	})
	// Put resources in increasing order.
	slices.Reverse(suffix.CommitQCs)
	slices.Reverse(suffix.Blocks)
	slices.Reverse(suffix.AppProposals)
	slices.Reverse(suffix.AppQCs)
	return suffix, nil
}

func (s *Store) ReadBlockByNumber(n types.GlobalBlockNumber) (utils.Option[*types.Block], error) {
	// Data below watermark should not be visible to the caller, even though it is pruned asynchronously.
	if uint64(n) < s.db.PruneWatermark() {
		return utils.None[*types.Block](), types.ErrPruned
	}
	value, exists, err := s.db.GetRecord(blocktypes.KindBlock, uint64(n))
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to read block: %w", err)
	}
	if !exists {
		return utils.None[*types.Block](), nil
	}
	_, blk, err := decodeBlock(value)
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return utils.Some(blk), nil
}

func (s *Store) ReadBlockByHash(hash types.BlockHeaderHash) (utils.Option[types.BlockWithNumber], error) {
	value, exists, err := s.db.GetBlockByHash(hash.Bytes())
	if err != nil {
		return utils.None[types.BlockWithNumber](), fmt.Errorf("failed to read block: %w", err)
	}
	if !exists {
		return utils.None[types.BlockWithNumber](), nil
	}
	n, blk, err := decodeBlock(value)
	if err != nil {
		return utils.None[types.BlockWithNumber](), fmt.Errorf("failed to unmarshal block: %w", err)
	}
	// The number is not known until the block is resolved;
	// Data below watermark should not be visible to the caller, even though it is pruned asynchronously.
	if uint64(n) < s.db.PruneWatermark() {
		return utils.None[types.BlockWithNumber](), nil
	}
	return utils.Some(types.BlockWithNumber{Block: blk, Number: n}), nil
}

func (s *Store) ReadQCByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.FullCommitQC], error) {
	// Below-watermark blocks are not served, so neither is their covering QC.
	if uint64(n) < s.db.PruneWatermark() {
		return utils.None[*types.FullCommitQC](), types.ErrPruned
	}
	value, exists, err := s.db.GetRecord(blocktypes.KindQC, uint64(n))
	if err != nil {
		return utils.None[*types.FullCommitQC](), fmt.Errorf("failed to read QC: %w", err)
	}
	if !exists {
		return utils.None[*types.FullCommitQC](), nil
	}
	qc, err := decodeQC(value)
	if err != nil {
		return utils.None[*types.FullCommitQC](), fmt.Errorf("failed to unmarshal QC: %w", err)
	}
	return utils.Some(qc), nil
}

func (s *Store) ReadAppProposalByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.AppProposal], error) {
	if uint64(n) < s.db.PruneWatermark() {
		return utils.None[*types.AppProposal](), types.ErrPruned
	}
	value, exists, err := s.db.GetRecord(blocktypes.KindAppProposal, uint64(n))
	if err != nil {
		return utils.None[*types.AppProposal](), fmt.Errorf("failed to read AppProposal: %w", err)
	}
	if !exists {
		return utils.None[*types.AppProposal](), nil
	}
	appProposal, err := decodeAppProposal(value)
	if err != nil {
		return utils.None[*types.AppProposal](), fmt.Errorf("failed to unmarshal AppProposal: %w", err)
	}
	return utils.Some(appProposal), nil
}

func (s *Store) ReadAppQCByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.AppQC], error) {
	if uint64(n) < s.db.PruneWatermark() {
		return utils.None[*types.AppQC](), types.ErrPruned
	}
	value, exists, err := s.db.GetRecord(blocktypes.KindAppQC, uint64(n))
	if err != nil {
		return utils.None[*types.AppQC](), fmt.Errorf("failed to read AppQC: %w", err)
	}
	if !exists {
		return utils.None[*types.AppQC](), nil
	}
	appQC, err := decodeAppQC(value)
	if err != nil {
		return utils.None[*types.AppQC](), fmt.Errorf("failed to unmarshal AppQC: %w", err)
	}
	return utils.Some(appQC), nil
}

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("failed to close block db: %w", err)
	}
	return nil
}
