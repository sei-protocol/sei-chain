package p2p

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/netip"
	"sync/atomic"


	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils/scope"
	p2pproto "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
)

type InvalidEndpointErr struct{ error }

// Connection implements Connection for Transport.
type Connection struct {
	conn net.Conn
	peerInfo types.NodeInfo
	mconn *conn.MConnection
}

// Handshake implements Connection.
func Handshake(
	ctx context.Context,
	logger log.Logger,
	nodeInfo types.NodeInfo,
	privKey crypto.PrivKey,
	tcpConn net.Conn,
	mConnConfig conn.MConnConfig,
	channelDescs []*ChannelDescriptor,
) (c *Connection, err error) {
	defer func() {
		// Late error check. Close conn to avoid leaking it.
		if err != nil {
			tcpConn.Close()
		}
	}()
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
		secretConn, err := conn.MakeSecretConnection(tcpConn, privKey)
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
			peerInfo: peerInfo,
			mconn:    conn.NewMConnection(
				logger.With("peer", remoteEndpoint(tcpConn).NodeAddress(peerInfo.NodeID)),
				secretConn,
				channelDescs,
				mConnConfig,
			),
		}, nil
	})
}

func (c *Connection) Run(ctx context.Context) error {
	return c.mconn.Run(ctx)
}

// String displays connection information.
func (c *Connection) String() string {
	return c.RemoteEndpoint().String()
}

// SendMessage implements Connection.
func (c *Connection) SendMessage(ctx context.Context, chID ChannelID, msg []byte) error {
	if chID > math.MaxUint8 {
		return fmt.Errorf("MConnection only supports 1-byte channel IDs (got %v)", chID)
	}
	return c.mconn.Send(ctx, chID, msg)
}

// ReceiveMessage implements Connection.
func (c *Connection) ReceiveMessage(ctx context.Context) (ChannelID, []byte, error) {
	return c.mconn.Recv(ctx)
}

// LocalEndpoint implements Connection.
func (c *Connection) LocalEndpoint() Endpoint {
	return Endpoint{c.conn.LocalAddr().(*net.TCPAddr).AddrPort()}
}

func (c *Connection) PeerInfo() types.NodeInfo {
	return c.peerInfo
}

// RemoteEndpoint implements Connection.
func remoteEndpoint(conn net.Conn) Endpoint {
	return Endpoint{conn.RemoteAddr().(*net.TCPAddr).AddrPort()}
}

// RemoteEndpoint implements Connection.
func (c *Connection) RemoteEndpoint() Endpoint {
	return remoteEndpoint(c.conn)
}

// Close implements Connection.
func (c *Connection) Close() error {
	return c.conn.Close()
}

// Endpoint represents a transport connection endpoint, either local or remote.
// It is a TCP endpoint address.
type Endpoint struct{ netip.AddrPort }

// NewEndpoint constructs an Endpoint from a types.NetAddress structure.
func NewEndpoint(addr string) (Endpoint, error) {
	e, err := types.ParseAddressString(addr)
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
