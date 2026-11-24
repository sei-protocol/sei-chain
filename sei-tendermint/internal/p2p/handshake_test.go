package p2p

import (
	"context"
	"fmt"
	"testing"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
)

// handshakePair runs both ends of a handshake over a pipe held open by s, and
// returns what each end saw: got[i] is the message handshake i received from
// its peer. End 0 is the server and end 1 is the client.
func handshakePair(
	ctx context.Context,
	s scope.Scope,
	specs [2]handshakeSpec,
) ([2]*handshakedConn, error) {
	conns := [2]tcp.Conn{}
	conns[0], conns[1] = tcp.TestPipe()
	var handles [2]scope.JoinHandle[*handshakedConn]
	for i, c := range conns {
		// The pipe outlives the handshake, and reports EOF once whichever end
		// the test is exercising closes. A handshake failure surfaces below.
		s.SpawnBg(func() error { _ = c.Run(ctx); return nil })
		handles[i] = s.Spawn1(func() (*handshakedConn, error) {
			spec := specs[i]
			spec.IsServer = i == 0
			return handshake(ctx, c, spec)
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
	specs [2]handshakeSpec,
) [2]*handshakedConn {
	t.Helper()
	var got [2]*handshakedConn
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		var err error
		got, err = handshakePair(ctx, s, specs)
		return err
	}))
	return got
}

