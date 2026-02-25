package p2p

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const queueBufferDefault = 1024

type InvalidEndpointErr struct{ error }

func toChannelIDs(bytes []byte) ChannelIDSet {
	c := make(map[ChannelID]struct{}, len(bytes))
	for _, b := range bytes {
		c[ChannelID(b)] = struct{}{}
	}
	return c
}

type ChannelIDSet map[ChannelID]struct{}

func (cs ChannelIDSet) Contains(id ChannelID) bool {
	_, ok := cs[id]
	return ok
}

// Connection implements Connection for Transport.
type ConnV2 struct {
	// Address at which this connection was dialed (None for inbound connections).
	dialAddr     utils.Option[NodeAddress]
	// Address under which this node can be dialed (declared by peer during handshake).
	selfAddr     utils.Option[NodeAddress]
	peerChannels ChannelIDSet
	peerInfo     types.NodeInfo
	sendQueue    *Queue[sendMsg]
	mconn        *conn.MConnection
}

func (c *ConnV2) Info() peerConnInfo {
	return peerConnInfo{
		ID:       c.peerInfo.NodeID,
		Channels: c.peerChannels,
		DialAddr: c.dialAddr,
		SelfAddr: c.selfAddr,
	}
}

func (r *Router) connSendRoutine(ctx context.Context, conn *ConnV2) error {
	for {
		start := time.Now().UTC()
		m, err := conn.sendQueue.Recv(ctx)
		if err != nil {
			return err
		}
		r.metrics.RouterPeerQueueRecv.Observe(time.Since(start).Seconds())
		bz, err := gogoproto.Marshal(m.Message)
		if err != nil {
			panic(fmt.Sprintf("proto.Marshal(): %v", err))
		}
		if m.ChannelID > math.MaxUint8 {
			return fmt.Errorf("MConnection only supports 1-byte channel IDs (got %v)", m.ChannelID)
		}
		if err := conn.mconn.Send(ctx, m.ChannelID, bz); err != nil {
			return err
		}
		r.logger.Debug("sent message", "peer", conn.peerInfo.NodeID, "message", m.Message)
	}
}

// receivePeer receives inbound messages from a peer, deserializes them and
// passes them on to the appropriate channel.
func (r *Router) connRecvRoutine(ctx context.Context, conn *ConnV2) error {
	for {
		chID, bz, err := conn.mconn.Recv(ctx)
		if err != nil {
			return err
		}
		for chs := range r.channels.RLock() {
			ch, ok := chs[chID]
			if !ok {
				// TODO(gprusak): verify if this is a misbehavior, and drop the peer if it is.
				r.logger.Debug("dropping message for unknown channel", "peer", conn.peerInfo.NodeID, "channel", chID)
				continue
			}

			msg := gogoproto.Clone(ch.desc.MessageType)
			if err := gogoproto.Unmarshal(bz, msg); err != nil {
				return fmt.Errorf("message decoding failed, dropping message: [peer=%v] %w", conn.peerInfo.NodeID, err)
			}
			// Priority is not used since all messages in this queue are from the same channel.
			if _, ok := ch.recvQueue.Send(RecvMsg[gogoproto.Message]{From: conn.peerInfo.NodeID, Message: msg}, gogoproto.Size(msg), 0).Get(); ok {
				r.metrics.QueueDroppedMsgs.With("ch_id", fmt.Sprint(chID), "direction", "in").Add(float64(1))
			}
			r.metrics.PeerReceiveBytesTotal.With(
				"chID", fmt.Sprint(chID),
				"peer_id", string(conn.peerInfo.NodeID),
				"message_type", r.lc.ValueToMetricLabel(msg)).Add(float64(gogoproto.Size(msg)))
			r.logger.Debug("received message", "peer", conn.peerInfo.NodeID, "message", msg)
		}
	}
}

func (r *Router) runConn(ctx context.Context, hConn *handshakedConn, peerInfo types.NodeInfo, dialAddr utils.Option[NodeAddress]) error {
	conn := &ConnV2{
		dialAddr:     dialAddr,
		selfAddr:     hConn.msg.SelfAddr,
		peerInfo:     peerInfo,
		sendQueue:    NewQueue[sendMsg](queueBufferDefault),
		peerChannels: toChannelIDs(peerInfo.Channels),
		mconn: conn.NewMConnection(
			r.logger.With("peer", Endpoint{hConn.conn.RemoteAddr()}.NodeAddress(peerInfo.NodeID)),
			hConn.conn,
			r.getChannelDescs(),
			r.options.Connection,
		),
	}
	if err := r.peerManager.Connected(conn); err != nil {
		return fmt.Errorf("r.peerManager.Connected(): %w", err)
	}
	defer r.peerManager.Disconnected(conn)
	r.logger.Info("peer connected", "peer", conn.PeerInfo().NodeID, "endpoint", conn)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("mconn.Run", func() error { return conn.mconn.Run(ctx) })
		s.SpawnNamed("connSendRoutine", func() error { return r.connSendRoutine(ctx, conn) })
		s.SpawnNamed("connRecvRoutine", func() error { return r.connRecvRoutine(ctx, conn) })
		return nil
	})
}

func (c *ConnV2) String() string           { return c.RemoteEndpoint().String() }
func (c *ConnV2) PeerInfo() types.NodeInfo { return c.peerInfo }
func (c *ConnV2) LocalEndpoint() Endpoint  { return Endpoint{c.mconn.LocalAddr()} }
func (c *ConnV2) RemoteEndpoint() Endpoint { return Endpoint{c.mconn.RemoteAddr()} }
func (c *ConnV2) Close()                   { c.mconn.Close() }

// Endpoint represents a transport connection endpoint, either local or remote.
// It is a TCP endpoint address.
type Endpoint struct{ netip.AddrPort }

// NewEndpoint constructs an Endpoint from a types.NetAddress structure.
func ResolveEndpoint(addr string) (Endpoint, error) {
	e, err := types.ResolveAddressString(addr)
	return Endpoint{e}, err
}

// NodeAddress converts the endpoint into a NodeAddress for the given node ID.
func (e Endpoint) NodeAddress(nodeID types.NodeID) NodeAddress {
	return NodeAddress{
		NodeID:   nodeID,
		Hostname: e.Addr().String(),
		Port:     e.Port(),
	}
}

// Validate validates the endpoint.
func (e Endpoint) Validate() error {
	if !e.IsValid() {
		return InvalidEndpointErr{fmt.Errorf("endpoint has invalid address %q", e.String())}
	}
	return nil
}
