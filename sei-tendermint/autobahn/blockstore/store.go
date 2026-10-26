// Package blockstore implements types.BlockStore, the durable backing store for
// the consensus state machine, over any sei-db block database.
package blockstore

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

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
	index := qc.QC().Index()

	if err := validateBlocks(qc, blocks); err != nil {
		return fmt.Errorf("invalid QC blocks: %w", err)
	}
	block := types.NewSuperBlock(qc, blocks)
	records := []blocktypes.BlockRecord{{Number: blocktypes.BlockNumber(qc.Index()), Hash: block.CalculateBlockHash().Bytes(), Value: encodeBlocks(blocks)}}

	for status := range s.status.Lock() {
		current, ok := status.Get()
		if ok && current.NextQC != index {
			return fmt.Errorf("QC starts at %d, expected %d: %w", index, current.NextQC, types.ErrQCNonContiguous)
		}
		if err := s.db.PutQC(blocktypes.QCGroupRecord{
			QC: blocktypes.QCRecord{Data: encodeQC(qc)}, Blocks: records,
		}); err != nil {
			return fmt.Errorf("failed to put QC %d: %w", index, err)
		}
		if !ok {
			current = types.SuffixRange{
				First: index, NextAppQC: index, NextAppProposal: index,
			}
			// Reads below the first QC must return ErrPruned before and after reopening.
			s.setPruneWatermark(index)
		}
		current.NextQC = index+1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) WriteAppQC(appQC *types.AppQC) error {
	index, next := appQC.Proposal().Index(), appQC.Proposal().Next()
	if next <= index {
		return fmt.Errorf("AppQC at %d covers no blocks: %w", index, types.ErrAppQCNonContiguous)
	}
	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok {
			return fmt.Errorf("AppQC [%d,%d) has no matching QC: %w", index, next, types.ErrAppQCMissingQC)
		}
		if current.NextAppQC != index {
			return fmt.Errorf("AppQC starts at %d, expected %d: %w",
				index, current.NextAppQC, types.ErrAppQCNonContiguous)
		}
		if next > current.NextAppProposal {
			return fmt.Errorf("AppQC [%d,%d) is not covered by written AppProposals: %w", index, next, types.ErrAppQCMissingQC)
		}

		if err := s.db.PutAppQC(blocktypes.AppQCRecord{
			Data:  encodeAppQC(appQC),
			First: blocktypes.BlockNumber(index), Next: blocktypes.BlockNumber(next),
		}); err != nil {
			return fmt.Errorf("failed to put AppQC [%d,%d): %w", index, next, err)
		}
		current.NextAppQC = next
		current.First = current.NextAppQC - 1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) WriteAppProposal(appProposal *types.AppProposal) error {
	index, next := appProposal.Index(), appProposal.Next()
	if next <= index {
		return fmt.Errorf("AppProposal at %d covers no blocks: %w", index, types.ErrAppProposalNonContiguous)
	}
	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok {
			return fmt.Errorf("AppProposal [%d,%d) has no matching QC: %w", index, next, types.ErrAppProposalMissingQC)
		}
		if current.NextAppProposal != index {
			return fmt.Errorf("AppProposal starts at %d, expected %d: %w",
				index, current.NextAppProposal, types.ErrAppProposalNonContiguous)
		}
		if next > current.NextBlock {
			return fmt.Errorf("AppProposal [%d,%d) is not covered by written blocks: %w", index, next, types.ErrAppProposalMissingQC)
		}

		if err := s.db.PutAppProposal(blocktypes.AppProposalRecord{
			Data:  encodeAppProposal(appProposal),
			First: blocktypes.BlockNumber(index), Next: blocktypes.BlockNumber(next),
		}); err != nil {
			return fmt.Errorf("failed to put AppProposal [%d,%d): %w", index, next, err)
		}
		current.NextAppProposal = next
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) PruneBefore(blockHeight types.RoadIndex) error {
	for status := range s.status.Lock() {

		current, ok := status.Get()
		if !ok {
			// Ignore prune requests if we've not got any data yet. Simplifies several edge cases
			// and is technically a legal implementation of the contract in the godocs.
			return nil
		}

		blockHeight = min(blockHeight, current.First)

		s.setPruneWatermark(blockHeight)
		return nil
	}
	panic("unreachable")
}

