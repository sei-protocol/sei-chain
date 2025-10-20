package p2p

import (
	"context"
	"fmt"
	"time"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/gogo/protobuf/proto"
	"math"
	"net"
	"net/netip"
	"sync/atomic"

	p2pproto "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
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
type Connection struct {
	conn *net.TCPConn
	peerChannels ChannelIDSet
	peerInfo types.NodeInfo
	sendQueue *Queue[sendMsg]
	mconn *conn.MConnection
}

// Handshake implements Connection.
func HandshakeOrClose(ctx context.Context, r *Router, tcpConn *net.TCPConn) (c *Connection, err error) {
	defer func() {
		// Late error check. Close conn to avoid leaking it.
		if err != nil {
			tcpConn.Close()
		}
	}()
	nodeInfo := r.nodeInfoProducer()
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*Connection, error) {
		var ok atomic.Bool
		s.SpawnBg(func() error {
			// Early error check. Close conn to terminate tasks which do not respect ctx.
			<-ctx.Done()
			if !ok.Load() {
				tcpConn.Close()
			}
			return nil
		})
		var err error
		secretConn, err := conn.MakeSecretConnection(tcpConn, r.privKey)
		if err != nil {
			return nil, err
		}
		s.Spawn(func() error {
			_, err := protoio.NewDelimitedWriter(secretConn).WriteMsg(nodeInfo.ToProto())
			return err
		})
		var pbPeerInfo p2pproto.NodeInfo
		if _, err := protoio.NewDelimitedReader(secretConn, types.MaxNodeInfoSize()).ReadMsg(&pbPeerInfo); err != nil {
			return nil, err
		}
		peerInfo, err := types.NodeInfoFromProto(&pbPeerInfo)
		if err != nil {
			return nil, fmt.Errorf("error reading NodeInfo: %w", err)
		}
		// Authenticate the peer first.
		peerID := types.NodeIDFromPubKey(secretConn.RemotePubKey())
		if peerID != peerInfo.NodeID {
			return nil, fmt.Errorf("peer's public key did not match its node ID %q (expected %q)",
				peerInfo.NodeID, peerID)
		}
		if err := peerInfo.Validate(); err != nil {
			return nil, fmt.Errorf("invalid handshake NodeInfo: %w", err)
		}
		ok.Store(true)
		return &Connection{
			conn:     tcpConn,
			sendQueue: NewQueue[sendMsg](queueBufferDefault),
			peerInfo: peerInfo,
			peerChannels: toChannelIDs(peerInfo.Channels),
			mconn: conn.NewMConnection(
				r.logger.With("peer", remoteEndpoint(tcpConn).NodeAddress(peerInfo.NodeID)),
				secretConn,
				r.getChannelDescs(),
				r.options.Connection,
			),
		}, nil
	})
}

func (c *Connection) sendRoutine(ctx context.Context, r *Router) error {
	for {
		start := time.Now().UTC()
		m, err := c.sendQueue.Recv(ctx)
		if err != nil {
			return err
		}
		r.metrics.RouterPeerQueueRecv.Observe(time.Since(start).Seconds())
		bz, err := proto.Marshal(m.Message)
		if err != nil {
			panic(fmt.Sprintf("proto.Marshal(): %v", err))
		}
		if m.ChannelID > math.MaxUint8 {
			return fmt.Errorf("MConnection only supports 1-byte channel IDs (got %v)", m.ChannelID)
		}
		if err := c.mconn.Send(ctx, m.ChannelID, bz); err != nil {
			return err
		}
		r.logger.Debug("sent message", "peer", c.peerInfo.NodeID, "message", m.Message)
	}
}

// receivePeer receives inbound messages from a peer, deserializes them and
// passes them on to the appropriate channel.
func (c *Connection) recvRoutine(ctx context.Context, r *Router) error {
	for {
		chID, bz, err := c.mconn.Recv(ctx)
		if err != nil {
			return err
		}
		for chs := range r.channels.RLock() {
			ch, ok := chs[chID]
			if !ok {
				// TODO(gprusak): verify if this is a misbehavior, and drop the peer if it is.
				r.logger.Debug("dropping message for unknown channel", "peer", c.peerInfo.NodeID, "channel", chID)
				continue
			}

			msg := proto.Clone(ch.desc.MessageType)
			if err := proto.Unmarshal(bz, msg); err != nil {
				return fmt.Errorf("message decoding failed, dropping message: [peer=%v] %w", c.peerInfo.NodeID, err)
			}
			if wrapper, ok := msg.(Wrapper); ok {
				var err error
				if msg, err = wrapper.Unwrap(); err != nil {
					return fmt.Errorf("failed to unwrap message: %w", err)
				}
			}
			// Priority is not used since all messages in this queue are from the same channel.
			if _, ok := ch.recvQueue.Send(RecvMsg{From: c.peerInfo.NodeID, Message: msg}, proto.Size(msg), 0).Get(); ok {
				r.metrics.QueueDroppedMsgs.With("ch_id", fmt.Sprint(chID), "direction", "in").Add(float64(1))
			}
			r.metrics.PeerReceiveBytesTotal.With(
				"chID", fmt.Sprint(chID),
				"peer_id", string(c.peerInfo.NodeID),
				"message_type", r.lc.ValueToMetricLabel(msg)).Add(float64(proto.Size(msg)))
			r.logger.Debug("received message", "peer", c.peerInfo.NodeID, "message", msg)
		}
	}
}

func (c *Connection) Run(ctx context.Context, r *Router) error {
	peerInfo := c.PeerInfo()
	peerID := peerInfo.NodeID
	r.metrics.Peers.Add(1)
	defer r.metrics.Peers.Add(-1)
	for conns := range r.conns.Lock() {
		if old, ok := conns[peerID]; ok {
			old.Close()
		}
		conns[peerID] = c
	}
	r.peerManager.Ready(ctx, peerID, c.peerChannels)
	r.logger.Info("peer connected", "peer", peerID, "endpoint", c)
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return c.mconn.Run(ctx) })
		s.Spawn(func() error { return c.sendRoutine(ctx, r) })
		s.Spawn(func() error { return c.recvRoutine(ctx, r) })
		return nil
	})
	for conns := range r.conns.Lock() {
		if conns[peerID] == c {
			delete(conns, peerID)
		}
	}
	// TODO(gprusak): investigate if peerManager handles overlapping connetions correctly
	r.peerManager.Disconnected(ctx, peerID)
	return err
}

// String displays connection information.
func (c *Connection) String() string {
	return c.RemoteEndpoint().String()
}

// LocalEndpoint implements Connection.
func (c *Connection) LocalEndpoint() Endpoint {
	return Endpoint{c.conn.LocalAddr().(*net.TCPAddr).AddrPort()}
}

func (c *Connection) PeerInfo() types.NodeInfo {
	return c.peerInfo
}

// RemoteEndpoint implements Connection.
func remoteEndpoint(conn *net.TCPConn) Endpoint {
	return Endpoint{conn.RemoteAddr().(*net.TCPAddr).AddrPort()}
}

// RemoteEndpoint.
func (c *Connection) RemoteEndpoint() Endpoint {
	return remoteEndpoint(c.conn)
}

// Close.
func (c *Connection) Close() {
	c.conn.Close()
}

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
