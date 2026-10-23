// Package blockstore implements types.BlockStore, the durable backing store for
// the consensus state machine, over any sei-db block database.
package blockstore

import (
	"fmt"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ types.BlockStore = (*Store)(nil)

// Store implements the consensus block store contract on top of any
// blocktypes.BlockDB. It owns what the storage layer is deliberately ignorant
// of: the value codec, the write-ordering contract, and the
// retention policy the storage garbage collector drives.
type Store struct {
	db blocktypes.BlockDB

	// status is the explicit write-order/recovery suffix status (see the
	// types.BlockStore contract). None means the store is empty.
	status utils.Mutex[*utils.Option[types.SuffixRange]]
}

// New returns a block store backed by db, recovering its write cursors from the
// records already in db. The store takes ownership of db: Close closes it. New
// does not close db when it returns an error.
func New(db blocktypes.BlockDB) (*Store, error) {
	s := &Store{db: db, status: utils.NewMutex(new(utils.Option[types.SuffixRange]))}
	suffix, err := s.ReadSuffix()
	if err != nil {
		return nil, fmt.Errorf("ReadSuffix(): %w", err)
	}
	for status := range s.status.Lock() {
		*status = suffix.Status
	}
	Global.pruneWatermarkAt().Set(utils.Clamp[int64](db.GetPruneWatermark()))
	return s, nil
}

func (s *Store) WriteQC(qc *types.FullCommitQC, blocks []*types.Block) error {
	gr := qc.QC().GlobalRange()
	if gr.Len() == 0 {
		return fmt.Errorf("QC at %d covers no blocks: %w", gr.First, types.ErrQCNonContiguous)
	}
	if len(blocks) != len(qc.Headers()) {
		return fmt.Errorf("QC has %d headers but %d blocks: %w", len(qc.Headers()), len(blocks), types.ErrQCBlocksMismatch)
	}
	records := make([]blocktypes.BlockRecord, len(blocks))
	for i, block := range blocks {
		if block.Header().Hash() != qc.Headers()[i].Hash() {
			return fmt.Errorf("block %d does not match QC header: %w", i, types.ErrQCBlocksMismatch)
		}
		n := gr.First + types.GlobalBlockNumber(i) //nolint:gosec // non-negative slice index
		records[i] = blocktypes.BlockRecord{Number: blocktypes.BlockNumber(n), Hash: blocktypes.BlockHash(block.Header().Hash().Bytes()), Value: encodeBlock(block)}
	}

	for status := range s.status.Lock() {
		current, ok := status.Get()
		if ok && current.NextQC != gr.First {
			return fmt.Errorf("QC starts at %d, expected %d: %w", gr.First, current.NextQC, types.ErrQCNonContiguous)
		}
		if err := s.db.PutQC(blocktypes.QCGroupRecord{
			QC: blocktypes.QCRecord{Data: encodeQC(qc)}, Blocks: records,
		}); err != nil {
			return fmt.Errorf("failed to put QC [%d,%d): %w", gr.First, gr.Next, err)
		}
		if !ok {
			current = types.SuffixRange{
				First: gr.First, NextAppQC: gr.First, NextAppProposal: gr.First,
			}
			// Reads below the first QC must return ErrPruned before and after reopening.
			s.setPruneWatermark(gr.First)
		}
		current.NextQC = gr.Next
		current.NextBlock = gr.Next
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) WriteAppQC(appQC *types.AppQC) error {
	gr := appQC.Proposal().GlobalRange()
	if gr.Len() == 0 {
		return fmt.Errorf("AppQC at %d covers no blocks: %w", gr.First, types.ErrAppQCNonContiguous)
	}
	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok {
			return fmt.Errorf("AppQC [%d,%d) has no matching QC: %w", gr.First, gr.Next, types.ErrAppQCMissingQC)
		}
		if current.NextAppQC != gr.First {
			return fmt.Errorf("AppQC starts at %d, expected %d: %w",
				gr.First, current.NextAppQC, types.ErrAppQCNonContiguous)
		}
		if gr.Next > current.NextAppProposal {
			return fmt.Errorf("AppQC [%d,%d) is not covered by written AppProposals: %w", gr.First, gr.Next, types.ErrAppQCMissingQC)
		}

		if err := s.db.PutAppQC(blocktypes.AppQCRecord{
			Data:  encodeAppQC(appQC),
			First: blocktypes.BlockNumber(gr.First), Next: blocktypes.BlockNumber(gr.Next),
		}); err != nil {
			return fmt.Errorf("failed to put AppQC [%d,%d): %w", gr.First, gr.Next, err)
		}
		current.NextAppQC = gr.Next
		current.First = current.NextAppQC - 1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) WriteAppProposal(appProposal *types.AppProposal) error {
	gr := appProposal.GlobalRange()
	if gr.Len() == 0 {
		return fmt.Errorf("AppProposal at %d covers no blocks: %w", gr.First, types.ErrAppProposalNonContiguous)
	}
	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok {
			return fmt.Errorf("AppProposal [%d,%d) has no matching QC: %w", gr.First, gr.Next, types.ErrAppProposalMissingQC)
		}
		if current.NextAppProposal != gr.First {
			return fmt.Errorf("AppProposal starts at %d, expected %d: %w",
				gr.First, current.NextAppProposal, types.ErrAppProposalNonContiguous)
		}
		if gr.Next > current.NextBlock {
			return fmt.Errorf("AppProposal [%d,%d) is not covered by written blocks: %w", gr.First, gr.Next, types.ErrAppProposalMissingQC)
		}

		if err := s.db.PutAppProposal(blocktypes.AppProposalRecord{
			Data:  encodeAppProposal(appProposal),
			First: blocktypes.BlockNumber(gr.First), Next: blocktypes.BlockNumber(gr.Next),
		}); err != nil {
			return fmt.Errorf("failed to put AppProposal [%d,%d): %w", gr.First, gr.Next, err)
		}
		current.NextAppProposal = gr.Next
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) PruneBefore(blockHeight types.GlobalBlockNumber) error {
	for status := range s.status.Lock() {

		current, ok := status.Get()
		if !ok {
			// Ignore prune requests if we've not got any data yet. Simplifies several edge cases
			// and is technically a legal implementation of the contract in the godocs.
			return nil
		}

		// Round the watermark down to the start of a QC's range, to avoid pruning a QC before its blocks.
		blockHeight, err := s.clampPruneBoundary(min(blockHeight, current.First))
		if err != nil {
			return err
		}

		s.setPruneWatermark(blockHeight)
		return nil
	}
	panic("unreachable")
}

// setPruneWatermark raises the floor below which reads return ErrPruned to n, and
// leaves it unchanged when it already sits at or above n.
func (s *Store) setPruneWatermark(n types.GlobalBlockNumber) {
	s.db.SetPruneWatermark(blocktypes.BlockNumber(n))
	Global.pruneWatermarkAt().Set(utils.Clamp[int64](s.db.GetPruneWatermark()))
}

func (s *Store) First() types.GlobalBlockNumber {
	return types.GlobalBlockNumber(s.db.GetPruneWatermark())
}

func (s *Store) Flush() error {
	if err := s.db.Flush(); err != nil {
		return fmt.Errorf("failed to flush block db: %w", err)
	}
	return nil
}

func (s *Store) Status() utils.Option[types.SuffixRange] {
	for status := range s.status.Lock() {
		return *status
	}
	panic("unreachable")
}

// ReadSuffix reads the materialized startup-recovery suffix.
func (s *Store) ReadSuffix() (types.Suffix, error) {
	// Suffix is computed under lock, so that GC cannot malform it:
	// locked => no new data can be appended => watermark doesn't move => GC doesn't consume data of the suffix.
	for range s.status.Lock() {
		records, err := s.db.ReadSuffix()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("failed to read suffix: %w", err)
		}
		var suffix types.Suffix
		var status types.SuffixRange
		blocks := records.Blocks
		for _, record := range records.QCs {
			qc, err := decodeQC(record.Data)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to decode suffix CommitQC: %w", err)
			}
			count := len(qc.Headers())
			if count > len(blocks) {
				return types.Suffix{}, types.ErrQCBlocksMismatch
			}
			group, err := decodeQCGroupBlocks(qc, blocks[:count])
			if err != nil {
				return types.Suffix{}, err
			}
			blocks = blocks[count:]
			status.NextQC = qc.QC().GlobalRange().Next
			suffix.QCGroups = append(suffix.QCGroups, group)
		}
		if len(blocks) > 0 {
			return types.Suffix{}, types.ErrQCBlocksMismatch
		}
		for _, record := range records.AppProposals {
			proposal, err := decodeAppProposal(record.Data)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to decode suffix AppProposal: %w", err)
			}
			status.NextAppProposal = proposal.GlobalRange().Next
			suffix.AppProposals = append(suffix.AppProposals, proposal)
		}
		for _, record := range records.AppQCs {
			qc, err := decodeAppQC(record.Data)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to decode suffix AppQC: %w", err)
			}
			status.NextAppQC = qc.Proposal().GlobalRange().Next
			status.First = status.NextAppQC - 1
			suffix.AppQCs = append(suffix.AppQCs, qc)
		}
		if len(suffix.QCGroups) == 0 {
			// Empty db.
			return types.Suffix{}, nil
		}
		// Set fields for missing resources.
		first := suffix.QCGroups[0].QC.QC().GlobalRange().First
		status.NextQC = max(status.NextQC, first)
		status.NextBlock = status.NextQC
		status.NextAppProposal = max(status.NextAppProposal, first)
		status.NextAppQC = max(status.NextAppQC, first)
		status.First = max(status.First, first)
		suffix.Status = utils.Some(status)

		return suffix, nil
	}
	panic("unreachable")
}

