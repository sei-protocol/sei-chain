package types

import (
	ics23 "github.com/confio/ics23/go"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	crypto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/crypto"
)

// ConvertProofs converts crypto.ProofOps into MerkleProof
func ConvertProofs(tmProof *crypto.ProofOps) (MerkleProof, error) {
	if tmProof == nil {
		return MerkleProof{}, sdkerrors.Wrapf(ErrInvalidMerkleProof, "tendermint proof is nil")
	}
	// Unmarshal all proof ops to CommitmentProof
	proofs := make([]*ics23.CommitmentProof, len(tmProof.Ops))
	for i, op := range tmProof.Ops {
		var p ics23.CommitmentProof
		err := p.Unmarshal(op.Data)
		if err != nil || p.Proof == nil {
			return MerkleProof{}, sdkerrors.Wrapf(ErrInvalidMerkleProof, "could not unmarshal proof op into CommitmentProof at index %d: %v", i, err)
		}
		proofs[i] = &p
	}
	return MerkleProof{
		Proofs: proofs,
	}, nil
}
