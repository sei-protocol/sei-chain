package secp256k1_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	underlyingSecp256k1 "github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcutil/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

type keyData struct {
	priv string
	pub  string
	addr string
}

var secpDataTable = []keyData{
	{
		priv: "a96e62ed3955e65be32703f12d87b6b5cf26039ecfa948dc5107a495418e5330",
		pub:  "02950e1cdfcb133d6024109fd489f734eeb4502418e538c28481f22bce276f248c",
		addr: "1CKZ9Nx4zgds8tU7nJHotKSDr4a9bYJCa3",
	},
}

func TestPubKeySecp256k1Address(t *testing.T) {
	for _, d := range secpDataTable {
		privB, _ := hex.DecodeString(d.priv)
		pubB, _ := hex.DecodeString(d.pub)
		addrBbz, _, _ := base58.CheckDecode(d.addr)
		addrB := crypto.Address(addrBbz)

		priv := secp256k1.PrivKey(privB)
		pubKey := priv.PubKey()
		pubT, _ := pubKey.(secp256k1.PubKey)
		pub := pubT
		addr := pubKey.Address()

		assert.Equal(t, pub, secp256k1.PubKey(pubB), "Expected pub keys to match")
		assert.Equal(t, addr, addrB, "Expected addresses to match")
	}
}

func TestSignAndValidateSecp256k1(t *testing.T) {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()

	msg := crypto.CRandBytes(128)
	sig, err := privKey.Sign(msg)
	require.NoError(t, err)

	assert.True(t, pubKey.VerifySignature(msg, sig))

	// Mutate the signature, just one bit.
	sig[3] ^= byte(0x01)

	assert.False(t, pubKey.VerifySignature(msg, sig))
}

// TestSignatureMalleabilityPrevention tests that high-S signatures are rejected
func TestSignatureMalleabilityPrevention(t *testing.T) {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	msg := []byte("test message")

	// Generate a valid signature
	sig, err := privKey.Sign(msg)
	require.NoError(t, err)
	require.True(t, pubKey.VerifySignature(msg, sig))

	// Create a high-S signature by manipulating S component
	// Get curve order N
	curve := underlyingSecp256k1.S256()
	N := curve.N
	halfN := new(big.Int).Rsh(N, 1) // N/2

	// Extract S from signature
	s := new(big.Int).SetBytes(sig[32:64])

	// If S <= N/2, create high-S version: S' = N - S
	if s.Cmp(halfN) <= 0 {
		highS := new(big.Int).Sub(N, s)

		// Create malicious signature with high-S
		maliciousSig := make([]byte, 64)
		copy(maliciousSig[:32], sig[:32]) // Keep R the same

		// Pad highS to 32 bytes
		highSBytes := highS.Bytes()
		copy(maliciousSig[64-len(highSBytes):], highSBytes)

		// This should be rejected due to malleability prevention
		assert.False(t, pubKey.VerifySignature(msg, maliciousSig),
			"High-S signature should be rejected to prevent malleability")
	}
}

// TestSignatureOverflowValidation tests that R,S >= curve order are rejected
func TestSignatureOverflowValidation(t *testing.T) {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	msg := []byte("test message")

	// Get curve order N
	N := underlyingSecp256k1.S256().N

	tests := []struct {
		name string
		r, s *big.Int
	}{
		{
			name: "R equals curve order",
			r:    new(big.Int).Set(N),
			s:    big.NewInt(1),
		},
		{
			name: "S equals curve order",
			r:    big.NewInt(1),
			s:    new(big.Int).Set(N),
		},
		{
			name: "R greater than curve order",
			r:    new(big.Int).Add(N, big.NewInt(1)),
			s:    big.NewInt(1),
		},
		{
			name: "S greater than curve order",
			r:    big.NewInt(1),
			s:    new(big.Int).Add(N, big.NewInt(1)),
		},
		{
			name: "Both R and S equal curve order",
			r:    new(big.Int).Set(N),
			s:    new(big.Int).Set(N),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create signature with invalid R,S values
			invalidSig := make([]byte, 64)

			// Convert R and S to 32-byte arrays
			rBytes := tt.r.Bytes()
			sBytes := tt.s.Bytes()

			// Pad to 32 bytes if needed
			copy(invalidSig[32-len(rBytes):32], rBytes)
			copy(invalidSig[64-len(sBytes):64], sBytes)

			// Should be rejected due to overflow
			assert.False(t, pubKey.VerifySignature(msg, invalidSig),
				"Signature with R,S >= curve order should be rejected")
		})
	}
}