func (s *Store) ReadBlockByNumber(n types.GlobalBlockNumber) (utils.Option[*types.Block], error) {
	// Data below watermark should not be visible to the caller, even though it is pruned asynchronously.
	if blocktypes.BlockNumber(n) < s.db.GetPruneWatermark() {
		return utils.None[*types.Block](), types.ErrPruned
	}
	value, exists, err := s.db.GetBlock(blocktypes.BlockNumber(n))
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to read block: %w", err)
	}
	if !exists {
		return utils.None[*types.Block](), nil
	}
	blk, err := decodeBlock(value)
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("failed to unmarshal block: %w", err)
	}
	return utils.Some(blk), nil
}

func (s *Store) ReadBlockByHash(hash types.BlockHeaderHash) (utils.Option[types.BlockWithNumber], error) {
	number, value, exists, err := s.db.GetBlockByHash(blocktypes.BlockHash(hash.Bytes()))
	if err != nil {
		return utils.None[types.BlockWithNumber](), fmt.Errorf("failed to read block: %w", err)
	}
	if !exists {
		return utils.None[types.BlockWithNumber](), nil
	}
	blk, err := decodeBlock(value)
	if err != nil {
		return utils.None[types.BlockWithNumber](), fmt.Errorf("failed to unmarshal block: %w", err)
	}
	// The number is not known until the block is resolved;
	// Data below watermark should not be visible to the caller, even though it is pruned asynchronously.
	if number < s.db.GetPruneWatermark() {
		return utils.None[types.BlockWithNumber](), nil
	}
	return utils.Some(types.BlockWithNumber{Block: blk, Number: types.GlobalBlockNumber(number)}), nil
}

func (s *Store) ReadQCByBlockNumber(
	n types.GlobalBlockNumber,
) (utils.Option[*types.FullCommitQC], error) {
	// Below-watermark blocks are not served, so neither is their covering QC.
	if blocktypes.BlockNumber(n) < s.db.GetPruneWatermark() {
		return utils.None[*types.FullCommitQC](), types.ErrPruned
	}
	value, exists, err := s.db.GetQC(blocktypes.BlockNumber(n))
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
	if blocktypes.BlockNumber(n) < s.db.GetPruneWatermark() {
		return utils.None[*types.AppProposal](), types.ErrPruned
	}
	value, exists, err := s.db.GetAppProposal(blocktypes.BlockNumber(n))
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
	if blocktypes.BlockNumber(n) < s.db.GetPruneWatermark() {
		return utils.None[*types.AppQC](), types.ErrPruned
	}
	value, exists, err := s.db.GetAppQC(blocktypes.BlockNumber(n))
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
