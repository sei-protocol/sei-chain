package ledger

import (
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/pkg/errors"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
)

var (
	// discoverLedger defines a function to be invoked at runtime for discovering
	// a connected Ledger device.
	discoverLedger discoverLedgerFn
)

type (
	// discoverLedgerFn defines a Ledger discovery function that returns a
	// connected device or an error upon failure. Its allows a method to avoid CGO
	// dependencies when Ledger support is potentially not enabled.
	discoverLedgerFn func() (SECP256K1, error)

	// SECP256K1 reflects an interface a Ledger API must implement for SECP256K1
	SECP256K1 interface {
		Close() error
		// Returns an uncompressed pubkey
		GetPublicKeySECP256K1([]uint32) ([]byte, error)
		// Returns a compressed pubkey and bech32 address (requires user confirmation)
		GetAddressPubKeySECP256K1([]uint32, string) ([]byte, string, error)
		// Signs a message (requires user confirmation)
		// signMode: 0 = SIGN_MODE_LEGACY_AMINO, 1 = SIGN_MODE_TEXTUAL
		SignSECP256K1([]uint32, []byte, byte) ([]byte, error)
	}

	// PrivKeyLedgerSecp256k1 implements PrivKey, calling the ledger nano we
	// cache the PubKey from the first call to use it later.
	PrivKeyLedgerSecp256k1 struct {
		// CachedPubKey should be private, but we want to encode it via
		// go-amino so we can view the address later, even without having the
		// ledger attached.
		CachedPubKey types.PubKey
		Path         hd.BIP44Params
	}
)

// NewPrivKeySecp256k1Unsafe will generate a new key and store the public key for later use.
//
// This function is marked as unsafe as it will retrieve a pubkey without user verification.
// It can only be used to verify a pubkey but never to create new accounts/keys. In that case,
// please refer to NewPrivKeySecp256k1
func NewPrivKeySecp256k1Unsafe(path hd.BIP44Params) (types.LedgerPrivKey, error) {
	device, err := getDevice()
	if err != nil {
		return nil, err
	}
	defer warnIfErrors(device.Close)

	pubKey, err := getPubKeyUnsafe(device, path)
	if err != nil {
		return nil, err
	}

	return PrivKeyLedgerSecp256k1{pubKey, path}, nil
}

// NewPrivKeySecp256k1 will generate a new key and store the public key for later use.
// The request will require user confirmation and will show account and index in the device
func NewPrivKeySecp256k1(path hd.BIP44Params, hrp string) (types.LedgerPrivKey, string, error) {
	device, err := getDevice()
	if err != nil {
		return nil, "", fmt.Errorf("failed to retrieve device: %w", err)
	}
	defer warnIfErrors(device.Close)

	pubKey, addr, err := getPubKeyAddrSafe(device, path, hrp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to recover pubkey: %w", err)
	}

	return PrivKeyLedgerSecp256k1{pubKey, path}, addr, nil
}

// PubKey returns the cached public key.
func (pkl PrivKeyLedgerSecp256k1) PubKey() types.PubKey {
	return pkl.CachedPubKey
}

// Sign returns a secp256k1 signature for the corresponding message
func (pkl PrivKeyLedgerSecp256k1) Sign(message []byte) ([]byte, error) {
	device, err := getDevice()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ledger device: %w", err)
	}
	defer warnIfErrors(device.Close)

	sig, err := sign(device, pkl, message)
	if err != nil {
		return nil, err
	}
	return sig, nil
}

// SignWithPath signs a message using a single device connection.
func SignWithPath(path hd.BIP44Params, message []byte) ([]byte, types.PubKey, error) {
	device, err := getDevice()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to ledger: %w", err)
	}
	defer warnIfErrors(device.Close)

	pubKey, err := getPubKeyUnsafe(device, path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get public key: %w", err)
	}

	sig, err := device.SignSECP256K1(path.DerivationPath(), message, 0)
	if err != nil {
		if err.Error() == "" {
			return nil, nil, errors.New("ledger returned empty error: transaction rejected, device timed out, or expert mode not enabled")
		}
		return nil, nil, fmt.Errorf("ledger signing failed: %w", err)
	}

	sig, err = convertDERtoBER(sig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse signature: %w", err)
	}

	return sig, pubKey, nil
}

// ShowAddress triggers a ledger device to show the corresponding address.
func ShowAddress(path hd.BIP44Params, expectedPubKey types.PubKey,
	accountAddressPrefix string) error {
	device, err := getDevice()
	if err != nil {
		return err
	}
	defer warnIfErrors(device.Close)

	pubKey, err := getPubKeyUnsafe(device, path)
	if err != nil {
		return err
	}

	if !pubKey.Equals(expectedPubKey) {
		return fmt.Errorf("the key's pubkey does not match with the one retrieved from Ledger. Check that the HD path and device are the correct ones")
	}

	pubKey2, _, err := getPubKeyAddrSafe(device, path, accountAddressPrefix)
	if err != nil {
		return err
	}

	if !pubKey2.Equals(expectedPubKey) {
		return fmt.Errorf("the key's pubkey does not match with the one retrieved from Ledger. Check that the HD path and device are the correct ones")
	}

	return nil
}

