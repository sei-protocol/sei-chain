package types

import (
	"crypto/ecdsa"
	"math/big"

	"github.com/coinbase/kryptology/pkg/core/curves"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type InitializeAccount struct {
	FromAddress        string                   `json:"from_address"`
	Denom              string                   `json:"denom"`
	Pubkey             *curves.Point            `json:"pubkey"`
	PendingBalanceLo   *elgamal.Ciphertext      `json:"pending_balance_lo"`
	PendingBalanceHi   *elgamal.Ciphertext      `json:"pending_balance_hi"`
	AvailableBalance   *elgamal.Ciphertext      `json:"available_balance"`
	DecryptableBalance string                   `json:"decryptable_balance"`
	Proofs             *InitializeAccountProofs `json:"proofs"`
}

type InitializeAccountProofs struct {
	PubkeyValidityProof       *zkproofs.PubKeyValidityProof `json:"pubkey_validity_proof"`
	ZeroPendingBalanceLoProof *zkproofs.ZeroBalanceProof    `json:"zero_pending_balance_lo_proof"`
	ZeroPendingBalanceHiProof *zkproofs.ZeroBalanceProof    `json:"zero_pending_balance_hi_proof"`
	ZeroAvailableBalanceProof *zkproofs.ZeroBalanceProof    `json:"zero_available_balance_proof"`
}

func NewInitializeAccount(address, denom string, privateKey ecdsa.PrivateKey) (*InitializeAccount, error) {
	teg := elgamal.NewTwistedElgamal()
	keys, err := teg.KeyGen(privateKey, denom)
	if err != nil {
		return &InitializeAccount{}, err
	}

	aesKey, err := encryption.GetAESKey(privateKey, denom)
	if err != nil {
		return &InitializeAccount{}, err
	}

	// Encrypt the 0 value using the aesKey
	decryptableBalance, err := encryption.EncryptAESGCM(big.NewInt(0), aesKey)
	if err != nil {
		return &InitializeAccount{}, err
	}

	// Encrypt the 0 value thrice using the public key for the account balances.
	zeroCiphertextLo, _, err := teg.Encrypt(keys.PublicKey, big.NewInt(0))
	if err != nil {
		return &InitializeAccount{}, err
	}

	zeroCiphertextHi, _, err := teg.Encrypt(keys.PublicKey, big.NewInt(0))
	if err != nil {
		return &InitializeAccount{}, err
	}

	zeroCiphertextAvailable, _, err := teg.Encrypt(keys.PublicKey, big.NewInt(0))
	if err != nil {
		return &InitializeAccount{}, err
	}

	pubkeyValidityProof, err := zkproofs.NewPubKeyValidityProof(keys.PublicKey, keys.PrivateKey)
	if err != nil {
		return &InitializeAccount{}, err
	}

	// Generate proofs for the zero values
	proofLo, err := zkproofs.NewZeroBalanceProof(keys, zeroCiphertextLo)
	if err != nil {
		return &InitializeAccount{}, err
	}

	proofHi, err := zkproofs.NewZeroBalanceProof(keys, zeroCiphertextHi)
	if err != nil {
		return &InitializeAccount{}, err
	}

	proofAvailable, err := zkproofs.NewZeroBalanceProof(keys, zeroCiphertextAvailable)
	if err != nil {
		return &InitializeAccount{}, err
	}

	proofs := InitializeAccountProofs{
		PubkeyValidityProof:       pubkeyValidityProof,
		ZeroPendingBalanceLoProof: proofLo,
		ZeroPendingBalanceHiProof: proofHi,
		ZeroAvailableBalanceProof: proofAvailable,
	}

	return &InitializeAccount{
		FromAddress:        address,
		Denom:              denom,
		Pubkey:             &keys.PublicKey,
		DecryptableBalance: decryptableBalance,
		PendingBalanceLo:   zeroCiphertextLo,
		PendingBalanceHi:   zeroCiphertextHi,
		AvailableBalance:   zeroCiphertextAvailable,
		Proofs:             &proofs,
	}, nil
}

// Decrypt decrypts the InitializeAccount message using the provided private key.
func (r InitializeAccount) Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool) (*InitializeAccountDecrypted, error) {
	if decryptor == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptor is required")
	}

	keyPair, err := decryptor.KeyGen(privKey, r.Denom)
	if err != nil {
		return &InitializeAccountDecrypted{}, err
	}

	aesKey, err := encryption.GetAESKey(privKey, r.Denom)
	if err != nil {
		return &InitializeAccountDecrypted{}, err
	}

	pendingBalanceLo, err := decryptor.DecryptLargeNumber(keyPair.PrivateKey, r.PendingBalanceLo, elgamal.MaxBits32)
	if err != nil {
		return nil, err
	}

	pendingBalanceHi, err := decryptor.DecryptLargeNumber(keyPair.PrivateKey, r.PendingBalanceHi, elgamal.MaxBits48)
	if err != nil {
		return nil, err
	}

	decryptableBalance, err := encryption.DecryptAESGCM(r.DecryptableBalance, aesKey)
	if err != nil {
		return nil, err
	}

	availableBalanceString := NotDecrypted

	if decryptAvailableBalance {
		availableBalance, err := decryptor.DecryptLargeNumber(keyPair.PrivateKey, r.AvailableBalance, elgamal.MaxBits48)
		if err != nil {
			return nil, err
		}
		availableBalanceString = availableBalance.String()
	}

	pubkeyRaw := *r.Pubkey
	pubkey := pubkeyRaw.ToAffineCompressed()

	return &InitializeAccountDecrypted{
		FromAddress:        r.FromAddress,
		Denom:              r.Denom,
		Pubkey:             pubkey,
		PendingBalanceLo:   uint32(pendingBalanceLo.Uint64()),
		PendingBalanceHi:   pendingBalanceHi.Uint64(),
		AvailableBalance:   availableBalanceString,
		DecryptableBalance: decryptableBalance.String(),
		Proofs:             NewInitializeAccountMsgProofs(r.Proofs),
	}, nil
}
