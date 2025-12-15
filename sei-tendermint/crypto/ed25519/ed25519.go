package ed25519

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"errors"
	"io"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/cache"

	"github.com/tendermint/tendermint/internal/jsontypes"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmjson "github.com/tendermint/tendermint/libs/json"
)

const PrivKeyName = "tendermint/PrivKeyEd25519"
const PubKeyName  = "tendermint/PubKeyEd25519"
const KeyType = "ed25519"

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

func init() {
	tmjson.RegisterType(PubKey{}, PubKeyName)
	jsontypes.MustRegister(PubKey{})
}

type Seed [ed25519.SeedSize]byte

type PrivKey struct {
	raw ed25519.PrivateKey // ed25519.PrivateKey is a slice, therefore the secret will not be copied around. 
}

// TypeTag satisfies the jsontypes.Tagged interface.
func (k PrivKey) TypeTag() string { return PrivKeyName }

// Bytes returns the privkey byte format.
func (k PrivKey) SecretBytes() []byte { return k.raw }

// Sig represents signature.
type Sig [ed25519.SignatureSize]byte

func SigFromBytes(raw []byte) (Sig,error) {
	if len(raw)!=len(Sig{}) {
		return Sig{},errors.New("invalid signature length")
	}
	return Sig(raw),nil
}

// Sign signs a message with the key. 
func (k PrivKey) Sign(msg []byte) Sig { return Sig(ed25519.Sign(k.raw, msg)) }

// PubKey gets the corresponding public key from the private key.
func (k PrivKey) PubKey() PubKey { return PubKey(k.raw.Public().([]byte)) }

func (k PrivKey) Type() string { return KeyType }

// GenPrivKey generates a new ed25519 private key from OS entropy.
func GenPrivKey() PrivKey {
	var seed Seed
	if _,err := io.ReadFull(rand.Reader,seed[:]); err != nil {
		panic(err)
	}
	return PrivKeyFromSeed(seed)
}

// PrivKeyFromSeed constructs a private key from seed.
func PrivKeyFromSeed(seed Seed) PrivKey {
	return PrivKey{ed25519.NewKeyFromSeed(seed[:])}
}

//-------------------------------------

// PubKey implements the Ed25519 signature scheme.
type PubKey [ed25519.PublicKeySize]byte

func PubKeyFromBytes(raw []byte) (PubKey,error) {
	if len(raw)!=len(PubKey{}) {
		return PubKey{},errors.New("invalid signature length")
	}
	return PubKey(raw),nil
}

// TypeTag satisfies the jsontypes.Tagged interface.
func (PubKey) TypeTag() string { return PubKeyName }

// Address is the SHA256-20 of the raw pubkey bytes.
func (k PubKey) Address() tmbytes.HexBytes {
	h := sha256.Sum256(k[:])
	return tmbytes.HexBytes(h[:20])
}

func (k PubKey) Verify(msg []byte, sig Sig) error {
	if !cachingVerifier.VerifyWithOptions(k[:], msg, sig[:], verifyOptions) {
		return errors.New("invalid signature")	
	}
	return nil
}

func (k PubKey) String() string { return fmt.Sprintf("PubKeyEd25519{%X}", k[:]) }
func (k PubKey) Type() string { return KeyType }

// BatchVerifier implements batch verification for ed25519.
type BatchVerifier struct { inner *ed25519.BatchVerifier }
func NewBatchVerifier() *BatchVerifier { return &BatchVerifier{ed25519.NewBatchVerifier()} }

func (b *BatchVerifier) Add(key PubKey, msg []byte, sig Sig) {
	cachingVerifier.AddWithOptions(b.inner, key[:], msg, sig[:], verifyOptions)
}

type ErrBadSig struct {
	Idx int // Index of the first invalid signature.
}

func (e ErrBadSig) Error() string {
	return fmt.Sprintf("invalid %vth signature",e.Idx)
}

// Verify verifies the batched signatures using OS entropy.
// If any signature is invalid, returns ErrBadSig with an index
// of the first invalid signature.
func (b *BatchVerifier) Verify() error {
	ok,res := b.inner.Verify(rand.Reader)
	if ok {
		return nil
	}
	for idx,ok := range res {
		if !ok {
			return ErrBadSig{idx}
		}
	}
	panic("unreachable")
}
