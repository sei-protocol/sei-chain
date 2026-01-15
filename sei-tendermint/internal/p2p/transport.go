package p2p

import (
	"context"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"math"
	"net"
	"net/netip"
	"time"

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
	dialAddr     utils.Option[NodeAddress]
	conn         *net.TCPConn
	peerChannels ChannelIDSet
	peerInfo     types.NodeInfo
	sendQueue    *Queue[sendMsg]
	mconn        *conn.MConnection
}

func (c *Connection) Info() peerConnInfo {
	return peerConnInfo{
		ID:       c.peerInfo.NodeID,
		Channels: c.peerChannels,
		DialAddr: c.dialAddr,
	}
}

type authConn struct {
	peerInfo types.NodeInfo
	dialAddr utils.Option[NodeAddress]
	sc *conn.SecretConnection
}

// handshake handshakes with a peer, validating the peer's information. If
// dialAddr is given, we check that the peer's info matches it.
// Closes the tcpConn if case of any error.
func (r *Router) handshake(ctx context.Context, c tcp.Conn, dialAddr utils.Option[NodeAddress]) (*authConn, error) {	
	if d, ok := r.options.HandshakeTimeout.Get(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	sc, err := conn.MakeSecretConnection(ctx, c)
	if err != nil { return nil,err }
	challenge := sc.Challenge()
	nodeInfo := r.nodeInfoProducer()
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*authConn, error) {
		s.Spawn(func() error {
			authMsg := &authSigMessage{r.privKey.Public(), r.privKey.Sign(challenge[:])}
			if err := conn.WriteSizedProto(ctx,sc,authSigMessageConv.Encode(authMsg)); err != nil { return err }
			if err := conn.WriteSizedProto(ctx,sc,nodeInfo.ToProto()); err != nil { return err }
			return sc.Flush(ctx)
		})
		
		var authProto pb.AuthSigMessage
		if err := conn.ReadSizedProto(ctx,sc,&authProto,1024*1024); err != nil {
			return nil, fmt.Errorf("%w: %v", errAuth, err)
		}
		authMsg, err := authSigMessageConv.Decode(&authProto)
		if err != nil {
			return nil, fmt.Errorf("%w: authSigMessageFromProto(): %v", errAuth, err)
		}
		if err := authMsg.Key.Verify(challenge[:], authMsg.Sig); err != nil {
			return nil, fmt.Errorf("%w: %v", errAuth, err)
		}
		var pbPeerInfo p2pproto.NodeInfo
		if err := conn.ReadSizedProto(ctx,sc,&pbPeerInfo,uint64(types.MaxNodeInfoSize())); err != nil { return nil,err }
		peerInfo, err := types.NodeInfoFromProto(&pbPeerInfo)
		if err != nil { return nil,fmt.Errorf("error reading NodeInfo: %w", err) }
		
		// Authenticate the peer first.
		peerID := types.NodeIDFromPubKey(authMsg.Key)
		if peerID != peerInfo.NodeID {
			return nil,fmt.Errorf("peer's public key did not match its node ID %q (expected %q)", peerInfo.NodeID, peerID)
		}
		// Validate the received info.
		if err := peerInfo.Validate(); err != nil {
			return nil,fmt.Errorf("invalid handshake NodeInfo: %w", err)
		}
		if peerInfo.Network != nodeInfo.Network {
			return nil,errBadNetwork{fmt.Errorf("connected to peer from wrong network, %q, removed from peer store", peerInfo.Network)}
		}
		if want, ok := dialAddr.Get(); ok && want.NodeID != peerInfo.NodeID {
			return nil,fmt.Errorf("expected to connect with peer %q, got %q", want.NodeID, peerInfo.NodeID)
		}
		if !dialAddr.IsPresent() {
			// Filter inbound 
			if err := r.options.filterPeerByID(ctx, peerInfo.ID()); err != nil {
				return nil,fmt.Errorf("peer filtered by ID (%v): %w",peerInfo.ID(),err)
			}
		}
		if err := nodeInfo.CompatibleWith(peerInfo); err != nil {
			return nil,ErrRejected{
				err:            err,
				id:             peerInfo.ID(),
				isIncompatible: true,
			}
		}
		return &authConn{
			dialAddr: dialAddr,
			peerInfo:     peerInfo,
			sc: sc,
		},nil
	})
		/*
			
			sendQueue:    NewQueue[sendMsg](queueBufferDefault),
			peerChannels: toChannelIDs(peerInfo.Channels),
			mconn: conn.NewMConnection(
				//r.logger.With("peer", Endpoint{sc.RemoteAddr()}.NodeAddress(peerInfo.NodeID)),
				sc,
				r.getChannelDescs(),
				r.options.Connection,
			),
		}, nil*/
}

