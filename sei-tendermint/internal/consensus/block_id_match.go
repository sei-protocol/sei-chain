package consensus

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// ErrNonCanonicalProposalParts is returned when assembled proposal parts are
// not the canonical protobuf encoding of the decoded block, or when the
// proposal BlockID hash disagrees with that block while parts still target the
// proposal PartSetHeader.
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

// verifyCanonicalProposalParts ensures the received part bytes are exactly the
// canonical protobuf encoding of the assembled block (equivalent to matching
// MakePartSet's PartSetHeader, without rebuilding the Merkle tree). When the
// current parts still belong to the stored proposal (same PartSetHeader), also
// require the proposal BlockID hash to match. Parts may instead track a
// maj23/commit certificate whose PartSetHeader differs from the original
// proposal; in that case only the canonical-bytes check applies.
func (cs *State) verifyCanonicalProposalParts(block *types.Block, partsBytes []byte) error {
	parts := cs.roundState.ProposalBlockParts()
	if parts == nil {
		return errors.New("nil proposal block parts")
	}
	pbb, err := block.ToProto()
	if err != nil {
		return fmt.Errorf("block.ToProto: %w", err)
	}
	canonical, err := proto.Marshal(pbb)
	if err != nil {
		return fmt.Errorf("proto.Marshal: %w", err)
	}
	if !bytes.Equal(partsBytes, canonical) {
		return fmt.Errorf(
			"%w: assembled block bytes are not the canonical encoding (len got %d, want %d; PartSetHeader got %v)",
			ErrNonCanonicalProposalParts, len(partsBytes), len(canonical), parts.Header(),
		)
	}
	if proposal := cs.roundState.Proposal(); proposal != nil && parts.HasHeader(proposal.BlockID.PartSetHeader) {
		// parts already match canonical encoding and proposal PartSetHeader,
		// so only the header hash can still disagree with the proposal BlockID.
		if !block.HashesTo(proposal.BlockID.Hash) {
			return fmt.Errorf(
				"%w: assembled block hash does not match proposal BlockID.Hash: got %X, want %X",
				ErrNonCanonicalProposalParts, block.Hash(), proposal.BlockID.Hash,
			)
		}
	}
	return nil
}
