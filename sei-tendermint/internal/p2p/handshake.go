package p2p

import (
	"context"
	"fmt"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"

	gogopb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type handshakedConn struct {
	conn *conn.SecretConnection
	msg  *handshakeMsg
}

func handshake(ctx context.Context, c conn.Conn, key NodeSecretKey, selfAddr utils.Option[NodeAddress], seiGigaConn bool) (*handshakedConn, error) {
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*handshakedConn, error) {
		sc, err := conn.MakeSecretConnection(ctx, c)
		if err != nil {
			return nil, err
		}
		s.Spawn(func() error {
			msg := &handshakeMsg{
				NodeAuth:          key.SignChallenge(sc.Challenge()),
				SelfAddr: selfAddr,
				SeiGigaConnection: seiGigaConn,
			}
			if err := conn.WriteSizedMsg(ctx, sc, handshakeMsgConv.Marshal(msg)); err != nil {
				return fmt.Errorf("conn.WriteSizedMsg(): %w", err)
			}
			if err := sc.Flush(ctx); err != nil {
				return fmt.Errorf("c.Flush(): %w", err)
			}
			return nil
		})
		msgBytes, err := conn.ReadSizedMsg(ctx, sc, 1024*1024)
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
		if selfAddr,ok := msg.SelfAddr.Get(); ok {
			if got,want := selfAddr.NodeID,msg.NodeAuth.Key().NodeID(); got!=want {
				return nil, fmt.Errorf("handshakeMsg.SelfAddr.NodeID = %v, want %v",got,want)
			}
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
