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
	c := &Ciphertext{}
	cProto := c.ToProto(&ciphertext)

	assert.NoError(t, cProto.Validate())

	afterConversion, err := cProto.FromProto()
	assert.NoError(t, err)
	assert.True(t, afterConversion.C.Equal(pointC))
	assert.True(t, afterConversion.D.Equal(pointD))
}
