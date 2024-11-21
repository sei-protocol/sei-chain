package types

import (
	"math/big"
	"strconv"

	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
)

type Account struct {
	// The Public Key, used for Twisted El Gamal Encryption
	PublicKey curves.Point

	// The TEG encrypted low 32 bits of the pending balance.
	// This is calculated as Encrypted(encryptionPK, <low_32_bits_pending_balance>)
	PendingBalanceLo *elgamal.Ciphertext

	// The TEG encrypted high bits of the pending balance.
	// This is calculated as Encrypted(encryptionPK, <high_bits_pending_balance>)
	// Where <high_bits_pending_balance> is at most a 48 bit number.
	PendingBalanceHi *elgamal.Ciphertext

	// The amount of transfers into this account that have not been applied.
	// This should be limited to 2^16 to prevent PendingBalanceLo from overflowing.
	PendingBalanceCreditCounter uint16

	// The encrypted available balance.
	// This is calculated as Encrypted(encryptionPK, <available_balance>)
	AvailableBalance *elgamal.Ciphertext

	// The Asymmetrically Encrypted available balance.
	// This is calculated as AsymmetricEncryption(otherPK, <available_balance>)
	// This is stored as the Base64-encoded ciphertext
	DecryptableAvailableBalance string
}

func (a *Account) GetPendingBalancePlaintext(decryptor *elgamal.TwistedElGamal, keypair *elgamal.KeyPair) (*big.Int, uint64, uint64, error) {
	actualPendingBalanceLo, err := decryptor.Decrypt(keypair.PrivateKey, a.PendingBalanceLo, elgamal.MaxBits32)
	if err != nil {
		return big.NewInt(0), 0, 0, err
	}
	actualPendingBalanceHi, err := decryptor.DecryptLargeNumber(keypair.PrivateKey, a.PendingBalanceHi, elgamal.MaxBits48)
	if err != nil {
		return big.NewInt(0), 0, 0, err
	}

	loBig := new(big.Int).SetUint64(actualPendingBalanceLo)
	hiBig := new(big.Int).SetUint64(actualPendingBalanceHi)

	// Shift the 48-bit number by 32 bits to the left
	hiBig.Lsh(hiBig, 16) // Equivalent to hi << 32

	// Combine by adding hiBig with loBig
	combined := new(big.Int).Add(hiBig, loBig)
	return combined, actualPendingBalanceLo, actualPendingBalanceHi, nil
}

// Returns the decrypted account state.
// Does not decyrpt the available balance unless specified. Decrypting the is only feasible for small numbers under 40 bits.
// TODO: Add tests for this method
func (a *Account) Decrypt(decryptor *elgamal.TwistedElGamal, keypair *elgamal.KeyPair, aesKey []byte, decryptAvailableBalance bool) (*DecryptedCtAccount, error) {
	pendingBalanceCombined, pendingBalanceLo, pendingBalanceHi, err := a.GetPendingBalancePlaintext(decryptor, keypair)
	if err != nil {
		return nil, err
	}

	aesAvailableBalance, err := encryption.DecryptAESGCM(a.DecryptableAvailableBalance, aesKey)
	if err != nil {
		return nil, err
	}

	availableBalanceString := NotDecrypted
	if decryptAvailableBalance {
		availableBalance, err := decryptor.DecryptLargeNumber(keypair.PrivateKey, a.AvailableBalance, elgamal.MaxBits40)
		if err != nil {
			return nil, err
		}

		availableBalanceString = strconv.FormatUint(availableBalance, 10)
	}

	return &DecryptedCtAccount{
		PublicKey:                   a.PublicKey.ToAffineCompressed(),
		PendingBalanceLo:            uint32(pendingBalanceLo),
		PendingBalanceHi:            pendingBalanceHi,
		CombinedPendingBalance:      pendingBalanceCombined.Uint64(),
		PendingBalanceCreditCounter: uint32(a.PendingBalanceCreditCounter),
		AvailableBalance:            availableBalanceString,
		DecryptableAvailableBalance: aesAvailableBalance,
	}, nil
}
