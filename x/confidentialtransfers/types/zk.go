package types

import (
	"github.com/coinbase/kryptology/pkg/bulletproof"
	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

func (c *TransferProofs) Validate() error {
	return nil
}

func (c *TransferProofs) ToProto(proofs *Proofs) *TransferProofs {
	return &TransferProofs{
		RemainingBalanceCommitmentValidityProof: c.RemainingBalanceCommitmentValidityProof.ToProto(proofs.RemainingBalanceCommitmentValidityProof),
		SenderTransferAmountLoValidityProof:     c.RemainingBalanceCommitmentValidityProof.ToProto(proofs.SenderTransferAmountLoValidityProof),
		SenderTransferAmountHiValidityProof:     c.RemainingBalanceCommitmentValidityProof.ToProto(proofs.SenderTransferAmountHiValidityProof),
		RecipientTransferAmountLoValidityProof:  c.RemainingBalanceCommitmentValidityProof.ToProto(proofs.RecipientTransferAmountLoValidityProof),
		RecipientTransferAmountHiValidityProof:  c.RemainingBalanceCommitmentValidityProof.ToProto(proofs.RecipientTransferAmountHiValidityProof),
		RemainingBalanceRangeProof:              c.RemainingBalanceRangeProof.ToProto(proofs.RemainingBalanceRangeProof),
	}
}

func (c *TransferProofs) FromProto() (*Proofs, error) {
	remainingBalanceCommitmentValidityProof, err := c.RemainingBalanceCommitmentValidityProof.FromProto()
	if err != nil {
		return nil, err
	}

	senderTransferAmountLoValidityProof, err := c.SenderTransferAmountLoValidityProof.FromProto()
	if err != nil {
		return nil, err
	}

	senderTransferAmountHiValidityProof, err := c.SenderTransferAmountHiValidityProof.FromProto()
	if err != nil {
		return nil, err
	}

	recipientTransferAmountLoValidityProof, err := c.RecipientTransferAmountLoValidityProof.FromProto()
	if err != nil {
		return nil, err
	}

	recipientTransferAmountHiValidityProof, err := c.RecipientTransferAmountHiValidityProof.FromProto()
	if err != nil {
		return nil, err
	}

	remainingBalanceRangeProof, err := c.RemainingBalanceRangeProof.FromProto()

	return &Proofs{
		RemainingBalanceCommitmentValidityProof: remainingBalanceCommitmentValidityProof,
		SenderTransferAmountLoValidityProof:     senderTransferAmountLoValidityProof,
		SenderTransferAmountHiValidityProof:     senderTransferAmountHiValidityProof,
		RecipientTransferAmountLoValidityProof:  recipientTransferAmountLoValidityProof,
		RecipientTransferAmountHiValidityProof:  recipientTransferAmountHiValidityProof,
		RemainingBalanceRangeProof:              remainingBalanceRangeProof,
	}, nil
}

func (c *CiphertextValidityProof) ToProto(zkp *zkproofs.CiphertextValidityProof) *CiphertextValidityProof {
	return &CiphertextValidityProof{
		Commitment_1: zkp.Commitment1.ToAffineCompressed(),
		Commitment_2: zkp.Commitment2.ToAffineCompressed(),
		Response_1:   zkp.Response1.Bytes(),
		Response_2:   zkp.Response2.Bytes(),
	}
}

func (c *CiphertextValidityProof) FromProto() (*zkproofs.CiphertextValidityProof, error) {
	ed25519Curve := curves.ED25519()

	c1, err := ed25519Curve.Point.FromAffineCompressed(c.Commitment_1)
	if err != nil {
		return nil, err
	}

	c2, err := ed25519Curve.Point.FromAffineCompressed(c.Commitment_2)
	if err != nil {
		return nil, err
	}

	r1, err := ed25519Curve.Scalar.SetBytes(c.Response_1)
	if err != nil {
		return nil, err
	}

	r2, err := ed25519Curve.Scalar.SetBytes(c.Response_2)
	if err != nil {
		return nil, err
	}
	return &zkproofs.CiphertextValidityProof{
		Commitment1: c1,
		Commitment2: c2,
		Response1:   r1,
		Response2:   r2,
	}, nil
}

func (r *RangeProof) ToProto(zkp *zkproofs.RangeProof) *RangeProof {
	return &RangeProof{
		Proof:      zkp.Proof.MarshalBinary(),
		Randomness: zkp.Randomness.ToAffineCompressed(),
		UpperBound: int64(zkp.UpperBound),
	}
}

func (r *RangeProof) FromProto() (*zkproofs.RangeProof, error) {
	ed25519Curve := curves.ED25519()
	proof := bulletproof.NewRangeProof(ed25519Curve)

	// Unmarshal the proof using the UnmarshalBinary method, which will populate all fields
	if err := proof.UnmarshalBinary(r.Proof); err != nil {
		return nil, err
	}

	randomness, err := ed25519Curve.Point.FromAffineCompressed(r.Randomness)
	if err != nil {
		return nil, err
	}

	return &zkproofs.RangeProof{
		Proof:      proof,
		Randomness: randomness,
		UpperBound: int(r.UpperBound),
	}, nil
}
