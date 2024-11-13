package types

import (
	"crypto/ecdsa"
	"errors"
	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
	"math/big"
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

func NewWithdraw(
	privateKey ecdsa.PrivateKey,
	currentAvailableBalance *elgamal.Ciphertext,
	denom,
	address,
	currentDecryptableBalance string,
	amount uint64) (*Withdraw, error) {
	aesKey, err := encryption.GetAESKey(privateKey, denom)
	if err != nil {
		return &Withdraw{}, err
	}

	teg := elgamal.NewTwistedElgamal()
	keyPair, err := teg.KeyGen(privateKey, denom)
	if err != nil {
		return &Withdraw{}, err
	}

	currentBalance, err := encryption.DecryptAESGCM(currentDecryptableBalance, aesKey)
	if err != nil {
		return &Withdraw{}, err
	}

	if currentBalance < amount {
		return &Withdraw{}, errors.New("insufficient balance")
	}

	newBalance := currentBalance - amount

	// Encrypt the new value using the aesKey
	newDecryptableBalance, err := encryption.EncryptAESGCM(newBalance, aesKey)
	if err != nil {
		return &Withdraw{}, err
	}

	// Create the commitment on the new balance
	newBalanceCommitment, randomness, err := teg.Encrypt(keyPair.PublicKey, newBalance)
	if err != nil {
		return &Withdraw{}, err
	}

	// Create the range proof of the new balance to show that it is greater than 0.
	rangeProof, err := zkproofs.NewRangeProof(64, int(newBalance), randomness)
	if err != nil {
		return &Withdraw{}, err
	}

	// Create the equality proof to show that the new balance is equal to the difference between availableBalance and scalar.
	newBalanceCiphertext, err := teg.SubScalar(currentAvailableBalance, amount)
	if err != nil {
		return &Withdraw{}, err
	}

	bigIntNewBalance := new(big.Int).SetUint64(newBalance)
	newBalanceScalar, err := curves.ED25519().Scalar.SetBigInt(bigIntNewBalance)
	if err != nil {
		return &Withdraw{}, err
	}

	equalityProof, err := zkproofs.NewCiphertextCommitmentEqualityProof(keyPair, newBalanceCiphertext, &randomness, &newBalanceScalar)
	if err != nil {
		return &Withdraw{}, err
	}

	proofs := WithdrawProofs{
		RemainingBalanceRangeProof:    rangeProof,
		RemainingBalanceEqualityProof: equalityProof,
	}
	return &Withdraw{
		FromAddress:                address,
		Denom:                      denom,
		DecryptableBalance:         newDecryptableBalance,
		Amount:                     amount,
		RemainingBalanceCommitment: newBalanceCommitment,
		Proofs:                     &proofs,
	}, nil
}
