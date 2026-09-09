package p2p

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

func signGigaClaim(
	key atypes.SecretKey,
	challenge conn.Challenge,
	nodeKey NodePublicKey,
	selfAddr NodeAddress,
	evmRPC url.URL,
) gigaHandshakeClaim {
	return gigaHandshakeClaim{
		Validator: key.Public(),
		sig: key.SignWithTag(
			gigaValidatorHandshakeTag,
			gigaClaimSignBytes(challenge, nodeKey, selfAddr, evmRPC.String()),
		),
		evmRPC: evmRPC.String(),
	}
}

// handshakePair runs both ends of a handshake over a pipe held open by s, and
// returns what each end saw: got[i] is the message handshake i received from
// its peer.
func handshakePair(
	ctx context.Context,
	s scope.Scope,
	keys [2]NodeSecretKey,
	specs [2]handshakeSpec,
	offers [2]utils.Option[handshakeOffer],
) ([2]*handshakedConn, error) {
	conns := [2]tcp.Conn{}
	conns[0], conns[1] = tcp.TestPipe()
	var handles [2]scope.JoinHandle[*handshakedConn]
	for i, c := range conns {
		// The pipe outlives the handshake, and reports EOF once whichever end
		// the test is exercising closes. A handshake failure surfaces below.
		s.SpawnBg(func() error { _ = c.Run(ctx); return nil })
		handles[i] = scope.Spawn1(s, func() (*handshakedConn, error) {
			return handshake(ctx, c, keys[i], specs[i], offers[i])
		})
	}
	var got [2]*handshakedConn
	for i, h := range handles {
		hConn, err := h.Join(ctx)
		if err != nil {
			return got, fmt.Errorf("handshake[%v]: %w", i, err)
		}
		got[i] = hConn
	}
	return got, nil
}

// handshakePairIn runs handshakePair in its own scope, for tests that only
// inspect the messages and do not use the connections.
func handshakePairIn(
	t *testing.T,
	keys [2]NodeSecretKey,
	specs [2]handshakeSpec,
	offers [2]utils.Option[handshakeOffer],
) [2]*handshakedConn {
	t.Helper()
	var got [2]*handshakedConn
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		var err error
		got, err = handshakePair(ctx, s, keys, specs, offers)
		return err
	}))
	return got
}

func TestHandshakeAuthenticatesGigaClaim(t *testing.T) {
	rng := utils.TestRng()
	nodeKeys := [2]NodeSecretKey{makeKey(rng), makeKey(rng)}
	validatorKeys := [2]atypes.SecretKey{atypes.GenSecretKey(rng), atypes.GenSecretKey(rng)}
	selfAddrs := [2]NodeAddress{
		{NodeID: nodeKeys[0].Public().NodeID(), Hostname: "validator-a.example", Port: 26656},
		{NodeID: nodeKeys[1].Public().NodeID(), Hostname: "validator-b.example", Port: 26656},
	}
	evmRPCs := [2]url.URL{
		*utils.OrPanic1(url.Parse("http://validator-a.example:8545")),
		*utils.OrPanic1(url.Parse("http://validator-b.example:8545")),
	}
	got := handshakePairIn(t, nodeKeys, [2]handshakeSpec{
		{SelfAddr: utils.Some(selfAddrs[0]), SeiGigaConnection: true},
		{SelfAddr: utils.Some(selfAddrs[1]), SeiGigaConnection: true},
	}, [2]utils.Option[handshakeOffer]{
		utils.Some(handshakeOffer{ValidatorKey: validatorKeys[0], EVMRPC: evmRPCs[0]}),
		utils.Some(handshakeOffer{ValidatorKey: validatorKeys[1], EVMRPC: evmRPCs[1]}),
	})
	for i := range got {
		peer := 1 - i
		claim := got[i].msg.GigaClaim.OrPanic("missing peer giga claim")
		require.Equal(t, validatorKeys[peer].Public(), claim.Validator)
		require.Equal(t, selfAddrs[peer], got[i].msg.SelfAddr.OrPanic("missing peer SelfAddr"))
		require.Equal(t, evmRPCs[peer].String(), claim.evmRPC)
	}
}

func TestHandshakeAcceptsFullnodeWithoutClaim(t *testing.T) {
	rng := utils.TestRng()
	validatorNode := makeKey(rng)
	fullnodeNode := makeKey(rng)
	validatorKey := atypes.GenSecretKey(rng)
	selfAddr := NodeAddress{
		NodeID:   validatorNode.Public().NodeID(),
		Hostname: "validator.example",
		Port:     26656,
	}
	evmRPC := *utils.OrPanic1(url.Parse("http://validator.example:8545"))
	got := handshakePairIn(t,
		[2]NodeSecretKey{validatorNode, fullnodeNode},
		[2]handshakeSpec{
			{SelfAddr: utils.Some(selfAddr), SeiGigaConnection: true},
			{SeiGigaConnection: true},
		},
		[2]utils.Option[handshakeOffer]{
			utils.Some(handshakeOffer{ValidatorKey: validatorKey, EVMRPC: evmRPC}),
			utils.None[handshakeOffer](),
		},
	)
	require.False(t, got[0].msg.GigaClaim.IsPresent())
	require.Equal(t, fullnodeNode.Public(), got[0].msg.NodeAuth.Key())
	require.Equal(t, validatorKey.Public(), got[1].msg.GigaClaim.OrPanic("validator omitted giga claim").Validator)
}

