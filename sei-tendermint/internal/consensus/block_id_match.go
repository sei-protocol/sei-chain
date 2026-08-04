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

// blockIDMatches reports whether block and parts together match the consensus
// BlockID (header hash and PartSetHeader). Consensus identity is the full
// BlockID; comparing only the header hash would treat different part-set
// encodings as the same value.
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

// verifyCanonicalProposalParts ensures ProposalBlockParts match
// block.MakePartSet(BlockPartSizeBytes). Parts that carry the same logical
// block bytes under a different chunk size produce a different PartSetHeader;
// those must be rejected so commit/blocksync (which rebuild with
// BlockPartSizeBytes) stay consistent. Proposal.BlockID.Hash is not checked
// here: a mismatched proposal hash must not block later maj23/commit catch-up
// that retargets the same PartSetHeader, and votes already commit to
// ProposalBlock.Hash() + parts.Header().
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
