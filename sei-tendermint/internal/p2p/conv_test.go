package p2p

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
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

func TestDecodeGigaClaimEVMRPC(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	sig := key.SignWithTag(gigaValidatorHandshakeTag, []byte("x"))
	claim := func(evm string) *pb.Handshake {
		return &pb.Handshake{
			ValidatorAuthKey: key.Public().Bytes(),
			ValidatorAuthSig: sig.Bytes(),
			EvmRpc:           &evm,
		}
	}
	for _, s := range []string{"http://validator.example:8545", "https://validator.example:8545"} {
		got, err := decodeGigaClaim(claim(s))
		require.NoError(t, err)
		require.Equal(t, s, got.OrPanic("missing claim").evmRPC)
	}
	for _, s := range []string{"file://localhost/rpc", "ws://validator.example:8545", "http://", "http://u:p@validator.example:8545"} {
		_, err := decodeGigaClaim(claim(s))
		require.Error(t, err)
	}
}

func TestDecodeGigaClaimRequiresAllFields(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	sig := key.SignWithTag(gigaValidatorHandshakeTag, []byte("x"))
	evmRPC := "http://validator.example:8545"
	full := func() *pb.Handshake {
		return &pb.Handshake{
			ValidatorAuthKey: key.Public().Bytes(),
			ValidatorAuthSig: sig.Bytes(),
			EvmRpc:           &evmRPC,
		}
	}
	for _, drop := range []func(*pb.Handshake){
		func(p *pb.Handshake) { p.ValidatorAuthKey = nil },
		func(p *pb.Handshake) { p.ValidatorAuthSig = nil },
		func(p *pb.Handshake) { p.EvmRpc = nil },
		func(p *pb.Handshake) { p.ValidatorAuthKey, p.ValidatorAuthSig = nil, nil },
		func(p *pb.Handshake) { p.ValidatorAuthKey, p.EvmRpc = nil, nil },
		func(p *pb.Handshake) { p.ValidatorAuthSig, p.EvmRpc = nil, nil },
	} {
		p := full()
		drop(p)
		_, err := decodeGigaClaim(p)
		require.Error(t, err)
	}

	// None of the three is a peer without a claim, rather than a malformed one.
	got, err := decodeGigaClaim(&pb.Handshake{})
	require.NoError(t, err)
	require.False(t, got.IsPresent())
}

func TestHandshakeWireguardRejectsOversizedEvmRPC(t *testing.T) {
	tooLong := strings.Repeat("a", 2049)
	raw, err := proto.Marshal(&pb.Handshake{EvmRpc: &tooLong})
	require.NoError(t, err)
	require.Error(t, protoutils.Scan[*pb.Handshake](raw))

	ok := strings.Repeat("a", 2048)
	raw, err = proto.Marshal(&pb.Handshake{EvmRpc: &ok})
	require.NoError(t, err)
	require.NoError(t, protoutils.Scan[*pb.Handshake](raw))
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
