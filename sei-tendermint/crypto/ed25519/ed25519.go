package ed25519

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"bytes"
	"runtime"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/cache"

	"github.com/tendermint/tendermint/libs/utils"
)

type ErrBadSig struct {
	Idx int // Index of the first invalid signature.
}

func (e ErrBadSig) Error() string {
	return fmt.Sprintf("invalid %vth signature", e.Idx)
}

// cacheSize is the number of public keys that will be cached in
// an expanded format for repeated signature verification.
//
// TODO/perf: Either this should exclude single verification, or be
// tuned to `> validatorSize + maxTxnsPerBlock` to avoid cache
// thrashing.
const cacheSize = 4096

// curve25519-voi's Ed25519 implementation supports configurable
// verification behavior, and tendermint uses the ZIP-215 verification
// semantics.
var verifyOptions = &ed25519.Options{Verify: ed25519.VerifyOptionsZIP_215}
var cachingVerifier = cache.NewVerifier(cache.NewLRUCache(cacheSize))

// SecretKey represents a secret key in the Ed25519 signature scheme.
type SecretKey struct {
	// This is a pointer to avoid copying the secret all over the memory.
	// This is a pointer to pointer, so that runtime.AddCleanup can actually work:
	// Cleanup requires the referenced pointer to be unreachable, even from
	// the cleanup function.
	key **[ed25519.PrivateKeySize]byte
	// Comparing the secrets is not allowed.
	// If you have to, compare the public keys instead.
	_ utils.NoCompare
}

// SecretKeyFromSecretBytes constructs a secret key from a raw secret material.
// WARNING: this function zeroes the content of the input slice.
func SecretKeyFromSecretBytes(b []byte) (SecretKey, error) {
	if got, want := len(b), ed25519.PrivateKeySize; got != want {
		return SecretKey{}, fmt.Errorf("ed25519: bad private key length: got %d, want %d", got, want)
	}
	raw := utils.Alloc([ed25519.PrivateKeySize]byte(b))
	runtime.AddCleanup(&raw, func(int) {
		// Zero the memory to avoid leaking the secret.
		for i := range raw {
			raw[i] = 0
		}
	}, 0)
	key := SecretKey{key: &raw}
	// Zero the input slice to avoid leaking the secret.
	for i := range b {
		b[i] = 0
	}
	return key, nil
}

// TestSecretKey generates a testonly secret key.
func TestSecretKey(seed []byte) SecretKey {
	h := sha256.Sum256(seed)
	key, err := SecretKeyFromSecretBytes(ed25519.NewKeyFromSeed(h[:]))
	if err != nil {
		panic(err)
	}
	return key
}

// GenerateSecretKey generates a new secret key using a cryptographically secure random number generator.
func GenerateSecretKey() (SecretKey, error) {
	var seed [ed25519.PrivateKeySize]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return SecretKey{}, err
	}
	return SecretKeyFromSecretBytes(ed25519.NewKeyFromSeed(seed[:]))
}

// Public returns the public key corresponding to the secret key.
func (k SecretKey) Public() PublicKey {
	p := ed25519.PrivateKey((*k.key)[:]).Public().(ed25519.PublicKey)
	return PublicKey{key: [ed25519.PublicKeySize]byte(p)}
}

// PublicKey represents a public key in the Ed25519 signature scheme.
type PublicKey struct {
	utils.ReadOnly
	key [ed25519.PublicKeySize]byte
}

// Signature represents a signature in the Ed25519 signature scheme.
type Signature struct {
	utils.ReadOnly
	sig [ed25519.SignatureSize]byte
}

// Sign signs a message using the secret key.
func (k SecretKey) Sign(message []byte) Signature {
	return Signature{
		sig: [ed25519.SignatureSize]byte(ed25519.Sign((*k.key)[:], message)),
	}
}

// Compare defines a total order on public keys.
func (k PublicKey) Compare(other PublicKey) int {
	return bytes.Compare(k.key[:], other.key[:])
}

// Verify verifies a signature using the public key.
func (k PublicKey) Verify(msg []byte, sig Signature) error {
	if !ed25519.VerifyWithOptions(k.key[:], msg, sig.sig[:], verifyOptions) {
		return ErrBadSig{}
	}
	return nil
}
// BatchVerifier implements batch verification for ed25519.
type BatchVerifier struct{ inner *ed25519.BatchVerifier }

func NewBatchVerifier() *BatchVerifier { return &BatchVerifier{ed25519.NewBatchVerifier()} }

func (b *BatchVerifier) Add(key PublicKey, msg []byte, sig Signature) {
	cachingVerifier.AddWithOptions(b.inner, key.key[:], msg, sig.sig[:], verifyOptions)
}

// Verify verifies the batched signatures using OS entropy.
// If any signature is invalid, returns ErrBadSig with an index
// of the first invalid signature.
func (b *BatchVerifier) Verify() error {
	ok, res := b.inner.Verify(rand.Reader)
	if ok {
		return nil
	}
	for idx, ok := range res {
		if !ok {
			return ErrBadSig{idx}
		}
	}
	panic("unreachable")
}
