package types

import (
	"github.com/coinbase/kryptology/pkg/bulletproof"
	"github.com/coinbase/kryptology/pkg/core/curves"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

func (c *TransferMsgProofs) Validate() error {
	if c.RemainingBalanceCommitmentValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "remaining balance commitment validity proof is required")
	}

	if c.SenderTransferAmountLoValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "sender transfer amount lo validity proof is required")
	}

	if c.SenderTransferAmountHiValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "sender transfer amount hi validity proof is required")
	}

	if c.RecipientTransferAmountLoValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "recipient transfer amount lo validity proof is required")
	}

	if c.RecipientTransferAmountHiValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "recipient transfer amount hi validity proof is required")
	}

	if c.RemainingBalanceRangeProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "remaining balance range proof is required")
	}

	if c.RemainingBalanceEqualityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "remaining balance equality proof is required")
	}

	if c.TransferAmountLoEqualityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "transfer amount lo equality proof is required")
	}

	if c.TransferAmountHiEqualityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "transfer amount hi equality proof is required")
	}

	return nil
}

func NewTransferMsgProofs(proofs *TransferProofs) *TransferMsgProofs {
	return &TransferMsgProofs{
		RemainingBalanceCommitmentValidityProof: NewCiphertextValidityProofProto(proofs.RemainingBalanceCommitmentValidityProof),
		SenderTransferAmountLoValidityProof:     NewCiphertextValidityProofProto(proofs.SenderTransferAmountLoValidityProof),
		SenderTransferAmountHiValidityProof:     NewCiphertextValidityProofProto(proofs.SenderTransferAmountHiValidityProof),
		RecipientTransferAmountLoValidityProof:  NewCiphertextValidityProofProto(proofs.RecipientTransferAmountLoValidityProof),
		RecipientTransferAmountHiValidityProof:  NewCiphertextValidityProofProto(proofs.RecipientTransferAmountHiValidityProof),
		RemainingBalanceRangeProof:              NewRangeProofProto(proofs.RemainingBalanceRangeProof),
		RemainingBalanceEqualityProof:           NewCiphertextCommitmentEqualityProofProto(proofs.RemainingBalanceEqualityProof),
		TransferAmountLoEqualityProof:           NewCiphertextCiphertextEqualityProofProto(proofs.TransferAmountLoEqualityProof),
		TransferAmountHiEqualityProof:           NewCiphertextCiphertextEqualityProofProto(proofs.TransferAmountHiEqualityProof),
	}
}

func (c *TransferMsgProofs) FromProto() (*TransferProofs, error) {
	err := c.Validate()
	if err != nil {
		return nil, err
	}
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

	return &TransferProofs{
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

func (c *InitializeAccountMsgProofs) Validate() error {
	if c.PubkeyValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pubkey validity proof is required")
	}

	if c.ZeroPendingBalanceLoProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "zero pending balance lo proof is required")
	}

	if c.ZeroPendingBalanceHiProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "zero pending balance hi proof is required")
	}

	if c.ZeroAvailableBalanceProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "zero available balance proof is required")
	}

	return nil
}

func NewInitializeAccountMsgProofs(proofs *InitializeAccountProofs) *InitializeAccountMsgProofs {
	return &InitializeAccountMsgProofs{
		PubkeyValidityProof:       NewPubkeyValidityProofProto(proofs.PubkeyValidityProof),
		ZeroPendingBalanceLoProof: NewZeroBalanceProofProto(proofs.ZeroPendingBalanceLoProof),
		ZeroPendingBalanceHiProof: NewZeroBalanceProofProto(proofs.ZeroPendingBalanceHiProof),
		ZeroAvailableBalanceProof: NewZeroBalanceProofProto(proofs.ZeroAvailableBalanceProof),
	}
}

