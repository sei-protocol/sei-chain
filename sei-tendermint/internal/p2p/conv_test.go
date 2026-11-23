package p2p

import (
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestHandshakeMsgConv(t *testing.T) {
	rng := utils.TestRng()
	for range 5 {
		require.NoError(t, nodePublicKeyConv.Test(makeKey(rng).Public()))

		challenge := atypes.GetChallenge(rng)
		key := makeKey(rng)
		msg := &handshakeMsg{
			NodeAuth: key.SignChallenge(conn.Challenge(challenge.SessionKey)),
			SelfAddr: utils.Some(makeAddrFor(rng, key.Public().NodeID())),
			PexAddrs: rng.GenSlice(makeAddr),
		}
		require.NoError(t, handshakeMsgConv.Test(msg))
		msg.Giga = utils.Some(gigaHandshake{})
		require.NoError(t, handshakeMsgConv.Test(msg))
		msg.Giga = utils.Some(gigaHandshake{Validator: utils.Some(validatorHandshake{auth: atypes.GenSigned(rng, challenge), EVMRPC: testEVMRPCURL()})})
		require.NoError(t, handshakeMsgConv.Test(msg))
	}
}

func TestHandshakeMsgConvRejectsInvalidValidatorFields(t *testing.T) {
	rng := utils.TestRng()
	key := makeKey(rng)
	challenge := atypes.Challenge{}
	msg := &handshakeMsg{
		NodeAuth: key.SignChallenge(conn.Challenge(challenge.SessionKey)),
		Giga: utils.Some(gigaHandshake{
			Validator: utils.Some(validatorHandshake{auth: atypes.GenSigned(rng, challenge), EVMRPC: testEVMRPCURL()}),
		}),
	}
	for _, malform := range []func(*apb.SignedChallenge){
		func(p *apb.SignedChallenge) { p.Msg.SessionKey = p.Msg.SessionKey[:31] },
		func(p *apb.SignedChallenge) { p.Msg.SessionKey = append(p.Msg.SessionKey, 0) },
	} {
		p := handshakeMsgConv.Encode(msg)
		malform(p.Giga.Validator.Auth)
		_, err := handshakeMsgConv.Decode(p)
		require.Error(t, err)
	}
	for _, malform := range []func(*pb.ValidatorHandshake){
		func(p *pb.ValidatorHandshake) { p.EvmRpcUrl = new("http://peer.example:8545") },
		func(p *pb.ValidatorHandshake) { p.EvmRpcUrl = new("https://peer.example:8545") },
		func(p *pb.ValidatorHandshake) { p.EvmRpcUrl = new("file://localhost/rpc") },
		func(p *pb.ValidatorHandshake) { p.EvmRpcUrl = new("ws://") },
		func(p *pb.ValidatorHandshake) { p.EvmRpcUrl = new("ws://u:p@peer.example:8546") },
	} {
		p := handshakeMsgConv.Encode(msg)
		malform(p.Giga.Validator)
		_, err := handshakeMsgConv.Decode(p)
		require.Error(t, err)
	}
}

func TestNodePublicKeyFromString(t *testing.T) {
	rng := utils.TestRng()
	key := makeKey(rng).Public()

	// Round-trip: String() -> FromString()
	s := key.String()
	parsed, err := NodePublicKeyFromString(s)
	require.NoError(t, err)
	require.Equal(t, key, parsed)

	// Round-trip: MarshalText -> UnmarshalText
	text, err := key.MarshalText()
	require.NoError(t, err)
	var unmarshaled NodePublicKey
	require.NoError(t, unmarshaled.UnmarshalText(text))
	require.Equal(t, key, unmarshaled)
}

func TestNodePublicKeyFromString_Invalid(t *testing.T) {
	// Missing prefix.
	_, err := NodePublicKeyFromString("ed25519:public:aabb")
	require.Error(t, err)

	// Wrong prefix.
	_, err = NodePublicKeyFromString("validator:ed25519:public:aabb")
	require.Error(t, err)

	// Bad hex after correct prefix.
	_, err = NodePublicKeyFromString("node:ed25519:public:not_hex")
	require.Error(t, err)

	// Empty string.
	_, err = NodePublicKeyFromString("")
	require.Error(t, err)
}