// setPruneWatermark raises the floor below which reads return ErrPruned to n, and
// leaves it unchanged when it already sits at or above n.
func (s *Store) setPruneWatermark(n types.RoadIndex) {
	s.db.SetPruneWatermark(blocktypes.BlockNumber(n))
	Global.pruneWatermarkAt().Set(utils.Clamp[int64](s.db.GetPruneWatermark()))
}

func (s *Store) First() types.RoadIndex {
	return types.RoadIndex(s.db.GetPruneWatermark())
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
			count := 1
			if count > len(blocks) {
				return types.Suffix{}, types.ErrQCBlocksMismatch
			}
			group, err := decodeQCGroupBlocks(qc, blocks[:count])
			if err != nil {
				return types.Suffix{}, err
			}
			blocks = blocks[count:]
			status.NextQC = qc.QC().Next()
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
			status.NextAppProposal = proposal.Next()
			suffix.AppProposals = append(suffix.AppProposals, proposal)
		}
		for _, record := range records.AppQCs {
			qc, err := decodeAppQC(record.Data)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to decode suffix AppQC: %w", err)
			}
			status.NextAppQC = qc.Proposal().Next()
			status.First = status.NextAppQC - 1
			suffix.AppQCs = append(suffix.AppQCs, qc)
		}
		if len(suffix.QCGroups) == 0 {
			// Empty db.
			return types.Suffix{}, nil
		}
		// Set fields for missing resources.
		first := suffix.QCGroups[0].QC.QC().Index()
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

// readBlocks returns all lane blocks belonging to road n.
func (s *Store) readBlocks(n types.RoadIndex) (utils.Option[[]*types.Block], error) {
	if blocktypes.BlockNumber(n) < s.db.GetPruneWatermark() {
		return utils.None[[]*types.Block](), types.ErrPruned
	}
	value, exists, err := s.db.GetBlock(blocktypes.BlockNumber(n))
	if err != nil {
		return utils.None[[]*types.Block](), err
	}
	if !exists {
		return utils.None[[]*types.Block](), nil
	}
	blocks, err := decodeBlocks(value)
	if err != nil {
		return utils.None[[]*types.Block](), err
	}
	return utils.Some(blocks), nil
}

func (s *Store) ReadLaneBlock(pos types.LaneBlockPos) (utils.Option[*types.Block], error) {
	opt, err := s.readBlocks(pos.Index)
	if err != nil {
		return utils.None[*types.Block](), err
	}
	blocks, ok := opt.Get()
	if !ok || pos.Offset >= uint64(len(blocks)) {
		return utils.None[*types.Block](), nil
	}
	return utils.Some(blocks[pos.Offset]), nil
}

func (s *Store) ReadSuperBlockByNumber(n types.RoadIndex) (utils.Option[*types.SuperBlock], error) {
	opt, err := s.readBlocks(n)
	if err != nil {
		return utils.None[*types.SuperBlock](), err
	}
	blocks, ok := opt.Get()
	if !ok {
		return utils.None[*types.SuperBlock](), nil
	}
	return s.assembleSuperBlock(n, blocks)
}

func (s *Store) assembleSuperBlock(n types.RoadIndex, blocks []*types.Block) (utils.Option[*types.SuperBlock], error) {
	opt, err := s.ReadQCByBlockNumber(n)
	if err != nil {
		return utils.None[*types.SuperBlock](), err
	}
	qc, ok := opt.Get()
	if !ok {
		return utils.None[*types.SuperBlock](), types.ErrQCBlocksMismatch
	}
	if err := validateBlocks(qc, blocks); err != nil {
		return utils.None[*types.SuperBlock](), err
	}
	return utils.Some(types.NewSuperBlock(qc, blocks)), nil
}

func (s *Store) ReadSuperBlockByHash(hash common.Hash) (utils.Option[*types.SuperBlock], error) {
	n, value, exists, err := s.db.GetBlockByHash(hash.Bytes())
	if err != nil {
		return utils.None[*types.SuperBlock](), err
	}
	if !exists || n < s.db.GetPruneWatermark() {
		return utils.None[*types.SuperBlock](), nil
	}
	blocks, err := decodeBlocks(value)
	if err != nil {
		return utils.None[*types.SuperBlock](), err
	}
	block, err := s.assembleSuperBlock(types.RoadIndex(n), blocks)
	if errors.Is(err, types.ErrPruned) {
		return utils.None[*types.SuperBlock](), nil
	}
	return block, err
}

func (s *Store) ReadQCByBlockNumber(
	n types.RoadIndex,
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
	n types.RoadIndex,
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
	n types.RoadIndex,
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
