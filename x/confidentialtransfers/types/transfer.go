package types

import (
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type Transfer struct {
	FromAddress                string              `json:"from_address"`
	ToAddress                  string              `json:"to_address"`
	Denom                      string              `json:"denom"`
	SenderTransferAmountLo     *elgamal.Ciphertext `json:"sender_transfer_amount_lo"`
	SenderTransferAmountHi     *elgamal.Ciphertext `json:"sender_transfer_amount_hi"`
	RecipientTransferAmountLo  *elgamal.Ciphertext `json:"recipient_transfer_amount_lo"`
	RecipientTransferAmountHi  *elgamal.Ciphertext `json:"recipient_transfer_amount_hi"`
	RemainingBalanceCommitment *elgamal.Ciphertext `json:"remaining_balance_commitment"`
	DecryptableBalance         string              `json:"decryptable_balance"`
	Proofs                     *Proofs             `json:"proofs"`
	Auditors                   []*TransferAuditor  `json:"auditors,omitempty"` //optional field
}

type Proofs struct {
	RemainingBalanceCommitmentValidityProof *zkproofs.CiphertextValidityProof           `json:"remaining_balance_commitment_validity_proof"`
	SenderTransferAmountLoValidityProof     *zkproofs.CiphertextValidityProof           `json:"sender_transfer_amount_lo_validity_proof"`
	SenderTransferAmountHiValidityProof     *zkproofs.CiphertextValidityProof           `json:"sender_transfer_amount_hi_validity_proof"`
	RecipientTransferAmountLoValidityProof  *zkproofs.CiphertextValidityProof           `json:"recipient_transfer_amount_lo_validity_proof"`
	RecipientTransferAmountHiValidityProof  *zkproofs.CiphertextValidityProof           `json:"recipient_transfer_amount_hi_validity_proof"`
	RemainingBalanceRangeProof              *zkproofs.RangeProof                        `json:"remaining_balance_range_proof"`
	RemainingBalanceEqualityProof           *zkproofs.CiphertextCommitmentEqualityProof `json:"remaining_balance_equality_proof"`
	TransferAmountLoEqualityProof           *zkproofs.CiphertextCiphertextEqualityProof `json:"transfer_amount_lo_equality_proof"`
	TransferAmountHiEqualityProof           *zkproofs.CiphertextCiphertextEqualityProof `json:"transfer_amount_hi_equality_proof"`
}

type TransferAuditor struct {
	Address                       string                                      `json:"address"`
	EncryptedTransferAmountLo     *elgamal.Ciphertext                         `json:"encrypted_transfer_amount_lo"`
	EncryptedTransferAmountHi     *elgamal.Ciphertext                         `json:"encrypted_transfer_amount_hi"`
	TransferAmountLoValidityProof *zkproofs.CiphertextValidityProof           `json:"transfer_amount_lo_validity_proof"`
	TransferAmountHiValidityProof *zkproofs.CiphertextValidityProof           `json:"transfer_amount_hi_validity_proof"`
	TransferAmountLoEqualityProof *zkproofs.CiphertextCiphertextEqualityProof `json:"transfer_amount_lo_equality_proof"`
	TransferAmountHiEqualityProof *zkproofs.CiphertextCiphertextEqualityProof `json:"transfer_amount_hi_equality_proof"`
}
