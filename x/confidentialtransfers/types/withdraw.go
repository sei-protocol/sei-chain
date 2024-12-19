package types

import (
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/coinbase/kryptology/pkg/core/curves"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type Withdraw struct {
	FromAddress        string   `json:"from_address"`
	Denom              string   `json:"denom"`
	Amount             *big.Int `json:"amount"`
	DecryptableBalance string   `json:"decrypted_balance"`

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

func NewWithdrawFromPrivateKey(
	privateKey ecdsa.PrivateKey,
	currentAvailableBalance *elgamal.Ciphertext,
	denom,
	address,
	currentDecryptableBalance string,
	amount *big.Int) (*Withdraw, error) {
	aesKey, err := utils.GetAESKey(privateKey, denom)
	if err != nil {
		return &Withdraw{}, err
	}

	teg := elgamal.NewTwistedElgamal()
	keyPair, err := utils.GetElGamalKeyPair(privateKey, denom)
	if err != nil {
		return &Withdraw{}, err
	}

	return NewWithdraw(teg, keyPair, aesKey, amount, address, denom, currentAvailableBalance, currentDecryptableBalance)
}

func NewWithdraw(
	teg *elgamal.TwistedElGamal,
	keyPair *elgamal.KeyPair,
	aesKey []byte,
	amount *big.Int,
	address,
	denom string,
	currentAvailableBalance *elgamal.Ciphertext,
	currentDecryptableBalance string) (*Withdraw, error) {
	currentBalance, err := encryption.DecryptAESGCM(currentDecryptableBalance, aesKey)
	if err != nil {
		return &Withdraw{}, err
	}

	if currentBalance.Cmp(amount) == -1 {
		return &Withdraw{}, errors.New("insufficient balance")
	}

	newBalance := new(big.Int).Sub(currentBalance, amount)

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
	rangeProof, err := zkproofs.NewRangeProof(128, newBalance, randomness)
	if err != nil {
		return &Withdraw{}, err
	}

	// Create the equality proof to show that the new balance is equal to the difference between availableBalance and scalar.
	newBalanceCiphertext, err := teg.SubScalar(currentAvailableBalance, amount)
	if err != nil {
		return &Withdraw{}, err
	}

	newBalanceScalar, err := curves.ED25519().Scalar.SetBigInt(newBalance)
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

func (r *Withdraw) Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool) (*WithdrawDecrypted, error) {
	if decryptor == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptor is required")
	}

	availableBalanceString := NotDecrypted
	keyPair, err := utils.GetElGamalKeyPair(privKey, r.Denom)
	if err != nil {
		return &WithdrawDecrypted{}, err
	}

	aesKey, err := utils.GetAESKey(privKey, r.Denom)
	if err != nil {
		return &WithdrawDecrypted{}, err
	}

	if decryptAvailableBalance {
		decryptedRemainingBalance, err := decryptor.Decrypt(keyPair.PrivateKey, r.RemainingBalanceCommitment, elgamal.MaxBits48)
		if err != nil {
			return &WithdrawDecrypted{}, err
		}

		availableBalanceString = decryptedRemainingBalance.String()
	}

	decryptableAvailableBalance, err := encryption.DecryptAESGCM(r.DecryptableBalance, aesKey)
	if err != nil {
		return &WithdrawDecrypted{}, err
	}

	return &WithdrawDecrypted{
		FromAddress:                r.FromAddress,
		Denom:                      r.Denom,
		Amount:                     r.Amount.String(),
		DecryptableBalance:         decryptableAvailableBalance.String(),
		RemainingBalanceCommitment: availableBalanceString,
		Proofs:                     NewWithdrawMsgProofs(r.Proofs),
	}, nil
}
