package consensus

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// ErrNonCanonicalProposalParts is returned when assembled proposal parts do not
// use the default BlockPartSizeBytes chunking of the block's canonical
// protobuf encoding (i.e. when parts.Header() differs from MakePartSet).
var ErrNonCanonicalProposalParts = errors.New("non-canonical proposal parts")

// blockIDMatches reports whether block and parts together match blockID's
// header hash and PartSetHeader.
func blockIDMatches(block *types.Block, parts *types.PartSet, blockID types.BlockID) bool {
	if block == nil || parts == nil || !blockID.IsComplete() {
		return false
	}
	return block.HashesTo(blockID.Hash) && parts.HasHeader(blockID.PartSetHeader)
}

// proposalMatchesLocked reports whether the proposal block and parts equal the
// locked block identity (header hash and PartSetHeader).
func proposalMatchesLocked(proposal, locked *types.Block, proposalParts, lockedParts *types.PartSet) bool {
	if locked == nil || lockedParts == nil {
		return false
	}
	return blockIDMatches(proposal, proposalParts, types.BlockID{
		Hash:          locked.Hash(),
		PartSetHeader: lockedParts.Header(),
	})
}

// verifyCanonicalProposalParts reports an error unless ProposalBlockParts
// match block.MakePartSet(BlockPartSizeBytes).Header(). It does not compare
// Proposal.BlockID.Hash.
func (cs *State) verifyCanonicalProposalParts(block *types.Block) error {
	parts := cs.roundState.ProposalBlockParts()
	if parts == nil {
		return errors.New("nil proposal block parts")
	}
	canonicalParts, err := block.MakePartSet(types.BlockPartSizeBytes)
	if err != nil {
		return fmt.Errorf("MakePartSet: %w", err)
	}
	if !parts.HasHeader(canonicalParts.Header()) {
		return fmt.Errorf(
			"%w: PartSetHeader got %v, want canonical %v",
			ErrNonCanonicalProposalParts, parts.Header(), canonicalParts.Header(),
		)
	}
	return nil
}
