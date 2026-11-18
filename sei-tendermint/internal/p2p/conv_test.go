package p2p

import (
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pb"
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
	}
}

func TestDecodeGigaClaimRequiresAllFields(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	sig := key.SignChallenge(atypes.Challenge{})
	full := func() *pb.Handshake {
		return &pb.Handshake{
			ValidatorAuthKey: key.Public().Bytes(),
			ValidatorAuthSig: sig.Bytes(),
		}
	}
	claim, err := decodeGigaClaim(full())
	require.NoError(t, err)
	require.Equal(t, key.Public(), claim.OrPanic("missing claim").Key())
	require.Equal(t, sig.Bytes(), claim.OrPanic("missing claim").Bytes())

	for _, drop := range []func(*pb.Handshake){
		func(p *pb.Handshake) { p.ValidatorAuthKey = nil },
		func(p *pb.Handshake) { p.ValidatorAuthSig = nil },
	} {
		p := full()
		drop(p)
		_, err := decodeGigaClaim(p)
		require.Error(t, err)
	}

	// Neither field being present means a peer without a claim.
	got, err := decodeGigaClaim(&pb.Handshake{})
	require.NoError(t, err)
	require.False(t, got.IsPresent())
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
