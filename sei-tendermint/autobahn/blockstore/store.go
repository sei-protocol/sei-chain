// Package blockstore implements types.BlockStore, the durable backing store for
// the consensus state machine, over any sei-db block database.
package blockstore

import (
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

func (s *Store) WriteBlock(block *types.RoadBlock) error {
	index := block.QC().Index()

	record := encodeBlock(block)

	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok {
			current = types.SuffixRange{
				First:           index,
				NextBlock:       index,
				NextAppQC:       index,
				NextAppProposal: index,
			}
		}
		if current.NextBlock != index {
			return fmt.Errorf("block index %d, expected %d: %w", index, current.NextBlock, types.ErrUnexpectedIndex)
		}
		if err := s.db.PutBlock(record); err != nil {
			return fmt.Errorf("failed to put block %d: %w", index, err)
		}
		if !ok {
			// Reads below the first QC must return ErrPruned before and after reopening.
			s.setPruneWatermark(index)
		}
		current.NextBlock = index + 1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) WriteAppQC(appQC *types.AppQC) error {
	index := appQC.Proposal().Index()
	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok || current.NextAppProposal <= index {
			return fmt.Errorf("AppQC %d: %w", index, types.ErrMissingAppProposal)
		}
		if current.NextAppQC != index {
			return fmt.Errorf("AppQC.Index() = %d, want %d: %w",
				index, current.NextAppQC, types.ErrUnexpectedIndex)
		}

		if err := s.db.PutAppQC(blocktypes.AppQCRecord{
			Index: blocktypes.BlockNumber(index),
			Data:  encodeAppQC(appQC),
		}); err != nil {
			return fmt.Errorf("failed to put AppQC %d: %w", index, err)
		}
		current.First = index
		current.NextAppQC = index + 1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) WriteAppProposal(appProposal *types.AppProposal) error {
	index := appProposal.Index()
	if index == utils.Max[types.RoadIndex]() {
		return fmt.Errorf("road index cannot advance: %w", types.ErrUnexpectedIndex)
	}
	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok || current.NextBlock <= index {
			return fmt.Errorf("AppProposal %d: %w", index, types.ErrMissingQC)
		}
		if current.NextAppProposal != index {
			return fmt.Errorf("AppProposal.Index() = %v, want %v: %w",
				index, current.NextAppProposal, types.ErrUnexpectedIndex)
		}

		if err := s.db.PutAppProposal(blocktypes.AppProposalRecord{
			Index: blocktypes.BlockNumber(index),
			Data:  encodeAppProposal(appProposal),
		}); err != nil {
			return fmt.Errorf("failed to put AppProposal %d: %w", index, err)
		}
		current.NextAppProposal = index + 1
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
	// Keep the pruning floor fixed while reading its recovery suffix.
	for range s.status.Lock() {
		records, err := s.db.ReadSuffix()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("read recovery suffix: %w", err)
		}
		if len(records.AppQCs) > 1 {
			return types.Suffix{}, fmt.Errorf("too large suffix")
		}
		if len(records.Blocks) == 0 {
			if len(records.AppQCs) > 0 || len(records.AppProposals) > 0 {
				return types.Suffix{}, types.ErrMissingQC
			}
			return types.Suffix{}, nil
		}
		first := types.RoadIndex(records.Blocks[0].Index)
		status := types.SuffixRange{First: first, NextBlock: first, NextAppProposal: first, NextAppQC: first}
		var suffix types.Suffix
		for _, record := range records.Blocks {
			if types.RoadIndex(record.Index) != status.NextBlock {
				return types.Suffix{}, types.ErrUnexpectedIndex
			}
			group, err := decodeBlock(record)
			if err != nil {
				return types.Suffix{}, fmt.Errorf("decode SuperBlock %d: %w", record.Index, err)
			}
			suffix.Blocks = append(suffix.Blocks, group)
			status.NextBlock += 1
		}
		for _, record := range records.AppProposals {
			proposal, err := decodeAppProposal(record.Data)
			if err != nil {
				return types.Suffix{}, err
			}
			if proposal.Index() >= status.NextBlock {
				return types.Suffix{}, types.ErrMissingQC
			}
			if proposal.Index() != status.NextAppProposal {
				return types.Suffix{}, types.ErrUnexpectedIndex
			}
			suffix.AppProposals = append(suffix.AppProposals, proposal)
			status.NextAppProposal += 1
		}
		for _, record := range records.AppQCs {
			qc, err := decodeAppQC(record.Data)
			if err != nil {
				return types.Suffix{}, err
			}
			if qc.Proposal().Index() >= status.NextAppProposal {
				return types.Suffix{}, types.ErrMissingAppProposal
			}
			if qc.Proposal().Index() != status.NextAppQC {
				return types.Suffix{}, types.ErrUnexpectedIndex
			}
			suffix.AppQCs = append(suffix.AppQCs, qc)
			status.NextAppQC += 1
		}
		suffix.Status = utils.Some(status)
		return suffix, nil
	}
	panic("unreachable")
}

func (s *Store) ReadLaneBlock(pos types.LaneBlockPos) (utils.Option[*types.Block], error) {
	if blocktypes.BlockNumber(pos.Index) < s.db.GetPruneWatermark() {
		return utils.None[*types.Block](), types.ErrPruned
	}
	value, exists, err := s.db.GetLaneBlock(blocktypes.BlockNumber(pos.Index), pos.Offset)
	if err != nil {
		return utils.None[*types.Block](), err
	}
	if !exists {
		return utils.None[*types.Block](), nil
	}
	block, err := decodeLaneBlock(value)
	if err != nil {
		return utils.None[*types.Block](), err
	}
	return utils.Some(block), nil
}

func (s *Store) ReadBlock(n types.RoadIndex) (utils.Option[*types.RoadBlock], error) {
	if blocktypes.BlockNumber(n) < s.db.GetPruneWatermark() {
		return utils.None[*types.RoadBlock](), types.ErrPruned
	}
	record, exists, err := s.db.GetBlockByNumber(blocktypes.BlockNumber(n))
	if err != nil {
		return utils.None[*types.RoadBlock](), err
	}
	if !exists {
		return utils.None[*types.RoadBlock](), nil
	}
	group, err := decodeBlock(record)
	if err != nil {
		return utils.None[*types.RoadBlock](), err
	}
	return utils.Some(group), nil
}

func (s *Store) ReadBlockByHash(hash common.Hash) (utils.Option[*types.RoadBlock], error) {
	record, exists, err := s.db.GetBlockByHash(blocktypes.BlockHash(hash))
	if err != nil {
		return utils.None[*types.RoadBlock](), err
	}
	if !exists || record.Index < s.db.GetPruneWatermark() {
		return utils.None[*types.RoadBlock](), nil
	}
	if record.Hash != blocktypes.BlockHash(hash) {
		return utils.None[*types.RoadBlock](), types.ErrQCBlocksMismatch
	}
	group, err := decodeBlock(record)
	if err != nil {
		return utils.None[*types.RoadBlock](), err
	}
	return utils.Some(group), nil
}

func (s *Store) ReadQC(n types.RoadIndex) (utils.Option[*types.FullCommitQC], error) {
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

func (s *Store) ReadAppProposal(n types.RoadIndex) (utils.Option[*types.AppProposal], error) {
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

func (s *Store) ReadAppQC(n types.RoadIndex) (utils.Option[*types.AppQC], error) {
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
