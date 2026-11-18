package p2p

import (
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestHandshakeMsgConv(t *testing.T) {
	rng := utils.TestRng()
	for range 5 {
		require.NoError(t, nodePublicKeyConv.Test(makeKey(rng).Public()))

		var challenge atypes.Challenge
		utils.OrPanic1(rng.Read(challenge[:]))
		key := makeKey(rng)
		msg := &handshakeMsg{
			NodeAuth: key.SignChallenge(challenge),
			handshakeSpec: handshakeSpec{
				SelfAddr:          utils.Some(makeAddrFor(rng, key.Public().NodeID())),
				PexAddrs:          rng.GenSlice(makeAddr),
				SeiGigaConnection: utils.GenBool(rng),
			},
		}
		require.NoError(t, handshakeMsgConv.Test(msg))
		msg.SeiGigaConnection = true
		msg.ValidatorAuth = utils.Some(atypes.GenSigned(rng, challenge))
		require.NoError(t, handshakeMsgConv.Test(msg))
	}
}

func TestHandshakeMsgConvRejectsMalformedValidatorAuth(t *testing.T) {
	rng := utils.TestRng()
	key := makeKey(rng)
	challenge := atypes.Challenge{}
	msg := &handshakeMsg{
		NodeAuth:      key.SignChallenge(challenge),
		handshakeSpec: handshakeSpec{SeiGigaConnection: true},
		ValidatorAuth: utils.Some(atypes.GenSigned(rng, challenge)),
	}
	for _, malform := range []func(*apb.SignedChallenge){
		func(p *apb.SignedChallenge) { p.Msg = nil },
		func(p *apb.SignedChallenge) { p.Msg.Data = p.Msg.Data[:31] },
		func(p *apb.SignedChallenge) { p.Msg.Data = append(p.Msg.Data, 0) },
		func(p *apb.SignedChallenge) { p.Sig = nil },
		func(p *apb.SignedChallenge) { p.Sig.Key = nil },
		func(p *apb.SignedChallenge) { p.Sig.Sig = nil },
	} {
		p := handshakeMsgConv.Encode(msg)
		malform(p.ValidatorAuth)
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