func (c *InitializeAccountMsgProofs) FromProto() (*InitializeAccountProofs, error) {
	err := c.Validate()
	if err != nil {
		return nil, err
	}

	pubkeyValidityProof, err := c.PubkeyValidityProof.FromProto()
	if err != nil {
		return nil, err
	}

	zeroPendingBalanceLoProof, err := c.ZeroPendingBalanceLoProof.FromProto()
	if err != nil {
		return nil, err
	}

	zeroPendingBalanceHiProof, err := c.ZeroPendingBalanceHiProof.FromProto()
	if err != nil {
		return nil, err
	}

	zeroAvailableBalanceProof, err := c.ZeroAvailableBalanceProof.FromProto()
	if err != nil {
		return nil, err
	}

	return &InitializeAccountProofs{
		PubkeyValidityProof:       pubkeyValidityProof,
		ZeroPendingBalanceLoProof: zeroPendingBalanceLoProof,
		ZeroPendingBalanceHiProof: zeroPendingBalanceHiProof,
		ZeroAvailableBalanceProof: zeroAvailableBalanceProof,
	}, nil
}

func (w *WithdrawMsgProofs) FromProto() (*WithdrawProofs, error) {
	err := w.Validate()
	if err != nil {
		return nil, err
	}

	remainingBalanceRangeProof, err := w.RemainingBalanceRangeProof.FromProto()
	if err != nil {
		return nil, err
	}

	remainingBalanceEqualityProof, err := w.RemainingBalanceEqualityProof.FromProto()
	if err != nil {
		return nil, err
	}

	return &WithdrawProofs{
		RemainingBalanceRangeProof:    remainingBalanceRangeProof,
		RemainingBalanceEqualityProof: remainingBalanceEqualityProof,
	}, nil
}

func (w *WithdrawMsgProofs) Validate() error {
	if w.RemainingBalanceRangeProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "remaining balance range proof is required")
	}

	if w.RemainingBalanceEqualityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "remaining balance equality proof is required")
	}

	return nil
}

func (w *WithdrawMsgProofs) NewWithdrawMsgProofs(proofs *WithdrawProofs) *WithdrawMsgProofs {
	return &WithdrawMsgProofs{
		RemainingBalanceRangeProof:    NewRangeProofProto(proofs.RemainingBalanceRangeProof),
		RemainingBalanceEqualityProof: NewCiphertextCommitmentEqualityProofProto(proofs.RemainingBalanceEqualityProof),
	}
}

func (c *CiphertextValidityProof) Validate() error {
	if c.Commitment_1 == nil || c.Commitment_2 == nil || c.Response_1 == nil || c.Response_2 == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "ciphertext validity proof is invalid")
	}
	return nil
}

func NewCiphertextValidityProofProto(zkp *zkproofs.CiphertextValidityProof) *CiphertextValidityProof {
	return &CiphertextValidityProof{
		Commitment_1: zkp.Commitment1.ToAffineCompressed(),
		Commitment_2: zkp.Commitment2.ToAffineCompressed(),
		Response_1:   zkp.Response1.Bytes(),
		Response_2:   zkp.Response2.Bytes(),
	}
}

func (c *CiphertextValidityProof) FromProto() (*zkproofs.CiphertextValidityProof, error) {
	err := c.Validate()
	if err != nil {
		return nil, err
	}
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

func (r *RangeProof) Validate() error {
	if r.Proof == nil || r.Randomness == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "range proof is invalid")
	}
	return nil
}

func NewRangeProofProto(zkp *zkproofs.RangeProof) *RangeProof {
	return &RangeProof{
		Proof:      zkp.Proof.MarshalBinary(),
		Randomness: zkp.Randomness.ToAffineCompressed(),
		UpperBound: int64(zkp.UpperBound),
	}
}

func (r *RangeProof) FromProto() (*zkproofs.RangeProof, error) {
	err := r.Validate()
	if err != nil {
		return nil, err
	}
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

func (c *CiphertextCommitmentEqualityProof) Validate() error {
	if c.Y0 == nil || c.Y1 == nil || c.Y2 == nil || c.Zr == nil || c.Zs == nil || c.Zx == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "ciphertext commitment equality proof is invalid")
	}
	return nil
}

