//go:build ledger && test_ledger_mock
// +build ledger,test_ledger_mock

package ledger

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func TestSignWithPath(t *testing.T) {
	path := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)
	msg := []byte("test message to sign")

	sig, pubKey, err := SignWithPath(path, msg)
	require.NoError(t, err)
	require.NotNil(t, sig)
	require.NotNil(t, pubKey)
	require.Len(t, sig, 64) // R||S format

	// Verify the signature is valid
	require.True(t, pubKey.VerifySignature(msg, sig))
}

func TestSignWithPath_DifferentPaths(t *testing.T) {
	msg := []byte("test message")

	// Test with different account indices
	path1 := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)
	path2 := *hd.NewFundraiserParams(1, types.GetConfig().GetCoinType(), 0)

	sig1, pub1, err := SignWithPath(path1, msg)
	require.NoError(t, err)

	sig2, pub2, err := SignWithPath(path2, msg)
	require.NoError(t, err)

	// Different paths should produce different pubkeys
	require.False(t, pub1.Equals(pub2))

	// Each signature should be valid for its own pubkey
	require.True(t, pub1.VerifySignature(msg, sig1))
	require.True(t, pub2.VerifySignature(msg, sig2))

	// But not for the other
	require.False(t, pub1.VerifySignature(msg, sig2))
	require.False(t, pub2.VerifySignature(msg, sig1))
}

func TestNewPrivKeySecp256k1Unsafe(t *testing.T) {
	path := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)

	privKey, err := NewPrivKeySecp256k1Unsafe(path)
	require.NoError(t, err)
	require.NotNil(t, privKey)
	require.NotNil(t, privKey.PubKey())
}

func TestNewPrivKeySecp256k1(t *testing.T) {
	path := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)

	privKey, addr, err := NewPrivKeySecp256k1(path, "sei")
	require.NoError(t, err)
	require.NotNil(t, privKey)
	require.NotEmpty(t, addr)
	require.Contains(t, addr, "sei")
}

func TestPrivKeyLedgerSecp256k1_Sign(t *testing.T) {
	path := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)

	privKey, err := NewPrivKeySecp256k1Unsafe(path)
	require.NoError(t, err)

	msg := []byte("message to sign")
	sig, err := privKey.Sign(msg)
	require.NoError(t, err)
	require.NotNil(t, sig)

	// Verify signature
	require.True(t, privKey.PubKey().VerifySignature(msg, sig))
}

func TestPrivKeyLedgerSecp256k1_ValidateKey(t *testing.T) {
	path := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)

	privKey, err := NewPrivKeySecp256k1Unsafe(path)
	require.NoError(t, err)

	// Cast to PrivKeyLedgerSecp256k1 to call ValidateKey
	pkl := privKey.(PrivKeyLedgerSecp256k1)
	err = pkl.ValidateKey()
	require.NoError(t, err)
}

func TestPrivKeyLedgerSecp256k1_Equals(t *testing.T) {
	path1 := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)
	path2 := *hd.NewFundraiserParams(1, types.GetConfig().GetCoinType(), 0)

	privKey1, err := NewPrivKeySecp256k1Unsafe(path1)
	require.NoError(t, err)

	privKey1Again, err := NewPrivKeySecp256k1Unsafe(path1)
	require.NoError(t, err)

	privKey2, err := NewPrivKeySecp256k1Unsafe(path2)
	require.NoError(t, err)

	pkl1 := privKey1.(PrivKeyLedgerSecp256k1)
	pkl1Again := privKey1Again.(PrivKeyLedgerSecp256k1)
	pkl2 := privKey2.(PrivKeyLedgerSecp256k1)

	require.True(t, pkl1.Equals(pkl1Again))
	require.False(t, pkl1.Equals(pkl2))
}

func TestPrivKeyLedgerSecp256k1_Type(t *testing.T) {
	path := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)

	privKey, err := NewPrivKeySecp256k1Unsafe(path)
	require.NoError(t, err)

	pkl := privKey.(PrivKeyLedgerSecp256k1)
	require.Equal(t, "PrivKeyLedgerSecp256k1", pkl.Type())
}

func TestPrivKeyLedgerSecp256k1_Bytes(t *testing.T) {
	path := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)

	privKey, err := NewPrivKeySecp256k1Unsafe(path)
	require.NoError(t, err)

	pkl := privKey.(PrivKeyLedgerSecp256k1)
	bytes := pkl.Bytes()
	require.NotEmpty(t, bytes)
}

func TestShowAddress(t *testing.T) {
	path := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)

	privKey, err := NewPrivKeySecp256k1Unsafe(path)
	require.NoError(t, err)

	// ShowAddress should succeed with matching pubkey
	err = ShowAddress(path, privKey.PubKey(), "sei")
	require.NoError(t, err)
}

func TestShowAddress_Mismatch(t *testing.T) {
	path1 := *hd.NewFundraiserParams(0, types.GetConfig().GetCoinType(), 0)
	path2 := *hd.NewFundraiserParams(1, types.GetConfig().GetCoinType(), 0)

	// Get pubkey from path2
	privKey2, err := NewPrivKeySecp256k1Unsafe(path2)
	require.NoError(t, err)

	// ShowAddress with path1 but pubkey from path2 should fail
	err = ShowAddress(path1, privKey2.PubKey(), "sei")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match")
}

func TestConvertDERtoBER(t *testing.T) {
	// Test with already 64-byte signature (passthrough)
	sig64 := make([]byte, 64)
	for i := range sig64 {
		sig64[i] = byte(i)
	}

	result, err := convertDERtoBER(sig64)
	require.NoError(t, err)
	require.Equal(t, sig64, result)
}