func TestHandshakeRejectsClaimOnNonGiga(t *testing.T) {
	rng := utils.TestRng()
	nodeKey := makeKey(rng)
	validatorKey := atypes.GenSecretKey(rng)
	selfAddr := NodeAddress{NodeID: nodeKey.Public().NodeID(), Hostname: "validator.example", Port: 26656}
	evmRPC := *utils.OrPanic1(url.Parse("http://validator.example:8545"))
	err := handshakeAgainst(t, nodeKey, handshakeSpec{
		SelfAddr:          utils.Some(selfAddr),
		SeiGigaConnection: true,
	}, utils.Some(handshakeOffer{ValidatorKey: validatorKey, EVMRPC: evmRPC}),
		func(ctx context.Context, sc *conn.SecretConnection) error {
			return writeHandshake(ctx, sc, nodeKey, handshakeSpec{
				SelfAddr:          utils.Some(selfAddr),
				SeiGigaConnection: false,
			}, utils.Some(signGigaClaim(validatorKey, sc.Challenge(), nodeKey.Public(), selfAddr, evmRPC)))
		},
	)
	require.ErrorIs(t, err, errGigaClaimOnNonGiga)
}

func TestHandshakeRejectsBadGigaClaimSig(t *testing.T) {
	rng := utils.TestRng()
	nodeKey := makeKey(rng)
	validatorKey := atypes.GenSecretKey(rng)
	selfAddr := NodeAddress{NodeID: nodeKey.Public().NodeID(), Hostname: "validator.example", Port: 26656}
	evmRPC := *utils.OrPanic1(url.Parse("http://validator.example:8545"))
	err := handshakeAgainst(t, nodeKey, handshakeSpec{
		SelfAddr:          utils.Some(selfAddr),
		SeiGigaConnection: true,
	}, utils.None[handshakeOffer](),
		func(ctx context.Context, sc *conn.SecretConnection) error {
			claim := signGigaClaim(validatorKey, sc.Challenge(), nodeKey.Public(), selfAddr, evmRPC)
			sig := claim.sig.Bytes()
			sig[0] ^= 1
			claim.sig = utils.OrPanic1(ed25519.SignatureFromBytes(sig))
			return writeHandshake(ctx, sc, nodeKey, handshakeSpec{
				SelfAddr:          utils.Some(selfAddr),
				SeiGigaConnection: true,
			}, utils.Some(claim))
		},
	)
	require.Error(t, err)
}

// A claim is meaningless without the address it is signed over, so neither end
// of the handshake may produce or accept one.
func TestHandshakeRejectsClaimWithoutSelfAddr(t *testing.T) {
	rng := utils.TestRng()
	nodeKey := makeKey(rng)
	validatorKey := atypes.GenSecretKey(rng)
	selfAddr := NodeAddress{NodeID: nodeKey.Public().NodeID(), Hostname: "validator.example", Port: 26656}
	evmRPC := *utils.OrPanic1(url.Parse("http://validator.example:8545"))
	received := handshakeAgainst(t, nodeKey, handshakeSpec{
		SelfAddr:          utils.Some(selfAddr),
		SeiGigaConnection: true,
	}, utils.None[handshakeOffer](),
		func(ctx context.Context, sc *conn.SecretConnection) error {
			return writeHandshake(ctx, sc, nodeKey, handshakeSpec{
				SeiGigaConnection: true,
			}, utils.Some(signGigaClaim(validatorKey, sc.Challenge(), nodeKey.Public(), selfAddr, evmRPC)))
		},
	)
	require.ErrorIs(t, received, errGigaClaimRequiresAddr)

	offered := handshakeAgainst(t, nodeKey, handshakeSpec{SeiGigaConnection: true},
		utils.Some(handshakeOffer{ValidatorKey: validatorKey, EVMRPC: evmRPC}),
		func(context.Context, *conn.SecretConnection) error { return nil },
	)
	require.ErrorIs(t, offered, errGigaClaimRequiresAddr)
}

func handshakeAgainst(
	t *testing.T,
	localKey NodeSecretKey,
	localSpec handshakeSpec,
	localOffer utils.Option[handshakeOffer],
	remote func(context.Context, *conn.SecretConnection) error,
) error {
	t.Helper()
	return scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		a, b := tcp.TestPipe()
		s.SpawnBg(func() error { return utils.IgnoreCancel(a.Run(ctx)) })
		s.SpawnBg(func() error { return utils.IgnoreCancel(b.Run(ctx)) })
		s.Spawn(func() error {
			sc, err := conn.MakeSecretConnection(ctx, b)
			if err != nil {
				return err
			}
			if err := remote(ctx, sc); err != nil {
				return err
			}
			// Only the local handshake's verdict is asserted. This read keeps
			// the pipe open until it reaches one, and sees EOF when the local
			// side rejects and closes first.
			_, _ = conn.ReadSizedMsg(ctx, sc, uint64((&pb.Handshake{}).MaxSize()))
			return nil
		})
		_, err := handshake(ctx, a, localKey, localSpec, localOffer)
		return err
	})
}

func writeHandshake(
	ctx context.Context,
	sc *conn.SecretConnection,
	key NodeSecretKey,
	spec handshakeSpec,
	claim utils.Option[gigaHandshakeClaim],
) error {
	msg := &handshakeMsg{NodeAuth: key.SignChallenge(sc.Challenge()), handshakeSpec: spec}
	if c, ok := claim.Get(); ok {
		msg.GigaClaim = utils.Some(c)
	}
	if err := conn.WriteSizedMsg(ctx, sc, handshakeMsgConv.Marshal(msg)); err != nil {
		return err
	}
	return sc.Flush(ctx)
}
