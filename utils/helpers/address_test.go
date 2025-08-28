package helpers

import (
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestPubkeyToEVMAddress(t *testing.T) {
	tests := []struct {
		name        string
		pubkey      []byte
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid uncompressed public key",
			pubkey:      generateValidUncompressedPubkey(t),
			expectError: false,
		},
		{
			name:        "empty public key",
			pubkey:      []byte{},
			expectError: true,
			errorMsg:    "invalid public key",
		},
		{
			name:        "invalid public key - wrong prefix",
			pubkey:      []byte{0x03, 0x01, 0x02, 0x03},
			expectError: true,
			errorMsg:    "invalid public key",
		},
		{
			name:        "public key without uncompressed prefix",
			pubkey:      []byte{0x01, 0x02, 0x03, 0x04},
			expectError: true,
			errorMsg:    "invalid public key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := PubkeyToEVMAddress(tt.pubkey)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
				require.Equal(t, common.Address{}, addr)
			} else {
				require.NoError(t, err)
				require.NotEqual(t, common.Address{}, addr)
				// Verify the address is 20 bytes
				require.Len(t, addr.Bytes(), 20)
			}
		})
	}
}

func TestPubkeyBytesToSeiPubKey(t *testing.T) {
	tests := []struct {
		name   string
		pubkey []byte
	}{
		{
			name:   "valid uncompressed public key",
			pubkey: generateValidUncompressedPubkey(t),
		},
		{
			name:   "valid compressed public key",
			pubkey: generateValidCompressedPubkey(t),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seiPubkey := PubkeyBytesToSeiPubKey(tt.pubkey)

			// Verify the key is properly converted
			require.NotNil(t, seiPubkey.Key)
			require.True(t, len(seiPubkey.Key) == 33) // Compressed key should be 33 bytes

			// Verify we can parse it back
			parsedKey, err := btcec.ParsePubKey(tt.pubkey)
			require.NoError(t, err)

			expectedSeiPubkey := secp256k1.PubKey{Key: parsedKey.SerializeCompressed()}
			require.Equal(t, expectedSeiPubkey, seiPubkey)
		})
	}
}

func TestRecoverPubkey(t *testing.T) {
	// Generate a known private key and signature for testing
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Create a test message hash
	message := []byte("test message")
	hash := crypto.Keccak256Hash(message)

	// Sign the hash
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	require.NoError(t, err)

	// Extract R, S, V from signature
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	v := new(big.Int).SetUint64(uint64(signature[64]) + 27)

	tests := []struct {
		name        string
		sighash     common.Hash
		r           *big.Int
		s           *big.Int
		v           *big.Int
		homestead   bool
		expectError bool
	}{
		{
			name:        "valid signature",
			sighash:     hash,
			r:           r,
			s:           s,
			v:           v,
			homestead:   true,
			expectError: false,
		},
		{
			name:        "invalid V value - too large",
			sighash:     hash,
			r:           r,
			s:           s,
			v:           new(big.Int).SetUint64(256), // V > 8 bits
			homestead:   true,
			expectError: true,
		},
		{
			name:        "invalid signature values",
			sighash:     hash,
			r:           big.NewInt(0), // Invalid R
			s:           s,
			v:           v,
			homestead:   true,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pubkey, err := RecoverPubkey(tt.sighash, tt.r, tt.s, tt.v, tt.homestead)

			if tt.expectError {
				require.Error(t, err)
				require.Equal(t, []byte{}, pubkey)
			} else {
				require.NoError(t, err)
				require.NotNil(t, pubkey)
				require.Len(t, pubkey, 65) // Uncompressed public key should be 65 bytes

				// Verify the recovered public key matches the original
				expectedPubkey := crypto.FromECDSAPub(&privateKey.PublicKey)
				require.Equal(t, expectedPubkey, pubkey)
			}
		})
	}
}

func TestGetAddressesFromPubkeyBytes(t *testing.T) {
	tests := []struct {
		name        string
		pubkey      []byte
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid uncompressed public key",
			pubkey:      generateValidUncompressedPubkey(t),
			expectError: false,
		},
		{
			name:        "invalid public key",
			pubkey:      []byte{0x01, 0x02, 0x03},
			expectError: true,
			errorMsg:    "invalid public key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evmAddr, seiAddr, seiPubkey, err := GetAddressesFromPubkeyBytes(tt.pubkey)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
				require.Equal(t, common.Address{}, evmAddr)
				require.Equal(t, sdk.AccAddress{}, seiAddr)
				require.Nil(t, seiPubkey)
			} else {
				require.NoError(t, err)
				require.NotEqual(t, common.Address{}, evmAddr)
				require.NotEqual(t, sdk.AccAddress{}, seiAddr)
				require.NotNil(t, seiPubkey)

				// Verify the addresses are derived correctly
				expectedEvmAddr, err := PubkeyToEVMAddress(tt.pubkey)
				require.NoError(t, err)
				require.Equal(t, expectedEvmAddr, evmAddr)

				expectedSeiPubkey := PubkeyBytesToSeiPubKey(tt.pubkey)
				require.Equal(t, &expectedSeiPubkey, seiPubkey)
				require.Equal(t, sdk.AccAddress(expectedSeiPubkey.Address()), seiAddr)
			}
		})
	}
}

func TestGetAddresses(t *testing.T) {
	// Generate a known private key and signature for testing
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Create a test message hash
	message := []byte("test message")
	hash := crypto.Keccak256Hash(message)

	// Sign the hash
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	require.NoError(t, err)

	// Extract R, S, V from signature
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	v := new(big.Int).SetUint64(uint64(signature[64]) + 27)

	tests := []struct {
		name        string
		v           *big.Int
		r           *big.Int
		s           *big.Int
		data        common.Hash
		expectError bool
	}{
		{
			name:        "valid signature data",
			v:           v,
			r:           r,
			s:           s,
			data:        hash,
			expectError: false,
		},
		{
			name:        "invalid signature - wrong V",
			v:           new(big.Int).SetUint64(256),
			r:           r,
			s:           s,
			data:        hash,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evmAddr, seiAddr, seiPubkey, err := GetAddresses(tt.v, tt.r, tt.s, tt.data)

			if tt.expectError {
				require.Error(t, err)
				require.Equal(t, common.Address{}, evmAddr)
				require.Equal(t, sdk.AccAddress{}, seiAddr)
				require.Nil(t, seiPubkey)
			} else {
				require.NoError(t, err)
				require.NotEqual(t, common.Address{}, evmAddr)
				require.NotEqual(t, sdk.AccAddress{}, seiAddr)
				require.NotNil(t, seiPubkey)

				// Verify the addresses match what we'd expect from the original key
				expectedEvmAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
				require.Equal(t, expectedEvmAddr, evmAddr)
			}
		})
	}
}

// Helper functions for generating test data

func generateValidUncompressedPubkey(t *testing.T) []byte {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	return crypto.FromECDSAPub(&privateKey.PublicKey)
}

func generateValidCompressedPubkey(t *testing.T) []byte {
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	// Parse the uncompressed key and serialize as compressed
	uncompressed := crypto.FromECDSAPub(&privateKey.PublicKey)
	pubkey, err := btcec.ParsePubKey(uncompressed)
	require.NoError(t, err)

	return pubkey.SerializeCompressed()
}
