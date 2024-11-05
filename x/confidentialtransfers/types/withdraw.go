package types

import (
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type Withdraw struct {
	FromAddress        string `json:"from_address"`
	Denom              string `json:"denom"`
	Amount             uint64 `json:"amount"`
	DecryptableBalance string `json:"decrypted_balance"`

	// The Encrypted remaining balance, but re-encrypted from its plaintext form.
	RemainingBalanceCommitment *elgamal.Ciphertext `json:"remaining_balance_commitment"`

	Proofs *WithdrawProofs `json:"proofs"`
}

type WithdrawProofs struct {
	// Proof that the remaining balance sent as a Commitment is greater or equal to 0
	RemainingBalanceRangeProof *zkproofs.RangeProof `json:"remaining_balance_range_proof"`

	// Equality proof that AvaialbleBalance - Enc(Amount) is equal to the RemainingBalance sent.
	RemainingBalanceEqualityProof *zkproofs.CiphertextCommitmentEqualityProof `json:"remaining_balance_equality_proof"`
}
