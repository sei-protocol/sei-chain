package types

import (
	"errors"
	"github.com/coinbase/kryptology/pkg/bulletproof"
	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

func (c *TransferProofs) Validate() error {

	if c.RemainingBalanceCommitmentValidityProof == nil {
		return errors.New("remaining balance commitment validity proof is nil")
	}

	if c.SenderTransferAmountLoValidityProof == nil {
		return errors.New("sender transfer amount lo validity proof is nil")
	}

	if c.SenderTransferAmountHiValidityProof == nil {
		return errors.New("sender transfer amount hi validity proof is nil")
	}

	if c.RecipientTransferAmountLoValidityProof == nil {
		return errors.New("recipient transfer amount lo validity proof is nil")
	}

	if c.RecipientTransferAmountHiValidityProof == nil {
		return errors.New("recipient transfer amount hi validity proof is nil")
	}

	if c.RemainingBalanceRangeProof == nil {
		return errors.New("remaining balance range proof is nil")
	}

	if c.RemainingBalanceEqualityProof == nil {
		return errors.New("remaining balance equality proof is nil")
	}

	if c.TransferAmountLoEqualityProof == nil {
		return errors.New("transfer amount lo equality proof is nil")
	}

	if c.TransferAmountHiEqualityProof == nil {
		return errors.New("transfer amount hi equality proof is nil")
	}

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
		RemainingBalanceEqualityProof:           c.RemainingBalanceEqualityProof.ToProto(proofs.RemainingBalanceEqualityProof),
		TransferAmountLoEqualityProof:           c.TransferAmountLoEqualityProof.ToProto(proofs.TransferAmountLoEqualityProof),
		TransferAmountHiEqualityProof:           c.TransferAmountHiEqualityProof.ToProto(proofs.TransferAmountHiEqualityProof),
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
	if err != nil {
		return nil, err
	}

	remainingBalanceEqualityProof, err := c.RemainingBalanceEqualityProof.FromProto()
	if err != nil {
		return nil, err
	}

	transferAmountLoEqualityProof, err := c.TransferAmountLoEqualityProof.FromProto()
	if err != nil {
		return nil, err
	}

	transferAmountHiEqualityProof, err := c.TransferAmountHiEqualityProof.FromProto()
	if err != nil {
		return nil, err
	}

	return &Proofs{
		RemainingBalanceCommitmentValidityProof: remainingBalanceCommitmentValidityProof,
		SenderTransferAmountLoValidityProof:     senderTransferAmountLoValidityProof,
		SenderTransferAmountHiValidityProof:     senderTransferAmountHiValidityProof,
		RecipientTransferAmountLoValidityProof:  recipientTransferAmountLoValidityProof,
		RecipientTransferAmountHiValidityProof:  recipientTransferAmountHiValidityProof,
		RemainingBalanceRangeProof:              remainingBalanceRangeProof,
		RemainingBalanceEqualityProof:           remainingBalanceEqualityProof,
		TransferAmountLoEqualityProof:           transferAmountLoEqualityProof,
		TransferAmountHiEqualityProof:           transferAmountHiEqualityProof,
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

func (c *CiphertextCommitmentEqualityProof) ToProto(zkp *zkproofs.CiphertextCommitmentEqualityProof) *CiphertextCommitmentEqualityProof {
	return &CiphertextCommitmentEqualityProof{
		Y0: zkp.Y0.ToAffineCompressed(),
		Y1: zkp.Y1.ToAffineCompressed(),
		Y2: zkp.Y2.ToAffineCompressed(),
		Zr: zkp.Zr.Bytes(),
		Zs: zkp.Zs.Bytes(),
		Zx: zkp.Zx.Bytes(),
	}
}

func (c *CiphertextCommitmentEqualityProof) FromProto() (*zkproofs.CiphertextCommitmentEqualityProof, error) {
	ed25519Curve := curves.ED25519()

	y0, err := ed25519Curve.Point.FromAffineCompressed(c.Y0)
	if err != nil {
		return nil, err
	}

	y1, err := ed25519Curve.Point.FromAffineCompressed(c.Y1)
	if err != nil {
		return nil, err
	}

	y2, err := ed25519Curve.Point.FromAffineCompressed(c.Y2)
	if err != nil {
		return nil, err
	}

	zR, err := ed25519Curve.Scalar.SetBytes(c.Zr)
	if err != nil {
		return nil, err
	}

	zS, err := ed25519Curve.Scalar.SetBytes(c.Zs)
	if err != nil {
		return nil, err
	}

	zX, err := ed25519Curve.Scalar.SetBytes(c.Zx)
	if err != nil {
		return nil, err
	}

	return &zkproofs.CiphertextCommitmentEqualityProof{
		Y0: y0,
		Y1: y1,
		Y2: y2,
		Zr: zR,
		Zs: zS,
		Zx: zX,
	}, nil
}

func (c *CiphertextCiphertextEqualityProof) ToProto(zkp *zkproofs.CiphertextCiphertextEqualityProof) *CiphertextCiphertextEqualityProof {
	return &CiphertextCiphertextEqualityProof{
		Y0: zkp.Y0.ToAffineCompressed(),
		Y1: zkp.Y1.ToAffineCompressed(),
		Y2: zkp.Y2.ToAffineCompressed(),
		Y3: zkp.Y3.ToAffineCompressed(),
		Zr: zkp.Zr.Bytes(),
		Zs: zkp.Zs.Bytes(),
		Zx: zkp.Zx.Bytes(),
	}
}

func (c *CiphertextCiphertextEqualityProof) FromProto() (*zkproofs.CiphertextCiphertextEqualityProof, error) {
	ed25519Curve := curves.ED25519()

	y0, err := ed25519Curve.Point.FromAffineCompressed(c.Y0)
	if err != nil {
		return nil, err
	}

	y1, err := ed25519Curve.Point.FromAffineCompressed(c.Y1)
	if err != nil {
		return nil, err
	}

	y2, err := ed25519Curve.Point.FromAffineCompressed(c.Y2)
	if err != nil {
		return nil, err
	}

	y3, err := ed25519Curve.Point.FromAffineCompressed(c.Y3)
	if err != nil {
		return nil, err
	}

	zR, err := ed25519Curve.Scalar.SetBytes(c.Zr)
	if err != nil {
		return nil, err
	}

	zS, err := ed25519Curve.Scalar.SetBytes(c.Zs)
	if err != nil {
		return nil, err
	}

	zX, err := ed25519Curve.Scalar.SetBytes(c.Zx)
	if err != nil {
		return nil, err
	}

	return &zkproofs.CiphertextCiphertextEqualityProof{
		Y0: y0,
		Y1: y1,
		Y2: y2,
		Y3: y3,
		Zr: zR,
		Zs: zS,
		Zx: zX,
	}, nil
}
