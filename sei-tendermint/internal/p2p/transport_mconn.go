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

	"golang.org/x/net/netutil"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	p2pproto "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
)

const (
	MConnProtocol Protocol = "mconn"
	TCPProtocol   Protocol = "tcp"
)

// MConnTransportOptions sets options for MConnTransport.
type MConnTransportOptions struct {
	// MaxAcceptedConnections is the maximum number of simultaneous accepted
	// (incoming) connections. Beyond this, new connections will block until
	// a slot is free. 0 means unlimited.
	//
	// FIXME: We may want to replace this with connection accounting in the
	// Router, since it will need to do e.g. rate limiting and such as well.
	// But it might also make sense to have per-transport limits.
	MaxAcceptedConnections uint32
}

// MConnTransport is a Transport implementation using the current multiplexed
// Tendermint protocol ("MConn").
type MConnTransport struct {
	logger       log.Logger
	endpoint     Endpoint
	options      MConnTransportOptions
	mConnConfig  conn.MConnConfig
	channelDescs []*ChannelDescriptor
	started      chan struct{}
	listener     chan *mConnConnection
}

// NewMConnTransport sets up a new MConnection transport. This uses the
// proprietary Tendermint MConnection protocol, which is implemented as
// conn.MConnection.
func NewMConnTransport(
	logger log.Logger,
	endpoint Endpoint,
	mConnConfig conn.MConnConfig,
	channelDescs []*ChannelDescriptor,
	options MConnTransportOptions,
) *MConnTransport {
	return &MConnTransport{
		logger:       logger,
		endpoint:     endpoint,
		options:      options,
		mConnConfig:  mConnConfig,
		channelDescs: channelDescs,
		// This is rendezvous channel, so that no unclosed connections get stuck inside
		// when transport is closing.
		started:  make(chan struct{}),
		listener: make(chan *mConnConnection),
	}
}

// WaitForStart waits until transport starts listening for incoming connections.
func (m *MConnTransport) WaitForStart(ctx context.Context) error {
	_, _, err := utils.RecvOrClosed(ctx, m.started)
	return err
}

func (m *MConnTransport) Endpoint() Endpoint {
	return m.endpoint
}

func (m *MConnTransport) Run(ctx context.Context) error {
	if err := m.validateEndpoint(m.endpoint); err != nil {
		return err
	}
	listener, err := tcp.Listen(m.endpoint.Addr)
	if err != nil {
		return fmt.Errorf("net.Listen(): %w", err)
	}
	close(m.started) // signal that we are listening
	if m.options.MaxAcceptedConnections > 0 {
		// FIXME: This will establish the inbound connection but simply hang it
		// until another connection is released. It would probably be better to
		// return an error to the remote peer or close the connection. This is
		// also a DoS vector since the connection will take up kernel resources.
		// This was just carried over from the legacy P2P stack.
		listener = netutil.LimitListener(listener, int(m.options.MaxAcceptedConnections))
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			<-ctx.Done()
			listener.Close()
			return nil
		})
		for {
			conn, err := listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
				return err
			}
			mconn := newMConnConnection(m.logger, conn, m.mConnConfig, m.channelDescs)
			if err := utils.Send(ctx, m.listener, mconn); err != nil {
				mconn.Close()
				return err
			}
		}
	})
}

// String implements Transport.
func (m *MConnTransport) String() string {
	return string(MConnProtocol)
}

// Protocols implements Transport. We support tcp for backwards-compatibility.
func (m *MConnTransport) Protocols() []Protocol {
	return []Protocol{MConnProtocol, TCPProtocol}
}

// Accept implements Transport.
func (m *MConnTransport) Accept(ctx context.Context) (Connection, error) {
	return utils.Recv(ctx, m.listener)
}

// Dial implements Transport.
func (m *MConnTransport) Dial(ctx context.Context, endpoint Endpoint) (Connection, error) {
	if err := m.validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	if endpoint.Addr.Port() == 0 {
		endpoint.Addr = netip.AddrPortFrom(endpoint.Addr.Addr(), 26657)
	}
	dialer := net.Dialer{}
	tcpConn, err := dialer.DialContext(ctx, "tcp", endpoint.Addr.String())
	if err != nil {
		return nil, fmt.Errorf("dialer.DialContext(%v): %w", endpoint.Addr, err)
	}
	return newMConnConnection(m.logger, tcpConn, m.mConnConfig, m.channelDescs), nil
}

