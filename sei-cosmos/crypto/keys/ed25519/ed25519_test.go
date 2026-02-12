package ed25519_test

import (
	stded25519 "crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	tmed25519 "github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
)

func TestSignAndValidateEd25519(t *testing.T) {
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()

	msg := crypto.CRandBytes(1000)
	sig, err := privKey.Sign(msg)
	require.Nil(t, err)

	// Test the signature
	assert.True(t, pubKey.VerifySignature(msg, sig))

	// ----
	// Test cross packages verification
	stdPrivKey := stded25519.PrivateKey(privKey.Key)
	stdPubKey := stdPrivKey.Public().(stded25519.PublicKey)

	assert.Equal(t, stdPubKey, pubKey.(*ed25519.PubKey).Key)
	assert.Equal(t, stdPrivKey, privKey.Key)
	assert.True(t, stded25519.Verify(stdPubKey, msg, sig))
	sig2 := stded25519.Sign(stdPrivKey, msg)
	assert.True(t, pubKey.VerifySignature(msg, sig2))

	// ----
	// Mutate the signature, just one bit.
	// TODO: Replace this with a much better fuzzer, tendermint/ed25519/issues/10
	sig[7] ^= byte(0x01)
	assert.False(t, pubKey.VerifySignature(msg, sig))
}

func TestPubKeyEquals(t *testing.T) {
	ed25519PubKey := ed25519.GenPrivKey().PubKey().(*ed25519.PubKey)

	testCases := []struct {
		msg      string
		pubKey   cryptotypes.PubKey
		other    cryptotypes.PubKey
		expectEq bool
	}{
		{
			"different bytes",
			ed25519PubKey,
			ed25519.GenPrivKey().PubKey(),
			false,
		},
		{
			"equals",
			ed25519PubKey,
			&ed25519.PubKey{
				Key: ed25519PubKey.Key,
			},
			true,
		},
		{
			"different types",
			ed25519PubKey,
			secp256k1.GenPrivKey().PubKey(),
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.msg, func(t *testing.T) {
			eq := tc.pubKey.Equals(tc.other)
			require.Equal(t, eq, tc.expectEq)
		})
	}
}

func TestAddressEd25519(t *testing.T) {
	pk := ed25519.PubKey{[]byte{125, 80, 29, 208, 159, 53, 119, 198, 73, 53, 187, 33, 199, 144, 62, 255, 1, 235, 117, 96, 128, 211, 17, 45, 34, 64, 189, 165, 33, 182, 54, 206}}
	addr := pk.Address()
	require.Len(t, addr, 20, "Address must be 20 bytes long")
}

func TestPrivKeyEquals(t *testing.T) {
	ed25519PrivKey := ed25519.GenPrivKey()

	testCases := []struct {
		msg      string
		privKey  cryptotypes.PrivKey
		other    cryptotypes.PrivKey
		expectEq bool
	}{
		{
			"different bytes",
			ed25519PrivKey,
			ed25519.GenPrivKey(),
			false,
		},
		{
			"equals",
			ed25519PrivKey,
			&ed25519.PrivKey{
				Key: ed25519PrivKey.Key,
			},
			true,
		},
		{
			"different types",
			ed25519PrivKey,
			secp256k1.GenPrivKey(),
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.msg, func(t *testing.T) {
			eq := tc.privKey.Equals(tc.other)
			require.Equal(t, eq, tc.expectEq)
		})
	}
}

func TestMarshalAmino(t *testing.T) {
	aminoCdc := codec.NewLegacyAmino()
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey().(*ed25519.PubKey)

	testCases := []struct {
		desc      string
		msg       codec.AminoMarshaler
		typ       any
		expBinary []byte
		expJSON   string
	}{
		{
			"ed25519 private key",
			privKey,
			&ed25519.PrivKey{},
			append([]byte{64}, privKey.Bytes()...), // Length-prefixed.
			"\"" + base64.StdEncoding.EncodeToString(privKey.Bytes()) + "\"",
		},
		{
			"ed25519 public key",
			pubKey,
			&ed25519.PubKey{},
			append([]byte{32}, pubKey.Bytes()...), // Length-prefixed.
			"\"" + base64.StdEncoding.EncodeToString(pubKey.Bytes()) + "\"",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Do a round trip of encoding/decoding binary.
			bz, err := aminoCdc.Marshal(tc.msg)
			require.NoError(t, err)
			require.Equal(t, tc.expBinary, bz)

			err = aminoCdc.Unmarshal(bz, tc.typ)
			require.NoError(t, err)

			require.Equal(t, tc.msg, tc.typ)

			// Do a round trip of encoding/decoding JSON.
			bz, err = aminoCdc.MarshalAsJSON(tc.msg)
			require.NoError(t, err)
			require.Equal(t, tc.expJSON, string(bz))

			err = aminoCdc.UnmarshalAsJSON(bz, tc.typ)
			require.NoError(t, err)

			require.Equal(t, tc.msg, tc.typ)
		})
	}
}

func TestMarshalAmino_BackwardsCompatibility(t *testing.T) {
	aminoCdc := codec.NewLegacyAmino()
	// Create Tendermint keys.
	tmPrivKey := tmed25519.GenerateSecretKey()
	tmPubKey := tmPrivKey.Public()
	// Create our own keys, with the same private key as Tendermint's.
	privKey := &ed25519.PrivKey{Key: tmPrivKey.SecretBytes()}
	pubKey := privKey.PubKey().(*ed25519.PubKey)

	testCases := []struct {
		desc      string
		tmKey     any
		ourKey    any
		marshalFn func(o any) ([]byte, error)
	}{
		{
			"ed25519 private key, binary",
			tmPrivKey,
			privKey,
			aminoCdc.Marshal,
		},
		{
			"ed25519 private key, JSON",
			tmPrivKey,
			privKey,
			aminoCdc.MarshalAsJSON,
		},
		{
			"ed25519 public key, binary",
			tmPubKey,
			pubKey,
			aminoCdc.Marshal,
		},
		{
			"ed25519 public key, JSON",
			tmPubKey,
			pubKey,
			aminoCdc.MarshalAsJSON,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Make sure Amino encoding override is not breaking backwards compatibility.
			bz1, err := tc.marshalFn(tc.tmKey)
			require.NoError(t, err)
			bz2, err := tc.marshalFn(tc.ourKey)
			require.NoError(t, err)
			require.Equal(t, bz1, bz2)
		})
	}
}

func TestMarshalJSON(t *testing.T) {
	require := require.New(t)
	privKey := ed25519.GenPrivKey()
	pk := privKey.PubKey()

	registry := types.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	bz, err := cdc.MarshalInterfaceJSON(pk)
	require.NoError(err)

	var pk2 cryptotypes.PubKey
	err = cdc.UnmarshalInterfaceJSON(bz, &pk2)
	require.NoError(err)
	require.True(pk2.Equals(pk))
}