func (r *Router) connSendRoutine(ctx context.Context, conn *Connection) error {
	for {
		start := time.Now().UTC()
		m, err := conn.sendQueue.Recv(ctx)
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
		if err := conn.mconn.Send(ctx, m.ChannelID, bz); err != nil {
			return err
		}
		r.logger.Debug("sent message", "peer", conn.peerInfo.NodeID, "message", m.Message)
	}
}

// receivePeer receives inbound messages from a peer, deserializes them and
// passes them on to the appropriate channel.
func (r *Router) connRecvRoutine(ctx context.Context, conn *Connection) error {
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

			msg := proto.Clone(ch.desc.MessageType)
			if err := proto.Unmarshal(bz, msg); err != nil {
				return fmt.Errorf("message decoding failed, dropping message: [peer=%v] %w", conn.peerInfo.NodeID, err)
			}
			// Priority is not used since all messages in this queue are from the same channel.
			if _, ok := ch.recvQueue.Send(RecvMsg[proto.Message]{From: conn.peerInfo.NodeID, Message: msg}, proto.Size(msg), 0).Get(); ok {
				r.metrics.QueueDroppedMsgs.With("ch_id", fmt.Sprint(chID), "direction", "in").Add(float64(1))
			}
			r.metrics.PeerReceiveBytesTotal.With(
				"chID", fmt.Sprint(chID),
				"peer_id", string(conn.peerInfo.NodeID),
				"message_type", r.lc.ValueToMetricLabel(msg)).Add(float64(proto.Size(msg)))
			r.logger.Debug("received message", "peer", conn.peerInfo.NodeID, "message", msg)
		}
	}
}

func (r *Router) runConn(ctx context.Context, conn *Connection) error {
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

// String displays connection information.
func (c *Connection) String() string {
	return c.RemoteEndpoint().String()
}

func (c *Connection) PeerInfo() types.NodeInfo {
	return c.peerInfo
}

// LocalEndpoint implements Connection.
func (c *Connection) LocalEndpoint() Endpoint {
	return Endpoint{tcp.LocalAddr(c.conn)}
}

// RemoteEndpoint.
func (c *Connection) RemoteEndpoint() Endpoint {
	return Endpoint{tcp.RemoteAddr(c.conn)}
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

type authSigMessage struct {
	Key crypto.PubKey
	Sig crypto.Sig
}

var authSigMessageConv = utils.ProtoConv[*authSigMessage, *pb.AuthSigMessage]{
	Encode: func(m *authSigMessage) *pb.AuthSigMessage {
		return &pb.AuthSigMessage{
			PubKey: crypto.PubKeyConv.Encode(m.Key),
			Sig:    m.Sig.Bytes(),
		}
	},
	Decode: func(p *pb.AuthSigMessage) (*authSigMessage, error) {
		key, err := crypto.PubKeyConv.DecodeReq(p.PubKey)
		if err != nil {
			return nil, fmt.Errorf("PubKey: %w", err)
		}
		sig, err := crypto.SigFromBytes(p.Sig)
		if err != nil {
			return nil, fmt.Errorf("Sig: %w", err)
		}
		return &authSigMessage{key, sig}, nil
	},
}
