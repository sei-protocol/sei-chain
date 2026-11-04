package blockstore

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func (s *Store) WriteBlock(block *types.RoadBlock) error {
	index := block.QC().Index()

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
		record := encodeBlock(block)
		if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
			err = fmt.Errorf("failed to put ledger record: %w", err)
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

		record := encodeAppQC(appQC)
		if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
			err = fmt.Errorf("failed to put ledger record: %w", err)
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

		record := encodeAppProposal(appProposal)
		if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
			err = fmt.Errorf("failed to put ledger record: %w", err)
			return fmt.Errorf("failed to put AppProposal %d: %w", index, err)
		}
		current.NextAppProposal = index + 1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) ReadSuffix() (types.Suffix, error) { return s.readSnapshot(s.readSuffix) }

func (s *Store) ReadLaneBlock(pos types.LaneBlockPos) (utils.Option[*types.Block], error) {
	if pos.Index < s.First() {
		return utils.None[*types.Block](), types.ErrPruned
	}
	value, exists, err := s.getLaneBlock(pos.Index, pos.Offset)
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
	if n < s.First() {
		return utils.None[*types.RoadBlock](), types.ErrPruned
	}
	value, exists, err := s.getRecord((blockKey{Index: n}).encode())
	if err != nil {
		return utils.None[*types.RoadBlock](), err
	}
	if !exists {
		return utils.None[*types.RoadBlock](), nil
	}
	block, err := decodeBlock(value)
	if err != nil {
		return utils.None[*types.RoadBlock](), err
	}
	return utils.Some(block), nil
}

func (s *Store) ReadBlockByHash(hash types.RoadBlockHeaderHash) (utils.Option[*types.RoadBlock], error) {
	value, exists, err := s.getRecord((blockHashKey{Hash: hash}).encode())
	if err != nil {
		return utils.None[*types.RoadBlock](), err
	}
	if !exists {
		return utils.None[*types.RoadBlock](), nil
	}
	// Pruned hash lookups do not decode values that are no longer served.
	if len(value) >= 8 && types.RoadIndex(binary.BigEndian.Uint64(value)) < s.First() {
		return utils.None[*types.RoadBlock](), nil
	}
	block, err := decodeBlock(value)
	if err != nil {
		return utils.None[*types.RoadBlock](), err
	}
	if block.QC().Index() < s.First() {
		return utils.None[*types.RoadBlock](), nil
	}
	if block.Header().Hash() != hash {
		return utils.None[*types.RoadBlock](), types.ErrQCBlocksMismatch
	}
	return utils.Some(block), nil
}

func (s *Store) ReadQC(n types.RoadIndex) (utils.Option[*types.FullCommitQC], error) {
	// Below-watermark blocks are not served, so neither is their covering QC.
	if n < s.First() {
		return utils.None[*types.FullCommitQC](), types.ErrPruned
	}
	value, exists, err := s.getQC(n)
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
	if n < s.First() {
		return utils.None[*types.AppProposal](), types.ErrPruned
	}
	value, exists, err := s.getAppProposal(n)
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
	if n < s.First() {
		return utils.None[*types.AppQC](), types.ErrPruned
	}
	value, exists, err := s.getAppQC(n)
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