func TestHandshakeAuthenticatesValidatorAuth(t *testing.T) {
	rng := utils.TestRng()
	nodeKeys := [2]NodeSecretKey{makeKey(rng), makeKey(rng)}
	validatorKeys := [2]atypes.SecretKey{atypes.GenSecretKey(rng), atypes.GenSecretKey(rng)}
	selfAddrs := [2]NodeAddress{
		{NodeID: nodeKeys[0].Public().NodeID(), Hostname: "validator-a.example", Port: 26656},
		{NodeID: nodeKeys[1].Public().NodeID(), Hostname: "validator-b.example", Port: 26656},
	}
	got := handshakePairIn(t,
		[2]handshakeSpec{
			{
				SelfAddr: utils.Some(selfAddrs[0]),
				NodeKey:  nodeKeys[0],
				Giga: utils.Some(gigaHandshakeSpec{
					ValidatorKey: utils.Some(validatorKeys[0]),
					EVMRPC:       testEVMRPCURL(),
				}),
			},
			{
				SelfAddr: utils.Some(selfAddrs[1]),
				NodeKey:  nodeKeys[1],
				Giga: utils.Some(gigaHandshakeSpec{
					ValidatorKey: utils.Some(validatorKeys[1]),
					EVMRPC:       testEVMRPCURL(),
				}),
			},
		})
	for i := range got {
		peer := 1 - i
		claim := got[i].msg.Giga.OrPanic("missing giga handshake").Validator.OrPanic("missing peer giga claim")
		require.Equal(t, validatorKeys[peer].Public(), claim.auth.Key())
		require.Equal(t, atypes.Challenge{SessionKey: atypes.SessionKey(got[i].conn.Challenge()), IsServer: peer == 0}, claim.auth.Msg())
		require.Equal(t, selfAddrs[peer], got[i].msg.SelfAddr.OrPanic("missing peer SelfAddr"))
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

	got := handshakePairIn(t,
		[2]handshakeSpec{
			{
				SelfAddr: utils.Some(selfAddr),
				NodeKey:  validatorNode,
				Giga: utils.Some(gigaHandshakeSpec{
					ValidatorKey: utils.Some(validatorKey),
					EVMRPC:       testEVMRPCURL(),
				}),
			},
			{
				NodeKey: fullnodeNode,
				Giga:    utils.Some(gigaHandshakeSpec{}),
			},
		})
	require.False(t, got[0].msg.Giga.OrPanic("missing giga handshake").Validator.IsPresent())
	require.Equal(t, fullnodeNode.Public(), got[0].msg.NodeAuth.Key())
	require.Equal(t, validatorKey.Public(), got[1].msg.Giga.OrPanic("missing giga handshake").Validator.OrPanic("validator omitted giga claim").auth.Key())
}

func TestHandshakeAuthenticatesGigaValidatorOnNonGiga(t *testing.T) {
	for _, isServer := range []bool{false, true} {
		t.Run(fmt.Sprintf("gigaIsServer=%v", isServer), func(t *testing.T) {
			rng := utils.TestRng()
			validatorKey := atypes.GenSecretKey(rng)
			specs := [2]handshakeSpec{{NodeKey: makeKey(rng)}, {NodeKey: makeKey(rng)}}
			gigaIndex := 1
			if isServer {
				gigaIndex = 0
			}
			specs[gigaIndex].Giga = utils.Some(gigaHandshakeSpec{
				ValidatorKey: utils.Some(validatorKey),
				EVMRPC:       testEVMRPCURL(),
			})
			got := handshakePairIn(t, specs)
			peer := got[1-gigaIndex]
			claim := peer.msg.Giga.OrPanic("missing giga handshake").Validator.OrPanic("missing validator auth").auth
			require.Equal(t, validatorKey.Public(), claim.Key())
			require.Equal(t, atypes.Challenge{SessionKey: atypes.SessionKey(peer.conn.Challenge()), IsServer: isServer}, claim.Msg())
			require.False(t, got[gigaIndex].msg.Giga.IsPresent())
		})
	}
}

func TestHandshakeRejectsBadValidatorAuthSig(t *testing.T) {
	rng := utils.TestRng()
	nodeKey := makeKey(rng)
	validatorKey := atypes.GenSecretKey(rng)
	selfAddr := NodeAddress{NodeID: nodeKey.Public().NodeID(), Hostname: "validator.example", Port: 26656}

	err := handshakeAgainst(t,
		handshakeSpec{
			SelfAddr: utils.Some(selfAddr),
			NodeKey:  nodeKey,
			IsServer: true,
		},
		func(ctx context.Context, sc *conn.SecretConnection) error {
			claim := validatorKey.Sign(atypes.Challenge{SessionKey: atypes.SessionKey(sc.Challenge()), IsServer: false})
			encoded := atypes.SignedChallengeConv.Encode(claim)
			encoded.Sig.Sig[0] ^= 1
			claim = utils.OrPanic1(atypes.SignedChallengeConv.Decode(encoded))
			return writeHandshake(ctx, sc, handshakeSpec{
				SelfAddr: utils.Some(selfAddr),
				NodeKey:  nodeKey,
				Giga:     utils.Some(gigaHandshakeSpec{}),
				IsServer: false,
			},
				utils.Some(claim))
		})
	require.Error(t, err)
}

func TestHandshakeRejectsDifferentChallenge(t *testing.T) {
	rng := utils.TestRng()
	nodeKey := makeKey(rng)
	validatorKey := atypes.GenSecretKey(rng)
	err := handshakeAgainst(t,
		handshakeSpec{
			NodeKey:  nodeKey,
			IsServer: true,
		},
		func(ctx context.Context, sc *conn.SecretConnection) error {
			challenge := sc.Challenge()
			challenge[0] ^= 1
			return writeHandshake(ctx, sc, handshakeSpec{
				NodeKey:  nodeKey,
				Giga:     utils.Some(gigaHandshakeSpec{}),
				IsServer: false,
			},
				utils.Some(validatorKey.Sign(atypes.Challenge{SessionKey: atypes.SessionKey(challenge), IsServer: false})))
		})
	require.Error(t, err)
}

func TestHandshakeAcceptsClaimWithoutSelfAddr(t *testing.T) {
	rng := utils.TestRng()
	nodeKeys := [2]NodeSecretKey{makeKey(rng), makeKey(rng)}
	validatorKey := atypes.GenSecretKey(rng)
	got := handshakePairIn(t,
		[2]handshakeSpec{
			{
				NodeKey: nodeKeys[0],
				Giga: utils.Some(gigaHandshakeSpec{
					ValidatorKey: utils.Some(validatorKey),
					EVMRPC:       testEVMRPCURL(),
				}),
			},
			{
				NodeKey: nodeKeys[1],
				Giga:    utils.Some(gigaHandshakeSpec{}),
			},
		})
	require.Equal(t, validatorKey.Public(), got[1].msg.Giga.OrPanic("missing giga handshake").Validator.OrPanic("missing claim").auth.Key())
}

func TestHandshakeValidatorLoopback(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		rng := utils.TestRng()
		nodeKey := makeKey(rng)
		validatorKey := atypes.GenSecretKey(rng)
		gs := gigaHandshakeSpec{
			ValidatorKey: utils.Some(validatorKey),
			EVMRPC:       testEVMRPCURL(),
		}
		got := handshakePairIn(t,
			[2]handshakeSpec{
				{
					NodeKey: nodeKey,
					Giga:    utils.Some(gs),
				},
				{
					NodeKey: nodeKey,
					Giga:    utils.Some(gs),
				},
			})
		for i := range got {
			require.Equal(t, nodeKey.Public(), got[i].msg.NodeAuth.Key())
			claim := got[i].msg.Giga.OrPanic("missing giga handshake").Validator.OrPanic("missing validator auth").auth
			require.Equal(t, validatorKey.Public(), claim.Key())
			require.Equal(t, atypes.Challenge{
				SessionKey: atypes.SessionKey(got[i].conn.Challenge()),
				IsServer:   i != 0,
			}, claim.Msg())
		}
	})

	for _, isServer := range []bool{false, true} {
		t.Run(fmt.Sprintf("reflected/isServer=%v", isServer), func(t *testing.T) {
			rng := utils.TestRng()
			nodeKey := makeKey(rng)
			validatorKey := atypes.GenSecretKey(rng)
			spec := handshakeSpec{
				NodeKey: nodeKey,
				Giga: utils.Some(gigaHandshakeSpec{
					ValidatorKey: utils.Some(validatorKey),
					EVMRPC:       testEVMRPCURL(),
				}),
				IsServer: isServer,
			}
			err := handshakeAgainst(t, spec,
				func(ctx context.Context, sc *conn.SecretConnection) error {
					raw, err := conn.ReadSizedMsg(ctx, sc, handshakeMsgMaxSize)
					if err != nil {
						return err
					}
					msg, err := handshakeMsgConv.Unmarshal(raw)
					if err != nil {
						return err
					}
					claim := msg.Giga.OrPanic("missing giga handshake").Validator.OrPanic("missing validator auth").auth
					return writeHandshake(ctx, sc, spec, utils.Some(claim))
				})
			require.Error(t, err)
		})
	}
}