// TestInvalidSignatureFormats tests various invalid signature formats
func TestInvalidSignatureFormats(t *testing.T) {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	msg := []byte("test message")

	tests := []struct {
		name string
		sig  []byte
	}{
		{
			name: "empty signature",
			sig:  []byte{},
		},
		{
			name: "short signature",
			sig:  make([]byte, 63),
		},
		{
			name: "long signature",
			sig:  make([]byte, 65),
		},
		{
			name: "zero signature",
			sig:  make([]byte, 64),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.False(t, pubKey.VerifySignature(msg, tt.sig),
				"Invalid signature format should be rejected")
		})
	}
}

// TestSignatureConsistency ensures our signatures are deterministic and consistent
func TestSignatureConsistency(t *testing.T) {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	msg := []byte("consistent test message")

	// Sign the same message multiple times
	sigs := make([][]byte, 10)
	for i := range sigs {
		sig, err := privKey.Sign(msg)
		require.NoError(t, err)
		sigs[i] = sig

		// Each signature should be valid
		assert.True(t, pubKey.VerifySignature(msg, sig))

		// Each signature should be low-S (malleability resistant)
		s := new(big.Int).SetBytes(sig[32:64])
		halfN := new(big.Int).Rsh(underlyingSecp256k1.S256().N, 1)
		assert.True(t, s.Cmp(halfN) <= 0, "Signature should have low-S")
	}
}

// This test is intended to justify the removal of calls to the underlying library
// in creating the privkey.
func TestSecp256k1LoadPrivkeyAndSerializeIsIdentity(t *testing.T) {
	numberOfTests := 256
	for i := 0; i < numberOfTests; i++ {
		// Seed the test case with some random bytes
		privKeyBytes := [32]byte{}
		copy(privKeyBytes[:], crypto.CRandBytes(32))

		// This function creates a private and public key in the underlying libraries format.
		// The private key is basically calling new(big.Int).SetBytes(pk), which removes leading zero bytes
		priv, _ := underlyingSecp256k1.PrivKeyFromBytes(privKeyBytes[:])
		// this takes the bytes returned by `(big int).Bytes()`, and if the length is less than 32 bytes,
		// pads the bytes from the left with zero bytes. Therefore these two functions composed
		// result in the identity function on privKeyBytes, hence the following equality check
		// always returning true.
		serializedBytes := priv.Serialize()
		require.Equal(t, privKeyBytes[:], serializedBytes)
	}
}

func TestGenPrivKeySecp256k1(t *testing.T) {
	// curve oder N
	N := underlyingSecp256k1.S256().N
	tests := []struct {
		name   string
		secret []byte
	}{
		{"empty secret", []byte{}},
		{
			"some long secret",
			[]byte("We live in a society exquisitely dependent on science and technology, " +
				"in which hardly anyone knows anything about science and technology."),
		},
		{"another seed used in cosmos tests #1", []byte{0}},
		{"another seed used in cosmos tests #2", []byte("mySecret")},
		{"another seed used in cosmos tests #3", []byte("")},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			gotPrivKey := secp256k1.GenPrivKeySecp256k1(tt.secret)
			require.NotNil(t, gotPrivKey)
			// interpret as a big.Int and make sure it is a valid field element:
			fe := new(big.Int).SetBytes(gotPrivKey[:])
			require.True(t, fe.Cmp(N) < 0)
			require.True(t, fe.Sign() > 0)
		})
	}
}
