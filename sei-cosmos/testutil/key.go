package testutil

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// GenerateCoinKey generates a new key mnemonic along with its addrress.
func GenerateCoinKey(algo keyring.SignatureAlgo) (seitypes.AccAddress, string, error) {
	// generate a private key, with mnemonic
	info, secret, err := keyring.NewInMemory().NewMnemonic(
		"name",
		keyring.English,
		seitypes.GetConfig().GetFullBIP44Path(),
		keyring.DefaultBIP39Passphrase,
		algo,
	)
	if err != nil {
		return seitypes.AccAddress{}, "", err
	}

	return seitypes.AccAddress(info.GetPubKey().Address()), secret, nil
}

// GenerateSaveCoinKey generates a new key mnemonic with its addrress.
// If mnemonic is provided then it's used for key generation.
// The key is saved in the keyring. The function returns error if overwrite=true and the key
// already exists.
func GenerateSaveCoinKey(
	keybase keyring.Keyring,
	keyName, mnemonic string,
	overwrite bool,
	algo keyring.SignatureAlgo,
) (seitypes.AccAddress, string, error) {
	exists := false
	_, err := keybase.Key(keyName)
	if err == nil {
		exists = true
	}

	// ensure no overwrite
	if !overwrite && exists {
		return seitypes.AccAddress{}, "", fmt.Errorf("key already exists, overwrite is disabled")
	}

	if exists {
		if err := keybase.Delete(keyName); err != nil {
			return seitypes.AccAddress{}, "", fmt.Errorf("failed to overwrite key")
		}
	}

	var (
		info   keyring.Info
		secret string
	)

	if mnemonic != "" {
		secret = mnemonic
		info, err = keybase.NewAccount(
			keyName,
			mnemonic,
			keyring.DefaultBIP39Passphrase,
			seitypes.GetConfig().GetFullBIP44Path(),
			algo,
		)
	} else {
		info, secret, err = keybase.NewMnemonic(
			keyName,
			keyring.English,
			seitypes.GetConfig().GetFullBIP44Path(),
			keyring.DefaultBIP39Passphrase,
			algo,
		)
	}
	if err != nil {
		return seitypes.AccAddress{}, "", err
	}

	return seitypes.AccAddress(info.GetAddress()), secret, nil
}