func handshakeAgainst(
	t *testing.T,
	localSpec handshakeSpec,
	remote func(context.Context, *conn.SecretConnection) error,
) error {
	t.Helper()
	return scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		a, b := tcp.TestPipe()
		// The pipe's own errors are dropped, as in handshakePair: a rejecting
		// local side closes its write half while the pipe may still be inside
		// a socket write, and the resulting broken pipe is not a verdict.
		s.SpawnBg(func() error { _ = a.Run(ctx); return nil })
		s.SpawnBg(func() error { _ = b.Run(ctx); return nil })
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
			_, _ = conn.ReadSizedMsg(ctx, sc, handshakeMsgMaxSize)
			return nil
		})
		_, err := handshake(ctx, a, localSpec)
		return err
	})
}

func writeHandshake(
	ctx context.Context,
	sc *conn.SecretConnection,
	spec handshakeSpec,
	claim utils.Option[*atypes.Signed[atypes.Challenge]],
) error {
	var validator utils.Option[validatorHandshake]
	if c, ok := claim.Get(); ok {
		validator = utils.Some(validatorHandshake{auth: c, EVMRPC: testEVMRPCURL()})
	}
	var giga utils.Option[gigaHandshake]
	if spec.Giga.IsPresent() {
		giga = utils.Some(gigaHandshake{Validator: validator})
	}
	msg := &handshakeMsg{
		NodeAuth: spec.NodeKey.SignChallenge(sc.Challenge()),
		SelfAddr: spec.SelfAddr,
		PexAddrs: spec.PexAddrs,
		Giga:     giga,
	}
	if err := conn.WriteSizedMsg(ctx, sc, handshakeMsgConv.Marshal(msg)); err != nil {
		return err
	}
	return sc.Flush(ctx)
}
