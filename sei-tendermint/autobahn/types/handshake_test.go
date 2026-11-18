package types_test

import (
	"crypto/sha256"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestHandshakeSignature(t *testing.T) {
	rng := utils.TestRng()
	key := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	validator := types.SecretKeyFromED25519(key)
	var challenge types.Challenge
	utils.OrPanic1(rng.Read(challenge[:]))
	msg := types.Handshake{
		Challenge: challenge,
		NodeKey:   types.GenPublicKey(rng).ED25519(),
		SelfAddr:  "a",
		EVMRPC:    "bc",
	}
	sig := validator.SignHandshake(msg)
	require.NoError(t, validator.Public().VerifyHandshake(msg, sig))
	decoded, err := types.HandshakeSigFromBytes(sig.Bytes())
	require.NoError(t, err)
	require.NoError(t, validator.Public().VerifyHandshake(msg, decoded))
	_, err = types.HandshakeSigFromBytes(sig.Bytes()[:63])
	require.Error(t, err)

	require.NoError(t, types.HandshakeConv.Test(msg))
	for _, malform := range []func(*pb.Handshake){
		func(p *pb.Handshake) { p.Challenge = nil },
		func(p *pb.Handshake) { p.Challenge = p.Challenge[:31] },
		func(p *pb.Handshake) { p.Challenge = append(p.Challenge, 0) },
		func(p *pb.Handshake) { p.NodeKey = nil },
		func(p *pb.Handshake) { p.SelfAddr = nil },
		func(p *pb.Handshake) { p.EvmRpc = nil },
	} {
		p := types.HandshakeConv.Encode(msg)
		malform(p)
		_, err := types.HandshakeConv.Decode(p)
		require.Error(t, err)
	}

	// The signature covers the SHA-256 hash of the canonical protobuf payload.
	payload := []byte{0x0a, 32}
	payload = append(payload, challenge[:]...)
	payload = append(payload, 0x12, 32)
	payload = append(payload, msg.NodeKey.Bytes()...)
	payload = append(payload, 0x1a, 1, 'a', 0x22, 2, 'b', 'c')
	hash := sha256.Sum256(payload)
	require.Equal(t, hash, [32]byte(msg.Hash()))
	tag := utils.OrPanic1(ed25519.NewTag("SEI_GIGA_VALIDATOR_HANDSHAKE_V1"))
	require.Equal(t, key.SignWithTag(tag, hash[:]).Bytes(), sig.Bytes())
	require.Error(t, types.GenPublicKey(rng).VerifyHandshake(msg, sig))

	otherChallenge := challenge
	otherChallenge[0] ^= 1
	for _, changed := range []types.Handshake{
		{Challenge: otherChallenge, NodeKey: msg.NodeKey, SelfAddr: msg.SelfAddr, EVMRPC: msg.EVMRPC},
		{Challenge: challenge, NodeKey: types.GenPublicKey(rng).ED25519(), SelfAddr: msg.SelfAddr, EVMRPC: msg.EVMRPC},
		{Challenge: challenge, NodeKey: msg.NodeKey, SelfAddr: "x", EVMRPC: msg.EVMRPC},
		{Challenge: challenge, NodeKey: msg.NodeKey, SelfAddr: msg.SelfAddr, EVMRPC: "x"},
		{Challenge: challenge, NodeKey: msg.NodeKey, SelfAddr: "ab", EVMRPC: "c"},
	} {
		require.Error(t, validator.Public().VerifyHandshake(changed, sig))
	}
}
