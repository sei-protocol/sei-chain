package utils

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	"testing"

	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test the GetSignedDenom method
func Test_GetSignedDenomHappyPath(t *testing.T) {
	// Arrange
	privateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	denom := "denom"

	// Act
	result, err := GetSignedDenom(privateKey, denom)

	// Assert
	assert.NotNil(t, result)
	assert.NoError(t, err)
}

// Test invalid inputs to the GetSignedDenom method
func TestUtils_GetSignedDenomInvalidInputs(t *testing.T) {
	type args struct {
		privateKey *ecdsa.PrivateKey
		denom      string
	}
	defaultPrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	testCases := []struct {
		name string
		args args
	}{
		{
			name: "Invalid Private Key",
			args: args{
				privateKey: &ecdsa.PrivateKey{},
				denom:      "denom",
			},
		},
		{
			name: "Nil Private Key",
			args: args{
				privateKey: nil,
				denom:      "denom",
			},
		},
		{
			name: "Invalid Denom",
			args: args{
				privateKey: defaultPrivateKey,
				denom:      "",
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			denomKey, err := GetSignedDenom(tt.args.privateKey, tt.args.denom)
			require.Error(t, err)
			require.Nil(t, denomKey)
		})
	}
}

// Test that the GetSignedDenom method is deterministic
func TestUtils_GetSignedDenomDeterminism(t *testing.T) {
	// Arrange
	privateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	denom := "denom"

	// Act
	result1, _ := GetSignedDenom(privateKey, denom)
	result2, _ := GetSignedDenom(privateKey, denom)

	// Assert
	assert.Equal(t, result1, result2)
}

// Test the GetElGamalKeyPair method
func TestUtils_GetElGamalKeyPairHappyPath(t *testing.T) {
	// Arrange
	privateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	denom := "denom"

	// Act
	result, err := GetElGamalKeyPair(*privateKey, denom)

	// Assert
	assert.NotNil(t, result)
	assert.NoError(t, err)
}

// Test invalid inputs to the GetElGamalKeypair method
func TestUtils_GetElGamalKeypairInvalidInputs(t *testing.T) {
	type args struct {
		privateKey *ecdsa.PrivateKey
		denom      string
	}
	defaultPrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	testCases := []struct {
		name string
		args args
	}{
		{
			name: "Invalid Private Key",
			args: args{
				privateKey: &ecdsa.PrivateKey{},
				denom:      "denom",
			},
		},
		{
			name: "Invalid Denom",
			args: args{
				privateKey: defaultPrivateKey,
				denom:      "",
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			denomKey, err := GetElGamalKeyPair(*tt.args.privateKey, tt.args.denom)
			require.Error(t, err)
			require.Nil(t, denomKey)
		})
	}
}

// Test that the GetElGamalKeyPair method is deterministic
func TestUtils_GetElGamalKeyPairDeterminism(t *testing.T) {
	// Arrange
	privateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	denom := "denom"

	// Act
	result1, _ := GetElGamalKeyPair(*privateKey, denom)
	result2, _ := GetElGamalKeyPair(*privateKey, denom)

	// Assert
	assert.Equal(t, result1, result2)
}

func TestUtils_GetAESKeyHappyPath(t *testing.T) {
	privateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	denom := "denom"

	// Act
	result, err := GetAESKey(*privateKey, denom)

	// Assert
	assert.NotNil(t, result)
	assert.NoError(t, err)
}

// Test invalid inputs to the GetAESKey method
func TestUtils_GetAESKey(t *testing.T) {
	type args struct {
		privateKey *ecdsa.PrivateKey
		denom      string
	}
	defaultPrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	testCases := []struct {
		name string
		args args
	}{
		{
			name: "Invalid Private Key",
			args: args{
				privateKey: &ecdsa.PrivateKey{},
				denom:      "denom",
			},
		},
		{
			name: "Invalid Denom",
			args: args{
				privateKey: defaultPrivateKey,
				denom:      "",
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			denomKey, err := GetAESKey(*tt.args.privateKey, tt.args.denom)
			require.Error(t, err)
			require.Nil(t, denomKey)
		})
	}
}

// Test that the GetAESKey method is deterministic
func TestUtils_GetAESKeyDeterminism(t *testing.T) {
	// Arrange
	privateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	denom := "denom"

	// Act
	result1, _ := GetAESKey(*privateKey, denom)
	result2, _ := GetAESKey(*privateKey, denom)

	// Assert
	assert.Equal(t, result1, result2)
}