// ValidateKey allows us to verify the sanity of a public key after loading it
// from disk.
func (pkl PrivKeyLedgerSecp256k1) ValidateKey() error {
	device, err := getDevice()
	if err != nil {
		return err
	}
	defer warnIfErrors(device.Close)

	return validateKey(device, pkl)
}

// AssertIsPrivKeyInner implements the PrivKey interface. It performs a no-op.
func (pkl *PrivKeyLedgerSecp256k1) AssertIsPrivKeyInner() {}

// Bytes implements the PrivKey interface. It stores the cached public key so
// we can verify the same key when we reconnect to a ledger.
func (pkl PrivKeyLedgerSecp256k1) Bytes() []byte {
	return cdc.MustMarshal(pkl)
}

// Equals implements the PrivKey interface. It makes sure two private keys
// refer to the same public key.
func (pkl PrivKeyLedgerSecp256k1) Equals(other types.LedgerPrivKey) bool {
	if otherKey, ok := other.(PrivKeyLedgerSecp256k1); ok {
		return pkl.CachedPubKey.Equals(otherKey.CachedPubKey)
	}
	return false
}

func (pkl PrivKeyLedgerSecp256k1) Type() string { return "PrivKeyLedgerSecp256k1" }

func warnIfErrors(f func() error) {
	_ = f()
}

// convertDERtoBER converts a DER-encoded signature to 64-byte R||S format.
func convertDERtoBER(sig []byte) ([]byte, error) {
	if len(sig) == 64 {
		return sig, nil
	}

	parsed, err := ecdsa.ParseDERSignature(sig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DER signature: %w", err)
	}

	r, s := parsed.R(), parsed.S()
	var rBytes, sBytes [32]byte
	r.PutBytesUnchecked(rBytes[:])
	s.PutBytesUnchecked(sBytes[:])

	result := make([]byte, 64)
	copy(result[:32], rBytes[:])
	copy(result[32:], sBytes[:])

	return result, nil
}

func getDevice() (SECP256K1, error) {
	if discoverLedger == nil {
		return nil, errors.New("no Ledger discovery function defined")
	}

	device, err := discoverLedger()
	if err != nil {
		return nil, errors.Wrap(err, "ledger nano S")
	}

	return device, nil
}

func validateKey(device SECP256K1, pkl PrivKeyLedgerSecp256k1) error {
	pub, err := getPubKeyUnsafe(device, pkl.Path)
	if err != nil {
		return err
	}
	if !pub.Equals(pkl.CachedPubKey) {
		return errors.New("cached key does not match device key")
	}
	return nil
}

func sign(device SECP256K1, pkl PrivKeyLedgerSecp256k1, msg []byte) ([]byte, error) {
	if err := validateKey(device, pkl); err != nil {
		return nil, fmt.Errorf("key validation failed: %w", err)
	}

	sig, err := device.SignSECP256K1(pkl.Path.DerivationPath(), msg, 0)
	if err != nil {
		if err.Error() == "" {
			return nil, errors.New("ledger returned empty error: transaction rejected, device timed out, or expert mode not enabled")
		}
		return nil, fmt.Errorf("ledger signing failed: %w", err)
	}

	return convertDERtoBER(sig)
}

// getPubKeyUnsafe retrieves pubkey without user verification (use getPubKeyAddrSafe for new keys).
func getPubKeyUnsafe(device SECP256K1, path hd.BIP44Params) (types.PubKey, error) {
	publicKey, err := device.GetPublicKeySECP256K1(path.DerivationPath())
	if err != nil {
		return nil, fmt.Errorf("open Cosmos app on Ledger: %w", err)
	}

	cmp, err := btcec.ParsePubKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	compressedPublicKey := make([]byte, secp256k1.PubKeySize)
	copy(compressedPublicKey, cmp.SerializeCompressed())

	return &secp256k1.PubKey{Key: compressedPublicKey}, nil
}

// getPubKeyAddrSafe retrieves pubkey with user confirmation (for creating new keys).
func getPubKeyAddrSafe(device SECP256K1, path hd.BIP44Params, hrp string) (types.PubKey, string, error) {
	publicKey, addr, err := device.GetAddressPubKeySECP256K1(path.DerivationPath(), hrp)
	if err != nil {
		return nil, "", fmt.Errorf("address rejected for path %s: %w", path.String(), err)
	}

	cmp, err := btcec.ParsePubKey(publicKey)
	if err != nil {
		return nil, "", fmt.Errorf("parse public key: %w", err)
	}

	compressedPublicKey := make([]byte, secp256k1.PubKeySize)
	copy(compressedPublicKey, cmp.SerializeCompressed())

	return &secp256k1.PubKey{Key: compressedPublicKey}, addr, nil
}