// SetChannels sets the channel descriptors to be used when
// establishing a connection.
//
// FIXME: To be removed when the legacy p2p stack is removed. Channel
// descriptors should be managed by the router. The underlying transport and
// connections should be agnostic to everything but the channel ID's which are
// initialized in the handshake.
func (m *MConnTransport) AddChannelDescriptors(channelDesc []*ChannelDescriptor) {
	m.channelDescs = append(m.channelDescs, channelDesc...)
}

type InvalidEndpointErr struct{ error }

// validateEndpoint validates an endpoint.
func (m *MConnTransport) validateEndpoint(endpoint Endpoint) error {
	if err := endpoint.Validate(); err != nil {
		return InvalidEndpointErr{err}
	}
	if endpoint.Protocol != MConnProtocol && endpoint.Protocol != TCPProtocol {
		return InvalidEndpointErr{fmt.Errorf("unsupported protocol %q", endpoint.Protocol)}
	}
	if !endpoint.Addr.IsValid() {
		return InvalidEndpointErr{errors.New("endpoint has invalid address")}
	}
	if endpoint.Path != "" {
		return InvalidEndpointErr{fmt.Errorf("endpoints with path not supported (got %q)", endpoint.Path)}
	}
	return nil
}

// mConnConnection implements Connection for MConnTransport.
type mConnConnection struct {
	logger       log.Logger
	conn         net.Conn
	mConnConfig  conn.MConnConfig
	channelDescs []*ChannelDescriptor
	errorCh      chan error
	doneCh       chan struct{}
	closeOnce    sync.Once

	mconn *conn.MConnection // set during Handshake()
}

// newMConnConnection creates a new mConnConnection.
func newMConnConnection(
	logger log.Logger,
	conn net.Conn,
	mConnConfig conn.MConnConfig,
	channelDescs []*ChannelDescriptor,
) *mConnConnection {
	return &mConnConnection{
		logger:       logger,
		conn:         conn,
		mConnConfig:  mConnConfig,
		channelDescs: channelDescs,
		errorCh:      make(chan error, 1), // buffered to avoid onError leak
		doneCh:       make(chan struct{}),
	}
}

// Handshake implements Connection.
func (c *mConnConnection) Handshake(
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
func (c *mConnConnection) String() string {
	return c.RemoteEndpoint().String()
}

// SendMessage implements Connection.
func (c *mConnConnection) SendMessage(ctx context.Context, chID ChannelID, msg []byte) error {
	if chID > math.MaxUint8 {
		return fmt.Errorf("MConnection only supports 1-byte channel IDs (got %v)", chID)
	}
	return c.mconn.Send(ctx, chID, msg)
}

// ReceiveMessage implements Connection.
func (c *mConnConnection) ReceiveMessage(ctx context.Context) (ChannelID, []byte, error) {
	return c.mconn.Recv(ctx)
}

// LocalEndpoint implements Connection.
func (c *mConnConnection) LocalEndpoint() Endpoint {
	endpoint := Endpoint{
		Protocol: MConnProtocol,
	}
	if addr, ok := c.conn.LocalAddr().(*net.TCPAddr); ok {
		endpoint.Addr = addr.AddrPort()
	}
	return endpoint
}

// RemoteEndpoint implements Connection.
func (c *mConnConnection) RemoteEndpoint() Endpoint {
	endpoint := Endpoint{
		Protocol: MConnProtocol,
	}
	if addr, ok := c.conn.RemoteAddr().(*net.TCPAddr); ok {
		endpoint.Addr = addr.AddrPort()
	}
	return endpoint
}

// Close implements Connection.
func (c *mConnConnection) Close() error {
	if c.mconn == nil {
		return c.conn.Close()
	}
	return c.mconn.Close()
}
