package p2p

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"slices"

	gogoproto "github.com/gogo/protobuf/proto"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pb"
	gogopb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var (
	gigaValidatorHandshakeTag = utils.OrPanic1(ed25519.NewTag("SEI_GIGA_VALIDATOR_HANDSHAKE_V1"))
	errMissingGigaClaim       = errors.New("missing validator_auth_key, validator_auth_sig, and evm_rpc")
	errGigaClaimRequiresAddr  = errors.New("validator giga handshake requires SelfAddr")
	errGigaClaimOnNonGiga     = errors.New("validator giga claim on non-giga connection")
)

// handshakeOffer is the Autobahn committee identity this node claims on a giga handshake.
type handshakeOffer struct {
	ValidatorKey atypes.SecretKey
	EVMRPC       url.URL
}

// gigaClaimSignBytes is the tagged payload for SEI_GIGA_VALIDATOR_HANDSHAKE_V1:
// challenge || node_public_key || uvarint(len(self_addr)) || self_addr || uvarint(len(evm_rpc)) || evm_rpc.
func gigaClaimSignBytes(
	challenge conn.Challenge,
	nodeKey NodePublicKey,
	selfAddr NodeAddress,
	evmRPC string,
) []byte {
	b := slices.Clone(challenge[:])
	b = append(b, nodeKey.Bytes()...)
	// Length-prefixed: without it, bytes could move across the address/URL
	// boundary and two different claims would sign the same payload.
	for _, s := range []string{selfAddr.String(), evmRPC} {
		b = binary.AppendUvarint(b, uint64(len(s)))
		b = append(b, s...)
	}
	return b
}

type handshakedConn struct {
	conn *conn.SecretConnection
	msg  *handshakeMsg
}

func handshake(
	ctx context.Context,
	c conn.Conn,
	key NodeSecretKey,
	spec handshakeSpec,
	offer utils.Option[handshakeOffer],
) (*handshakedConn, error) {
	// Checked before the connection so a local misconfiguration is reported as
	// itself, rather than as whichever handshake step the abort trips first.
	o, offering := offer.Get()
	selfAddr, hasSelfAddr := spec.SelfAddr.Get()
	if offering && !hasSelfAddr {
		return nil, errGigaClaimRequiresAddr
	}
	if offering {
		if err := utils.CheckHTTPURL(o.EVMRPC); err != nil {
			return nil, fmt.Errorf("EvmRpc: %w", err)
		}
	}
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*handshakedConn, error) {
		sc, err := conn.MakeSecretConnection(ctx, c)
		if err != nil {
			return nil, err
		}
		s.Spawn(func() error {
			msg := &handshakeMsg{
				NodeAuth:      key.SignChallenge(sc.Challenge()),
				handshakeSpec: spec,
			}
			if offering {
				msg.GigaClaim = utils.Some(gigaHandshakeClaim{
					Validator: o.ValidatorKey.Public(),
					sig: o.ValidatorKey.SignWithTag(
						gigaValidatorHandshakeTag,
						gigaClaimSignBytes(sc.Challenge(), key.Public(), selfAddr, o.EVMRPC.String()),
					),
					evmRPC: o.EVMRPC.String(),
				})
			}
			if err := conn.WriteSizedMsg(ctx, sc, handshakeMsgConv.Marshal(msg)); err != nil {
				return fmt.Errorf("conn.WriteSizedMsg(): %w", err)
			}
			if err := sc.Flush(ctx); err != nil {
				return fmt.Errorf("c.Flush(): %w", err)
			}
			return nil
		})
		msgBytes, err := conn.ReadSizedMsg(ctx, sc, uint64((&pb.Handshake{}).MaxSize()))
		if err != nil {
			return nil, fmt.Errorf("conn.ReadSizedMsg(): %w", err)
		}
		msg, err := handshakeMsgConv.Unmarshal(msgBytes)
		if err != nil {
			return nil, fmt.Errorf("handshakeMsgConv.Unmarshal(): %w", err)
		}
		if err := msg.NodeAuth.Verify(sc.Challenge()); err != nil {
			return nil, fmt.Errorf("handshakeMsg.NodeAuth.Verify(): %w", err)
		}
		if selfAddr, ok := msg.SelfAddr.Get(); ok {
			if got, want := selfAddr.NodeID, msg.NodeAuth.Key().NodeID(); got != want {
				return nil, fmt.Errorf("handshakeMsg.SelfAddr.NodeID = %v, want %v", got, want)
			}
		}
		if claim, ok := msg.GigaClaim.Get(); ok {
			if !msg.SeiGigaConnection {
				return nil, errGigaClaimOnNonGiga
			}
			selfAddr, ok := msg.SelfAddr.Get()
			if !ok {
				return nil, errGigaClaimRequiresAddr
			}
			if err := claim.Validator.VerifyWithTag(
				gigaValidatorHandshakeTag,
				gigaClaimSignBytes(sc.Challenge(), msg.NodeAuth.Key(), selfAddr, claim.evmRPC),
				claim.sig,
			); err != nil {
				return nil, fmt.Errorf("handshakeMsg.GigaClaim: %w", err)
			}
		}
		if len(msg.PexAddrs) > MaxPexAddrs {
			return nil, fmt.Errorf("len(handshakeMsg.PexAddrs) = %v, want <= %v", len(msg.PexAddrs), MaxPexAddrs)
		}
		return &handshakedConn{conn: sc, msg: msg}, nil
	})
}

