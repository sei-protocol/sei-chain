package consensus

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// blockIDMatches reports whether block and parts together match the consensus
// BlockID (header hash and PartSetHeader). Consensus identity is the full
// BlockID; comparing only the header hash would treat different part-set
// encodings as the same value.
func blockIDMatches(block *types.Block, parts *types.PartSet, blockID types.BlockID) bool {
	if block == nil || parts == nil || blockID.IsNil() {
		return false
	}
	return block.HashesTo(blockID.Hash) && parts.HasHeader(blockID.PartSetHeader)
}

// proposalMatchesLocked reports whether the proposal block and parts equal the
// locked block identity (header hash and PartSetHeader).
func proposalMatchesLocked(proposal, locked *types.Block, proposalParts, lockedParts *types.PartSet) bool {
	if proposal == nil || locked == nil || proposalParts == nil || lockedParts == nil {
		return false
	}
	return proposal.HashesTo(locked.Hash()) && proposalParts.HasHeader(lockedParts.Header())
}

// verifyCanonicalProposalParts ensures the received part set is the canonical
// MakePartSet encoding of the assembled block. When the current parts still
// belong to the stored proposal (same PartSetHeader), also require the
// proposal BlockID hash to match. Parts may instead track a maj23/commit
// certificate whose PartSetHeader differs from the original proposal; in that
// case only the canonical-parts check applies.
func (cs *State) verifyCanonicalProposalParts(block *types.Block) error {
	parts := cs.roundState.ProposalBlockParts()
	if parts == nil {
		return fmt.Errorf("nil proposal block parts")
	}
	canonical, err := block.MakePartSet(types.BlockPartSizeBytes)
	if err != nil {
		return fmt.Errorf("MakePartSet: %w", err)
	}
	if !parts.HasHeader(canonical.Header()) {
		return fmt.Errorf(
			"assembled block PartSetHeader does not match canonical MakePartSet: got %v, want %v",
			parts.Header(), canonical.Header(),
		)
	}
	if proposal := cs.roundState.Proposal(); proposal != nil && parts.HasHeader(proposal.BlockID.PartSetHeader) {
		// parts already match canonical and proposal PartSetHeader, so only the
		// header hash can still disagree with the proposal BlockID.
		if !block.HashesTo(proposal.BlockID.Hash) {
			return fmt.Errorf(
				"assembled block hash does not match proposal BlockID.Hash: got %X, want %X",
				block.Hash(), proposal.BlockID.Hash,
			)
		}
	}
	return nil
}