func NewCiphertextCommitmentEqualityProofProto(zkp *zkproofs.CiphertextCommitmentEqualityProof) *CiphertextCommitmentEqualityProof {
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
	err := c.Validate()
	if err != nil {
		return nil, err
	}
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

func (c *CiphertextCiphertextEqualityProof) Validate() error {
	if c.Y0 == nil || c.Y1 == nil || c.Y2 == nil || c.Y3 == nil || c.Zr == nil || c.Zs == nil || c.Zx == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "ciphertext ciphertext equality proof is invalid")
	}
	return nil
}

func NewCiphertextCiphertextEqualityProofProto(zkp *zkproofs.CiphertextCiphertextEqualityProof) *CiphertextCiphertextEqualityProof {
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
	err := c.Validate()
	if err != nil {
		return nil, err
	}
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

func (a *Auditor) Validate() error {
	if a.EncryptedTransferAmountLo == nil || a.EncryptedTransferAmountHi == nil || a.TransferAmountLoValidityProof == nil || a.TransferAmountHiValidityProof == nil || a.TransferAmountLoEqualityProof == nil || a.TransferAmountHiEqualityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "auditor is invalid")
	}
	return nil
}

func NewAuditorProto(transferAuditor *TransferAuditor) *Auditor {
	transferAmountLo := NewCiphertextProto(transferAuditor.EncryptedTransferAmountLo)
	transferAmountHi := NewCiphertextProto(transferAuditor.EncryptedTransferAmountHi)
	transferAmountLoValidityProof := NewCiphertextValidityProofProto(transferAuditor.TransferAmountLoValidityProof)
	transferAmountHiValidityProof := NewCiphertextValidityProofProto(transferAuditor.TransferAmountHiValidityProof)
	transferAmountLoEqualityProof := NewCiphertextCiphertextEqualityProofProto(transferAuditor.TransferAmountLoEqualityProof)
	transferAmountHiEqualityProof := NewCiphertextCiphertextEqualityProofProto(transferAuditor.TransferAmountHiEqualityProof)
	return &Auditor{
		AuditorAddress:                transferAuditor.Address,
		EncryptedTransferAmountLo:     transferAmountLo,
		EncryptedTransferAmountHi:     transferAmountHi,
		TransferAmountLoValidityProof: transferAmountLoValidityProof,
		TransferAmountHiValidityProof: transferAmountHiValidityProof,
		TransferAmountLoEqualityProof: transferAmountLoEqualityProof,
		TransferAmountHiEqualityProof: transferAmountHiEqualityProof,
	}
}

func (a *Auditor) FromProto() (*TransferAuditor, error) {
	err := a.Validate()
	if err != nil {
		return nil, err
	}
	transferAmountLo, err := a.EncryptedTransferAmountLo.FromProto()
	if err != nil {
		return nil, err
	}
	transferAmountHi, err := a.EncryptedTransferAmountHi.FromProto()
	if err != nil {
		return nil, err
	}
	transferAmountLoValidityProof, err := a.TransferAmountLoValidityProof.FromProto()
	if err != nil {
		return nil, err
	}
	transferAmountHiValidityProof, err := a.TransferAmountHiValidityProof.FromProto()
	if err != nil {
		return nil, err
	}
	transferAmountLoEqualityProof, err := a.TransferAmountLoEqualityProof.FromProto()
	if err != nil {
		return nil, err
	}
	transferAmountHiEqualityProof, err := a.TransferAmountLoEqualityProof.FromProto()
	if err != nil {
		return nil, err
	}
	return &TransferAuditor{
		Address:                       a.AuditorAddress,
		EncryptedTransferAmountLo:     transferAmountLo,
		EncryptedTransferAmountHi:     transferAmountHi,
		TransferAmountLoValidityProof: transferAmountLoValidityProof,
		TransferAmountHiValidityProof: transferAmountHiValidityProof,
		TransferAmountLoEqualityProof: transferAmountLoEqualityProof,
		TransferAmountHiEqualityProof: transferAmountHiEqualityProof,
	}, nil
}