// handshake handshakes with a peer, validating the peer's information. If
// dialAddr is given, we check that the peer's info matches it.
// Closes the tcpConn if case of any error.
func exchangeNodeInfo(ctx context.Context, hConn *handshakedConn, nodeInfo types.NodeInfo) (types.NodeInfo, error) {
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (types.NodeInfo, error) {
		s.Spawn(func() error {
			// Marshalling should always succeed.
			if err := conn.WriteSizedMsg(ctx, hConn.conn, utils.OrPanic1(gogoproto.Marshal(nodeInfo.ToProto()))); err != nil {
				return fmt.Errorf("conn.WriteSizedMsg(<nodeInfo>): %w", err)
			}
			return hConn.conn.Flush(ctx)
		})
		nodeInfoBytes, err := conn.ReadSizedMsg(ctx, hConn.conn, uint64(types.MaxNodeInfoSize())) //nolint:gosec // MaxNodeInfoSize() returns a small positive constant
		if err != nil {
			return types.NodeInfo{}, fmt.Errorf("conn.ReadSizedMsg(): %w", err)
		}
		var nodeInfoProto gogopb.NodeInfo
		if err := gogoproto.Unmarshal(nodeInfoBytes, &nodeInfoProto); err != nil {
			return types.NodeInfo{}, fmt.Errorf("gogoproto.Unmarshal(): %w", err)
		}
		peerInfo, err := types.NodeInfoFromProto(&nodeInfoProto)
		if err != nil {
			return types.NodeInfo{}, fmt.Errorf("types.NodeInfoFromProto(): %w", err)
		}

		// Authenticate the peer first.
		peerID := hConn.msg.NodeAuth.Key().NodeID()
		if peerID != peerInfo.NodeID {
			return types.NodeInfo{}, fmt.Errorf("peer's public key did not match its node ID %q (expected %q)", peerInfo.NodeID, peerID)
		}
		// Validate the received info.
		if err := peerInfo.Validate(); err != nil {
			return types.NodeInfo{}, fmt.Errorf("invalid handshake NodeInfo: %w", err)
		}
		if peerInfo.Network != nodeInfo.Network {
			return types.NodeInfo{}, errBadNetwork{fmt.Errorf("connected to peer from wrong network, %q, removed from peer store", peerInfo.Network)}
		}
		if err := nodeInfo.CompatibleWith(peerInfo); err != nil {
			return types.NodeInfo{}, ErrRejected{
				err:            err,
				id:             peerInfo.ID(),
				isIncompatible: true,
			}
		}
		return peerInfo, nil
	})
}
