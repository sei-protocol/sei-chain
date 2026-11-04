package blockstore

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

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
	for {
		current := s.first.Load()
		if uint64(n) <= current {
			return
		}
		if s.first.CompareAndSwap(current, uint64(n)) {
			Global.pruneWatermarkAt().Set(utils.Clamp[int64](n))
			return
		}
	}
}

func (s *Store) First() types.RoadIndex {
	return types.RoadIndex(s.first.Load())
}

func (s *Store) Status() utils.Option[types.SuffixRange] {
	for status := range s.status.Lock() {
		return *status
	}
	panic("unreachable")
}

// readSnapshot returns a snapshot while writes and pruning are excluded.
func (s *Store) readSnapshot(read func() (types.Suffix, error)) (types.Suffix, error) {
	for range s.status.Lock() {
		suffix, err := read()
		if err != nil {
			return types.Suffix{}, err
		}
		return validateSuffix(suffix)
	}
	panic("unreachable")
}

// restore initializes the cursors and floor from retained records.
func (s *Store) restore(first types.RoadIndex, suffix types.Suffix) error {
	suffix, err := validateSuffix(suffix)
	if err != nil {
		return err
	}
	for status := range s.status.Lock() {
		*status = suffix.Status
		s.first.Store(uint64(first))
		Global.pruneWatermarkAt().Set(utils.Clamp[int64](first))
	}
	return nil
}

// validateSuffix checks that recovery streams are contiguous and have their prerequisites.
func validateSuffix(suffix types.Suffix) (types.Suffix, error) {
	if len(suffix.AppQCs) > 1 {
		return types.Suffix{}, fmt.Errorf("too large suffix")
	}
	if len(suffix.Blocks) == 0 {
		if len(suffix.AppQCs) > 0 || len(suffix.AppProposals) > 0 {
			return types.Suffix{}, types.ErrMissingQC
		}
		suffix.Status = utils.None[types.SuffixRange]()
		return suffix, nil
	}
	first := suffix.Blocks[0].QC().Index()
	status := types.SuffixRange{First: first, NextBlock: first, NextAppProposal: first, NextAppQC: first}
	for _, block := range suffix.Blocks {
		if block.QC().Index() != status.NextBlock {
			return types.Suffix{}, types.ErrUnexpectedIndex
		}
		status.NextBlock++
	}
	for _, proposal := range suffix.AppProposals {
		if proposal.Index() >= status.NextBlock {
			return types.Suffix{}, types.ErrMissingQC
		}
		if proposal.Index() != status.NextAppProposal {
			return types.Suffix{}, types.ErrUnexpectedIndex
		}
		status.NextAppProposal++
	}
	for _, qc := range suffix.AppQCs {
		index := qc.Proposal().Index()
		if index >= status.NextAppProposal {
			return types.Suffix{}, types.ErrMissingAppProposal
		}
		if index != status.NextAppQC {
			return types.Suffix{}, types.ErrUnexpectedIndex
		}
		status.First = index
		status.NextAppQC++
	}
	suffix.Status = utils.Some(status)
	return suffix, nil
}
