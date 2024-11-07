package types

import (
	"errors"
	"github.com/coinbase/kryptology/pkg/core/curves"
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
	Proofs                     *TransferProofs     `json:"proofs"`
	Auditors                   []*TransferAuditor  `json:"auditors,omitempty"` //optional field
}

type TransferProofs struct {
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

// Verifies the proofs sent in the transfer request. This does not verify proofs for auditors.
func VerifyTransferProofs(params *Transfer, senderPubkey *curves.Point, recipientPubkey *curves.Point, newBalanceCiphertext *elgamal.Ciphertext) error {
	// Verify the validity proofs that the ciphertexts sent are valid (encrypted with the correct pubkey).
	ok := zkproofs.VerifyCiphertextValidity(params.Proofs.RemainingBalanceCommitmentValidityProof, *senderPubkey, params.RemainingBalanceCommitment)
	if !ok {
		return errors.New("Failed to verify remaining balance commitment")
	}

	ok = zkproofs.VerifyCiphertextValidity(params.Proofs.SenderTransferAmountLoValidityProof, *senderPubkey, params.SenderTransferAmountLo)
	if !ok {
		return errors.New("Failed to verify senderTransferAmountLo")
	}

	ok = zkproofs.VerifyCiphertextValidity(params.Proofs.SenderTransferAmountHiValidityProof, *senderPubkey, params.SenderTransferAmountHi)
	if !ok {
		return errors.New("Failed to verify senderTransferAmountHi")
	}

	ok = zkproofs.VerifyCiphertextValidity(params.Proofs.RecipientTransferAmountLoValidityProof, *recipientPubkey, params.RecipientTransferAmountLo)
	if !ok {
		return errors.New("Failed to verify recipientTransferAmountLo")
	}

	ok = zkproofs.VerifyCiphertextValidity(params.Proofs.RecipientTransferAmountHiValidityProof, *recipientPubkey, params.RecipientTransferAmountHi)
	if !ok {
		return errors.New("Failed to verify recipientTransferAmountHi")
	}

	// Verify that the account's remaining balance is greater than zero after this transfer.
	// This validates the RemainingBalanceCommitment sent by the user, so an additional check is needed to make sure this matches what is calculated by the server.
	ok, err := zkproofs.VerifyRangeProof(params.Proofs.RemainingBalanceRangeProof, params.RemainingBalanceCommitment, 64)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("Range proof verification failed")
	}

	// As part of the range proof above, we verify that the RemainingBalanceCommitment sent by the user is equal to the remaining balance calculated by the server.
	ok = zkproofs.VerifyCiphertextCommitmentEquality(params.Proofs.RemainingBalanceEqualityProof, senderPubkey, newBalanceCiphertext, &params.RemainingBalanceCommitment.C)
	if !ok {
		return errors.New("Ciphertext Commitment equality verification failed")
	}

	// Lastly verify that the transferAmount ciphertexts encode the same value
	ok = zkproofs.VerifyCiphertextCiphertextEquality(params.Proofs.TransferAmountLoEqualityProof, senderPubkey, recipientPubkey, params.SenderTransferAmountLo, params.RecipientTransferAmountLo)
	if !ok {
		return errors.New("Ciphertext Ciphertext equality verification on transferAmountLo failed")
	}

	ok = zkproofs.VerifyCiphertextCiphertextEquality(params.Proofs.TransferAmountHiEqualityProof, senderPubkey, recipientPubkey, params.SenderTransferAmountHi, params.RecipientTransferAmountHi)
	if !ok {
		return errors.New("Ciphertext Ciphertext equality verification on transferAmountHi failed")
	}

	return nil
}

// Verifies the proofs sent for an individual auditor.
func VerifyAuditorProof(
	senderTransferAmountLo,
	senderTransferAmountHi *elgamal.Ciphertext,
	auditorParams *TransferAuditor,
	senderPubkey *curves.Point,
	auditorPubkey *curves.Point) error {
	// Verify that the transfer amounts are valid (encrypted with the correct pubkey).
	ok := zkproofs.VerifyCiphertextValidity(auditorParams.TransferAmountLoValidityProof, *auditorPubkey, auditorParams.EncryptedTransferAmountLo)
	if !ok {
		return errors.New("Failed to verify auditor TransferAmountLo")
	}

	ok = zkproofs.VerifyCiphertextValidity(auditorParams.TransferAmountHiValidityProof, *auditorPubkey, auditorParams.EncryptedTransferAmountHi)
	if !ok {
		return errors.New("Failed to verify auditor TransferAmountHi")
	}

	// Then, verify that the transferAmount ciphertexts encode the same value
	ok = zkproofs.VerifyCiphertextCiphertextEquality(auditorParams.TransferAmountLoEqualityProof, senderPubkey, auditorPubkey, senderTransferAmountLo, auditorParams.EncryptedTransferAmountLo)
	if !ok {
		return errors.New("Ciphertext Ciphertext equality verification on auditor transferAmountLo failed")
	}

	ok = zkproofs.VerifyCiphertextCiphertextEquality(auditorParams.TransferAmountHiEqualityProof, senderPubkey, auditorPubkey, senderTransferAmountHi, auditorParams.EncryptedTransferAmountHi)
	if !ok {
		return errors.New("Ciphertext Ciphertext equality verification on auditor transferAmountHi failed")
	}

	return nil
}
