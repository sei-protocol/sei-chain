package types

import (
	"crypto/ecdsa"
	"errors"
	"strconv"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
)

// ApplyPendingBalance is a message to apply the pending balance to the available balance
type ApplyPendingBalance struct {
	Address                        string
	Denom                          string
	NewDecryptableAvailableBalance string
	CurrentPendingBalanceCounter   uint32
	CurrentAvailableBalance        *elgamal.Ciphertext
}

// NewApplyPendingBalance creates a new MsgApplyPendingBalance instance
func NewApplyPendingBalance(
	privKey ecdsa.PrivateKey,
	address, denom,
	currentDecryptableBalance string,
	currentPendingBalanceCounter uint16,
	currentAvailableBalance,
	currentPendingBalanceLo,
	currentPendingBalanceHi *elgamal.Ciphertext) (*ApplyPendingBalance, error) {
	aesKey, err := encryption.GetAESKey(privKey, denom)
	if err != nil {
		return nil, err
	}

	// Get the current balance from the decryptable balance.
	currentBalance, err := encryption.DecryptAESGCM(currentDecryptableBalance, aesKey)
	if err != nil {
		return nil, err
	}

	teg := elgamal.NewTwistedElgamal()
	keyPair, err := teg.KeyGen(privKey, denom)
	if err != nil {
		return nil, err
	}

	// Calculate the pending balances that we need to add to the available balance.
	loBalance, err := teg.Decrypt(keyPair.PrivateKey, currentPendingBalanceLo, elgamal.MaxBits32)
	if err != nil {
		return nil, err
	}

	hiBalance, err := teg.DecryptLargeNumber(keyPair.PrivateKey, currentPendingBalanceHi, elgamal.MaxBits48)
	if err != nil {
		return nil, err
	}

	// Get the pending balance by combining the lo and hi bits
	pendingBalance := utils.CombineTransferAmount(uint16(loBalance), uint32(hiBalance))

	// Sum the balances to get the new available balance
	newDecryptedAvailableBalance := currentBalance + pendingBalance

	// Check for overflow: if the sum is less than one of the operands, an overflow has occurred.
	if newDecryptedAvailableBalance < currentBalance {
		return nil, errors.New("addition overflow: total balance exceeds uint64")
	}

	// Encrypt the new available balance
	newDecryptableAvailableBalance, err := encryption.EncryptAESGCM(newDecryptedAvailableBalance, aesKey)
	if err != nil {
		return nil, err
	}

	return &ApplyPendingBalance{
		Address:                        address,
		Denom:                          denom,
		NewDecryptableAvailableBalance: newDecryptableAvailableBalance,
		CurrentPendingBalanceCounter:   uint32(currentPendingBalanceCounter),
		CurrentAvailableBalance:        currentAvailableBalance,
	}, nil
}

// Decrypt returns the decrypted version of ApplyPendingBalance by decrypting using the given privateKey.
func (r *ApplyPendingBalance) Decrypt(decryptor *elgamal.TwistedElGamal, privKey ecdsa.PrivateKey, decryptAvailableBalance bool) (*ApplyPendingBalanceDecrypted, error) {
	if decryptor == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "decryptor is required")
	}

	availableBalanceString := "Not Decrypted"
	keyPair, err := decryptor.KeyGen(privKey, r.Denom)
	if err != nil {
		return &ApplyPendingBalanceDecrypted{}, err
	}

	aesKey, err := encryption.GetAESKey(privKey, r.Denom)
	if err != nil {
		return &ApplyPendingBalanceDecrypted{}, err
	}

	if decryptAvailableBalance {
		decryptedRemainingBalance, err := decryptor.Decrypt(keyPair.PrivateKey, r.CurrentAvailableBalance, elgamal.MaxBits48)
		if err != nil {
			return &ApplyPendingBalanceDecrypted{}, err
		}

		availableBalanceString = strconv.FormatUint(decryptedRemainingBalance, 10)
	}

	decryptableAvailableBalance, err := encryption.DecryptAESGCM(r.NewDecryptableAvailableBalance, aesKey)
	if err != nil {
		return &ApplyPendingBalanceDecrypted{}, err
	}

	return &ApplyPendingBalanceDecrypted{
		Address:                        r.Address,
		Denom:                          r.Denom,
		NewDecryptableAvailableBalance: decryptableAvailableBalance,
		CurrentPendingBalanceCounter:   r.CurrentPendingBalanceCounter,
		CurrentAvailableBalance:        availableBalanceString,
	}, nil
}
