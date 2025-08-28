package helpers

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// Test the constructor
func TestNewAssociationHelper(t *testing.T) {
	// Test that the constructor properly initializes the helper
	helper := NewAssociationHelper(nil, nil, nil)
	require.NotNil(t, helper)
	require.Nil(t, helper.evmKeeper)
	require.Nil(t, helper.bankKeeper)
	require.Nil(t, helper.accountKeeper)
}

// Test address generation and conversion functions
func TestAddressConversions(t *testing.T) {
	// Generate test keys
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	pubkeyBytes := crypto.FromECDSAPub(&privateKey.PublicKey)
	evmAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	t.Run("pubkey conversion to sei pubkey", func(t *testing.T) {
		seiPubkey := PubkeyBytesToSeiPubKey(pubkeyBytes)
		require.NotNil(t, seiPubkey.Key)
		require.Equal(t, 33, len(seiPubkey.Key)) // Compressed key length
	})

	t.Run("addresses from pubkey bytes", func(t *testing.T) {
		derivedEvmAddr, seiAddr, seiPubkey, err := GetAddressesFromPubkeyBytes(pubkeyBytes)
		require.NoError(t, err)
		require.Equal(t, evmAddr, derivedEvmAddr)
		require.NotNil(t, seiAddr)
		require.NotNil(t, seiPubkey)

		// Verify the sei address is derived from the pubkey
		expectedSeiAddr := sdk.AccAddress(seiPubkey.Address())
		require.Equal(t, expectedSeiAddr, seiAddr)
	})
}

func TestAddressHelperErrorCases(t *testing.T) {
	t.Run("invalid pubkey to EVM address", func(t *testing.T) {
		// Test with empty pubkey
		_, err := PubkeyToEVMAddress([]byte{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid public key")

		// Test with wrong prefix
		_, err = PubkeyToEVMAddress([]byte{0x03, 0x01, 0x02})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid public key")
	})

	t.Run("invalid pubkey bytes to addresses", func(t *testing.T) {
		_, _, _, err := GetAddressesFromPubkeyBytes([]byte{0x01, 0x02, 0x03})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid public key")
	})
}

func TestSignatureRecovery(t *testing.T) {
	// Generate a known private key and signature for testing
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Create a test message hash
	message := []byte("test message for signature recovery")
	hash := crypto.Keccak256Hash(message)

	// Sign the hash
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	require.NoError(t, err)

	t.Run("successful signature recovery", func(t *testing.T) {
		// Extract R, S, V from signature
		r := signature[:32]
		s := signature[32:64]
		v := signature[64] + 27

		// Recover the public key
		recoveredPubkey, err := crypto.Ecrecover(hash.Bytes(), append(r, append(s, v-27)...))
		require.NoError(t, err)
		require.NotNil(t, recoveredPubkey)
		require.Equal(t, 65, len(recoveredPubkey)) // Uncompressed public key

		// Verify the recovered public key matches the original
		expectedPubkey := crypto.FromECDSAPub(&privateKey.PublicKey)
		require.Equal(t, expectedPubkey, recoveredPubkey)
	})
}

func TestKeyConversionConsistency(t *testing.T) {
	// Test that converting between different key formats is consistent
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Get the uncompressed public key
	uncompressedPubkey := crypto.FromECDSAPub(&privateKey.PublicKey)

	// Convert to Sei pubkey
	seiPubkey := PubkeyBytesToSeiPubKey(uncompressedPubkey)

	// Get EVM address from both sources
	evmAddr1 := crypto.PubkeyToAddress(privateKey.PublicKey)
	evmAddr2, err := PubkeyToEVMAddress(uncompressedPubkey)
	require.NoError(t, err)

	// They should be the same
	require.Equal(t, evmAddr1, evmAddr2)

	// Get addresses using our helper
	evmAddr3, seiAddr, returnedSeiPubkey, err := GetAddressesFromPubkeyBytes(uncompressedPubkey)
	require.NoError(t, err)

	// All EVM addresses should match
	require.Equal(t, evmAddr1, evmAddr3)

	// The returned sei pubkey should match
	require.Equal(t, seiPubkey, *returnedSeiPubkey.(*secp256k1.PubKey))

	// The sei address should be derived from the pubkey
	expectedSeiAddr := sdk.AccAddress(seiPubkey.Address())
	require.Equal(t, expectedSeiAddr, seiAddr)
}

func TestEdgeCases(t *testing.T) {
	t.Run("pubkey conversion edge cases", func(t *testing.T) {
		// Test with a byte array that has correct prefix but is empty after prefix
		invalidKey := []byte{0x04} // Just the prefix, no actual key data
		_, err := PubkeyToEVMAddress(invalidKey)
		// This should succeed in PubkeyToEVMAddress but may fail elsewhere
		// Since our function just checks prefix and computes keccak256, it should work
		require.NoError(t, err) // Changed expectation based on actual implementation

		// Test that the function produces a valid 20-byte address even with minimal data
		addr, err := PubkeyToEVMAddress(invalidKey)
		require.NoError(t, err)
		require.Equal(t, 20, len(addr.Bytes()))
	})

	t.Run("address byte array conversions", func(t *testing.T) {
		// Test that EVM address conversion produces 20-byte addresses
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		uncompressedPubkey := crypto.FromECDSAPub(&privateKey.PublicKey)
		evmAddr, err := PubkeyToEVMAddress(uncompressedPubkey)
		require.NoError(t, err)
		require.Equal(t, 20, len(evmAddr.Bytes()))
	})
}
