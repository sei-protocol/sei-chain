package helpers

import (
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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

func TestRecoverAddressesFromTx(t *testing.T) {
	chainID := big.NewInt(713715) // Sei chain ID

	// Generate a private key for signing
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	expectedEvmAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Create a signer for this chain (simulating what evmante.SignerMap[version](chainID) returns)
	// In production, the signer is selected based on block height/time via evmante.GetVersion
	signer := ethtypes.NewCancunSigner(chainID)

	tests := []struct {
		name   string
		txType string
		makeTx func() *ethtypes.Transaction
	}{
		{
			name:   "DynamicFeeTx (EIP-1559)",
			txType: "dynamic",
			makeTx: func() *ethtypes.Transaction {
				tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
					ChainID:   chainID,
					Nonce:     0,
					GasFeeCap: big.NewInt(100000000000),
					GasTipCap: big.NewInt(100000000000),
					Gas:       21000,
					To:        &expectedEvmAddr,
					Value:     big.NewInt(1),
				})
				signedTx, err := ethtypes.SignTx(tx, signer, privateKey)
				require.NoError(t, err)
				return signedTx
			},
		},
		{
			name:   "AccessListTx (EIP-2930)",
			txType: "accesslist",
			makeTx: func() *ethtypes.Transaction {
				tx := ethtypes.NewTx(&ethtypes.AccessListTx{
					ChainID:  chainID,
					Nonce:    1,
					GasPrice: big.NewInt(100000000000),
					Gas:      21000,
					To:       &expectedEvmAddr,
					Value:    big.NewInt(1),
				})
				signedTx, err := ethtypes.SignTx(tx, signer, privateKey)
				require.NoError(t, err)
				return signedTx
			},
		},
		{
			name:   "LegacyTx (protected)",
			txType: "legacy",
			makeTx: func() *ethtypes.Transaction {
				tx := ethtypes.NewTx(&ethtypes.LegacyTx{
					Nonce:    2,
					GasPrice: big.NewInt(100000000000),
					Gas:      21000,
					To:       &expectedEvmAddr,
					Value:    big.NewInt(1),
				})
				signedTx, err := ethtypes.SignTx(tx, signer, privateKey)
				require.NoError(t, err)
				return signedTx
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signedTx := tt.makeTx()

			// Use RecoverAddressesFromTx with explicit signer (same pattern as production code)
			evmAddr, seiAddr, pubkey, err := RecoverAddressesFromTx(signedTx, signer, chainID)
			require.NoError(t, err)

			// Verify EVM address matches expected
			require.Equal(t, expectedEvmAddr, evmAddr, "EVM address should match expected")

			// Verify EVM address matches what go-ethereum's Sender returns
			gethSender, err := ethtypes.Sender(signer, signedTx)
			require.NoError(t, err)
			require.Equal(t, gethSender, evmAddr, "EVM address should match go-ethereum Sender result")

			// Verify Sei address is derived correctly from pubkey
			require.NotNil(t, pubkey)
			expectedSeiAddr := sdk.AccAddress(pubkey.Address())
			require.Equal(t, expectedSeiAddr, seiAddr, "Sei address should be derived from pubkey")

			// Verify the pubkey can be used to derive the EVM address
			uncompressedPubkey := crypto.FromECDSAPub(&privateKey.PublicKey)
			derivedEvmAddr, err := PubkeyToEVMAddress(uncompressedPubkey)
			require.NoError(t, err)
			require.Equal(t, derivedEvmAddr, evmAddr, "EVM address from pubkey should match")
		})
	}
}

func TestRecoverAddressesFromTx_MatchesPreprocessLogic(t *testing.T) {
	// This test verifies that RecoverAddressesFromTx produces the same results
	// as the preprocess.go logic by manually performing the same steps

	chainID := big.NewInt(713715)
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	toAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Use version-based signer like production code (Cancun signer for current chain)
	signer := ethtypes.NewCancunSigner(chainID)

	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasFeeCap: big.NewInt(100000000000),
		GasTipCap: big.NewInt(100000000000),
		Gas:       21000,
		To:        &toAddr,
		Value:     big.NewInt(1),
	}), signer, privateKey)
	require.NoError(t, err)

	// Method 1: Use RecoverAddressesFromTx (what production code uses)
	evmAddr1, seiAddr1, pubkey1, err := RecoverAddressesFromTx(signedTx, signer, chainID)
	require.NoError(t, err)

	// Method 2: Manually replicate what preprocess.go does
	txHash := signer.Hash(signedTx)
	V, R, S := signedTx.RawSignatureValues()
	// For non-legacy tx, V needs to be bumped by 27 (same as AdjustV)
	adjustedV := AdjustV(V, signedTx.Type(), chainID)
	evmAddr2, seiAddr2, pubkey2, err := GetAddresses(adjustedV, R, S, txHash)
	require.NoError(t, err)

	// Verify both methods produce the same results
	require.Equal(t, evmAddr1, evmAddr2, "EVM addresses should match between methods")
	require.Equal(t, seiAddr1, seiAddr2, "Sei addresses should match between methods")
	require.Equal(t, pubkey1.Bytes(), pubkey2.Bytes(), "Pubkeys should match between methods")

	// Also verify against go-ethereum's Sender
	gethSender, err := ethtypes.Sender(signer, signedTx)
	require.NoError(t, err)
	require.Equal(t, gethSender, evmAddr1, "should match go-ethereum Sender")
}

func TestAdjustV(t *testing.T) {
	chainID := big.NewInt(713715)

	tests := []struct {
		name     string
		txType   uint8
		inputV   *big.Int
		expected *big.Int
	}{
		{
			name:     "DynamicFeeTx - V=0",
			txType:   ethtypes.DynamicFeeTxType,
			inputV:   big.NewInt(0),
			expected: big.NewInt(27),
		},
		{
			name:     "DynamicFeeTx - V=1",
			txType:   ethtypes.DynamicFeeTxType,
			inputV:   big.NewInt(1),
			expected: big.NewInt(28),
		},
		{
			name:     "AccessListTx - V=0",
			txType:   ethtypes.AccessListTxType,
			inputV:   big.NewInt(0),
			expected: big.NewInt(27),
		},
		{
			name:   "LegacyTx - EIP-155 V value",
			txType: ethtypes.LegacyTxType,
			// For EIP-155: V = chainID * 2 + 35 + recovery_id
			// For chainID 713715 and recovery_id 0: V = 713715*2 + 35 + 0 = 1427465
			inputV:   big.NewInt(1427465),
			expected: big.NewInt(27), // After adjustment: 1427465 - 713715*2 - 8 = 27
		},
		{
			name:     "LegacyTx - EIP-155 V value (recovery_id=1)",
			txType:   ethtypes.LegacyTxType,
			inputV:   big.NewInt(1427466), // recovery_id = 1
			expected: big.NewInt(28),      // After adjustment: 1427466 - 713715*2 - 8 = 28
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AdjustV(tt.inputV, tt.txType, chainID)
			require.Equal(t, tt.expected, result, "adjusted V should match expected")
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
