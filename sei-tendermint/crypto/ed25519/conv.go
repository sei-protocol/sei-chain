package ed25519

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"strings"
)

// Bytes converts the public key or signature to bytes.
func (k PublicKey) Bytes() []byte { return k.key[:] }

// Bytes converts the signature to bytes.
func (s Signature) Bytes() []byte { return s.sig[:] }

// PublicKeyFromBytes constructs a public key from bytes.
func PublicKeyFromBytes(key []byte) (PublicKey, error) {
	if got, want := len(key), ed25519.PublicKeySize; got != want {
		return PublicKey{}, fmt.Errorf("ed25519: bad public key length: got %d, want %d", got, want)
	}
	return PublicKey{key: ([ed25519.PublicKeySize]byte)(key)}, nil
}

// SignatureFromBytes constructs a signature from bytes.
func SignatureFromBytes(sig []byte) (Signature, error) {
	if got, want := len(sig), ed25519.SignatureSize; got != want {
		return Signature{}, fmt.Errorf("ed25519: bad signature length: got %d, want %d", got, want)
	}
	return Signature{sig: ([ed25519.SignatureSize]byte)(sig)}, nil
}

// String returns a string representation.
func (k PublicKey) String() string { return fmt.Sprintf("ed25519:public:%x", k.key[:]) }

// String returns a log-safe representation of the secret key.
func (k SecretKey) String() string { return fmt.Sprintf("<secret of %s>", k.Public()) }

// String returns a string representation.
func (s Signature) String() string { return fmt.Sprintf("ed25519:sig:%x", s.sig[:]) }

// GoString returns a strings representation.
func (k PublicKey) GoString() string { return k.String() }

// GoString returns a log-safe representation of the secret key.
func (k SecretKey) GoString() string { return k.String() }

// GoString returns a strings representation.
func (s Signature) GoString() string { return s.String() }

// PublicKeyFromString constructs a public key from a string representation.
func PublicKeyFromString(s string) (PublicKey, error) {
	s2 := strings.TrimPrefix(s, "ed25519:public:")
	if s == s2 {
		return PublicKey{}, errors.New("bad prefix")
	}
	b, err := hex.DecodeString(s2)
	if err != nil {
		return PublicKey{}, fmt.Errorf("hex.DecodeString: %w", err)
	}
	return PublicKeyFromBytes(b)
}

// Address is the SHA256-20 of the raw pubkey bytes.
func (k PublicKey) Address() tmbytes.HexBytes {
	h := sha256.Sum256(k.Bytes())
	return tmbytes.HexBytes(h[:20])
}