func (p *PubkeyValidityProof) Validate() error {
	if p.Y == nil || p.Z == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "pubkey validity proof is invalid")
	}
	return nil
}

func (z *ZeroBalanceProof) Validate() error {
	if z.YP == nil || z.YD == nil || z.Z == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "zero proof is invalid")
	}
	return nil
}

func NewPubkeyValidityProofProto(zkp *zkproofs.PubKeyValidityProof) *PubkeyValidityProof {
	return &PubkeyValidityProof{
		Y: zkp.Y.ToAffineCompressed(),
		Z: zkp.Z.Bytes(),
	}
}

func (p *PubkeyValidityProof) FromProto() (*zkproofs.PubKeyValidityProof, error) {
	err := p.Validate()
	if err != nil {
		return nil, err
	}
	ed25519Curve := curves.ED25519()
	y, err := ed25519Curve.Point.FromAffineCompressed(p.Y)
	if err != nil {
		return nil, err
	}

	z, err := ed25519Curve.Scalar.SetBytes(p.Z)
	if err != nil {
		return nil, err
	}

	return &zkproofs.PubKeyValidityProof{
		Y: y,
		Z: z,
	}, nil
}

func NewZeroBalanceProofProto(zkp *zkproofs.ZeroBalanceProof) *ZeroBalanceProof {
	return &ZeroBalanceProof{
		YP: zkp.Yp.ToAffineCompressed(),
		YD: zkp.Yd.ToAffineCompressed(),
		Z:  zkp.Z.Bytes(),
	}
}

func (z *ZeroBalanceProof) FromProto() (*zkproofs.ZeroBalanceProof, error) {
	err := z.Validate()
	if err != nil {
		return nil, err
	}
	ed25519Curve := curves.ED25519()

	yp, err := ed25519Curve.Point.FromAffineCompressed(z.YP)
	if err != nil {
		return nil, err
	}

	yd, err := ed25519Curve.Point.FromAffineCompressed(z.YD)
	if err != nil {
		return nil, err
	}

	zScalar, err := ed25519Curve.Scalar.SetBytes(z.Z)
	if err != nil {
		return nil, err
	}

	return &zkproofs.ZeroBalanceProof{
		Yp: yp,
		Yd: yd,
		Z:  zScalar,
	}, nil
}

func (c *CloseAccountMsgProofs) Validate() error {
	if c.ZeroAvailableBalanceProof == nil || c.ZeroPendingBalanceLoProof == nil || c.ZeroPendingBalanceHiProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "close account proof is invalid")
	}
	return nil
}

func NewCloseAccountMsgProofs(proofs *CloseAccountProofs) *CloseAccountMsgProofs {
	return &CloseAccountMsgProofs{
		ZeroAvailableBalanceProof: NewZeroBalanceProofProto(proofs.ZeroAvailableBalanceProof),
		ZeroPendingBalanceLoProof: NewZeroBalanceProofProto(proofs.ZeroPendingBalanceLoProof),
		ZeroPendingBalanceHiProof: NewZeroBalanceProofProto(proofs.ZeroPendingBalanceHiProof),
	}
}

func (c *CloseAccountMsgProofs) FromProto() (*CloseAccountProofs, error) {
	err := c.Validate()
	if err != nil {
		return nil, err
	}
	zeroAvailableBalanceProof, err := c.ZeroAvailableBalanceProof.FromProto()
	if err != nil {
		return nil, err
	}

	zeroPendingBalanceLoProof, err := c.ZeroPendingBalanceLoProof.FromProto()
	if err != nil {
		return nil, err
	}

	zeroPendingBalanceHiProof, err := c.ZeroPendingBalanceHiProof.FromProto()
	if err != nil {
		return nil, err
	}

	return &CloseAccountProofs{
		ZeroAvailableBalanceProof: zeroAvailableBalanceProof,
		ZeroPendingBalanceLoProof: zeroPendingBalanceLoProof,
		ZeroPendingBalanceHiProof: zeroPendingBalanceHiProof,
	}, nil
}
