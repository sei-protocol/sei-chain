package p2p

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"sync"
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
	logger       log.Logger
	conn         net.Conn
	mConnConfig  conn.MConnConfig
	channelDescs []*ChannelDescriptor
	errorCh      chan error
	doneCh       chan struct{}
	closeOnce    sync.Once

	mconn *conn.MConnection // set during Handshake()
}

// newMConnConnection creates a new Connection.
func newConnection(
	logger log.Logger,
	conn net.Conn,
	mConnConfig conn.MConnConfig,
	channelDescs []*ChannelDescriptor,
) *Connection {
	return &Connection{
		logger:       logger,
		conn:         conn,
		mConnConfig:  mConnConfig,
		channelDescs: channelDescs,
		errorCh:      make(chan error, 1), // buffered to avoid onError leak
		doneCh:       make(chan struct{}),
	}
}

// Handshake implements Connection.
func (c *Connection) Handshake(
	ctx context.Context,
	nodeInfo types.NodeInfo,
	privKey crypto.PrivKey,
) (types.NodeInfo, error) {
	if c.mconn != nil {
		return types.NodeInfo{}, errors.New("connection is already handshaked")
	}
	var peerInfo types.NodeInfo
	var secretConn *conn.SecretConnection
	var ok atomic.Bool
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			<-ctx.Done()
			// Close the connection if handshake did not complete.
			if !ok.Load() {
				c.conn.Close()
			}
			return nil
		})
		var err error
		secretConn, err = conn.MakeSecretConnection(c.conn, privKey)
		if err != nil {
			return err
		}
		s.Spawn(func() error {
			_, err := protoio.NewDelimitedWriter(secretConn).WriteMsg(nodeInfo.ToProto())
			return err
		})
		var pbPeerInfo p2pproto.NodeInfo
		if _, err := protoio.NewDelimitedReader(secretConn, types.MaxNodeInfoSize()).ReadMsg(&pbPeerInfo); err != nil {
			return err
		}
		peerInfo, err = types.NodeInfoFromProto(&pbPeerInfo)
		if err != nil {
			return fmt.Errorf("error reading NodeInfo: %w", err)
		}
		// Authenticate the peer first.
		peerID := types.NodeIDFromPubKey(secretConn.RemotePubKey())
		if peerID != peerInfo.NodeID {
			return fmt.Errorf("peer's public key did not match its node ID %q (expected %q)",
				peerInfo.NodeID, peerID)
		}
		if err := peerInfo.Validate(); err != nil {
			return fmt.Errorf("invalid handshake NodeInfo: %w", err)
		}
		ok.Store(true)
		return nil
	})
	if err != nil {
		return types.NodeInfo{}, err
	}
	// mconn takes ownership of conn.
	c.mconn = conn.SpawnMConnection(
		c.logger.With("peer", c.RemoteEndpoint().NodeAddress(peerInfo.NodeID)),
		secretConn,
		c.channelDescs,
		c.mConnConfig,
	)
	return peerInfo, nil
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

// RemoteEndpoint implements Connection.
func (c *Connection) RemoteEndpoint() Endpoint {
	return Endpoint{c.conn.RemoteAddr().(*net.TCPAddr).AddrPort()}
}

// Close implements Connection.
func (c *Connection) Close() error {
	if c.mconn == nil {
		return c.conn.Close()
	}
	return c.mconn.Close()
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
