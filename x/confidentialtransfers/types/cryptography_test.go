package types

import (
	"testing"

	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/stretchr/testify/assert"
)

func TestFromCiphertext(t *testing.T) {
	// Create a sample elgamal.Ciphertext
	ed25519Curve := curves.ED25519()
	pointC := ed25519Curve.Point.Generator()
	pointD := ed25519Curve.Point.Generator()

	ciphertext := elgamal.Ciphertext{
		C: pointC,
		D: pointD,
	}

	// Convert to our Ciphertext type
	cProto := NewCiphertextProto(&ciphertext)

	assert.NoError(t, cProto.Validate())

	afterConversion, err := cProto.FromProto()
	assert.NoError(t, err)
	assert.True(t, afterConversion.C.Equal(pointC))
	assert.True(t, afterConversion.D.Equal(pointD))
}

func TestFromCiphertextInvalid(t *testing.T) {
	c := &Ciphertext{
		C: nil,
		D: nil,
	}

	_, err := c.FromProto()
	assert.Error(t, err)
}

func TestInvalidCiphertextFailsValidation(t *testing.T) {
	c := &Ciphertext{
		C: nil,
		D: nil,
	}

	err := c.Validate()
	assert.Error(t, err)
}
