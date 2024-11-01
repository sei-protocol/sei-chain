package types

import (
	"github.com/coinbase/kryptology/pkg/core/curves"
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
