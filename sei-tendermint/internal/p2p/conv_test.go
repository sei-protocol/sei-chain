package p2p

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestHandshakeMsgConv(t *testing.T) {
	rng := utils.TestRng()
	for range 5 {
		require.NoError(t, nodePublicKeyConv.Test(makeKey(rng).Public()))

		var challenge conn.Challenge
		utils.OrPanic1(rng.Read(challenge[:]))
		key := makeKey(rng)
		msg := &handshakeMsg{
			NodeAuth: key.SignChallenge(challenge),
			handshakeSpec: handshakeSpec{
				SelfAddr:          utils.Some(makeAddrFor(rng, key.Public().NodeID())),
				PexAddrs:          utils.GenSlice(rng, makeAddr),
				SeiGigaConnection: utils.GenBool(rng),
			},
		}
		require.NoError(t, handshakeMsgConv.Test(msg))
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
