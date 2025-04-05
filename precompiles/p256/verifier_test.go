package p256

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math/big"
	"testing"
)

// Helper function to generate valid key and signature for testing
func generateValidKeyAndSignature(t *testing.T) (hash []byte, r, s, x, y *big.Int) {
	// Generate a private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Get public key coordinates
	x = privateKey.PublicKey.X
	y = privateKey.PublicKey.Y

	// Create a message and hash it
	hash = []byte("test message")

	// Sign the hash
	r, s, err = ecdsa.Sign(rand.Reader, privateKey, hash)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	return hash, r, s, x, y
}

func TestVerify(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() ([]byte, *big.Int, *big.Int, *big.Int, *big.Int)
		expected bool
	}{
		{
			name: "Valid signature should verify",
			setup: func() ([]byte, *big.Int, *big.Int, *big.Int, *big.Int) {
				return generateValidKeyAndSignature(t)
			},
			expected: true,
		},
		{
			name: "Invalid signature should not verify",
			setup: func() ([]byte, *big.Int, *big.Int, *big.Int, *big.Int) {
				hash, r, s, x, y := generateValidKeyAndSignature(t)
				// Tamper with the signature
				s.Add(s, big.NewInt(1))
				return hash, r, s, x, y
			},
			expected: false,
		},
		{
			name: "Invalid public key coordinates should not verify",
			setup: func() ([]byte, *big.Int, *big.Int, *big.Int, *big.Int) {
				hash, r, s, _, _ := generateValidKeyAndSignature(t)
				// Use invalid public key coordinates
				invalidX := big.NewInt(0)
				invalidY := big.NewInt(0)
				return hash, r, s, invalidX, invalidY
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, r, s, x, y := tt.setup()
			result := Verify(hash, r, s, x, y)
			if result != tt.expected {
				t.Errorf("Verify() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestVerifyEdgeCases tests various edge cases for Verify function
func TestVerifyEdgeCases(t *testing.T) {
	_, r, s, x, y := generateValidKeyAndSignature(t)

	t.Run("Empty hash should not verify", func(t *testing.T) {
		emptyHash := []byte{}
		if Verify(emptyHash, r, s, x, y) {
			t.Error("Verify() returned true for empty hash")
		}
	})

	t.Run("Nil inputs should not verify", func(t *testing.T) {
		hash := []byte("test")
		if Verify(hash, nil, s, x, y) {
			t.Error("Verify() returned true for nil r")
		}
		if Verify(hash, r, nil, x, y) {
			t.Error("Verify() returned true for nil s")
		}
		if Verify(hash, r, s, nil, y) {
			t.Error("Verify() returned true for nil x")
		}
		if Verify(hash, r, s, x, nil) {
			t.Error("Verify() returned true for nil y")
		}
	})
}
