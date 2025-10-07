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

// TransportOptions sets options for Transport.
type TransportOptions struct {
	// MaxAcceptedConnections is the maximum number of simultaneous accepted
	// (incoming) connections. Beyond this, new connections will block until
	// a slot is free. 0 means unlimited.
	//
	// FIXME: We may want to replace this with connection accounting in the
	// Router, since it will need to do e.g. rate limiting and such as well.
	// But it might also make sense to have per-transport limits.
	MaxAcceptedConnections uint32
}

// TestTransport creates a new Transport for tests.
func TestTransport(logger log.Logger, nodeID types.NodeID, descs ...*ChannelDescriptor) *Transport {
	return NewTransport(
		logger.With("local", nodeID),
		Endpoint{tcp.TestReserveAddr()},
		conn.DefaultMConnConfig(),
		descs,
		TransportOptions{},
	)
}

// Transport is a Transport implementation using the current multiplexed
// Tendermint protocol ("MConn").
type Transport struct {
	logger       log.Logger
	endpoint     Endpoint
	options      TransportOptions
	mConnConfig  conn.MConnConfig
	channelDescs utils.Mutex[*[]*ChannelDescriptor]
	started      chan struct{}
	listener     chan *mConnConnection
}

// NewTransport sets up a new MConnection transport. This uses the
// proprietary Tendermint MConnection protocol, which is implemented as
// conn.MConnection.
func NewTransport(
	logger log.Logger,
	endpoint Endpoint,
	mConnConfig conn.MConnConfig,
	channelDescs []*ChannelDescriptor,
	options TransportOptions,
) *Transport {
	return &Transport{
		logger:       logger,
		endpoint:     endpoint,
		options:      options,
		mConnConfig:  mConnConfig,
		channelDescs: utils.NewMutex(&channelDescs),
		// This is rendezvous channel, so that no unclosed connections get stuck inside
		// when transport is closing.
		started:  make(chan struct{}),
		listener: make(chan *mConnConnection),
	}
}

// WaitForStart waits until transport starts listening for incoming connections.
func (m *Transport) WaitForStart(ctx context.Context) error {
	_, _, err := utils.RecvOrClosed(ctx, m.started)
	return err
}

func (m *Transport) Endpoint() Endpoint {
	return m.endpoint
}

func (m *Transport) Run(ctx context.Context) error {
	if err := m.endpoint.Validate(); err != nil {
		return err
	}
	listener, err := tcp.Listen(m.endpoint.AddrPort)
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
			descs := m.getChannelDescs()
			mconn := newMConnConnection(m.logger, conn, m.mConnConfig, descs)
			if err := utils.Send(ctx, m.listener, mconn); err != nil {
				mconn.Close()
				return err
			}
		}
	})
}

// String implements Transport.
func (m *Transport) String() string {
	return "transport"
}

// Accept implements Transport.
func (m *Transport) Accept(ctx context.Context) (Connection, error) {
	return utils.Recv(ctx, m.listener)
}

// Dial implements Transport.
func (m *Transport) Dial(ctx context.Context, endpoint Endpoint) (Connection, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	if endpoint.Port() == 0 {
		endpoint.AddrPort = netip.AddrPortFrom(endpoint.Addr(), 26657)
	}
	dialer := net.Dialer{}
	tcpConn, err := dialer.DialContext(ctx, "tcp", endpoint.String())
	if err != nil {
		return nil, fmt.Errorf("dialer.DialContext(%v): %w", endpoint, err)
	}
	descs := m.getChannelDescs()
	return newMConnConnection(m.logger, tcpConn, m.mConnConfig, descs), nil
}

// SetChannels sets the channel descriptors to be used when
// establishing a connection.
//
// FIXME: To be removed when the legacy p2p stack is removed. Channel
// descriptors should be managed by the router. The underlying transport and
// connections should be agnostic to everything but the channel ID's which are
// initialized in the handshake.
func (m *Transport) AddChannelDescriptors(channelDesc []*ChannelDescriptor) {
	for descs := range m.channelDescs.Lock() {
		*descs = append(*descs, channelDesc...)
	}
}

func (m *Transport) getChannelDescs() []*ChannelDescriptor {
	var descs []*ChannelDescriptor
	for d := range m.channelDescs.Lock() {
		descs = make([]*ChannelDescriptor, len(*d))
		copy(descs, *d)
	}
	return descs
}

type InvalidEndpointErr struct{ error }

// mConnConnection implements Connection for Transport.
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
	return Endpoint{c.conn.LocalAddr().(*net.TCPAddr).AddrPort()}
}

// RemoteEndpoint implements Connection.
func (c *mConnConnection) RemoteEndpoint() Endpoint {
	return Endpoint{c.conn.RemoteAddr().(*net.TCPAddr).AddrPort()}
}

// Close implements Connection.
func (c *mConnConnection) Close() error {
	if c.mconn == nil {
		return c.conn.Close()
	}
	return c.mconn.Close()
}

// Connection represents an established connection between two endpoints.
//
// FIXME: This is a temporary interface for backwards-compatibility with the
// current MConnection-protocol, which is message-oriented. It should be
// migrated to a byte-oriented multi-stream interface instead, which would allow
// e.g. adopting QUIC and making message framing, traffic scheduling, and node
// handshakes a Router concern shared across all transports. However, this
// requires MConnection protocol changes or a shim. For details, see:
// https://github.com/tendermint/spec/pull/227
//
// FIXME: The interface is currently very broad in order to accommodate
// MConnection behavior that the legacy P2P stack relies on. It should be
// cleaned up when the legacy stack is removed.
type Connection interface {
	// Handshake executes a node handshake with the remote peer. It must be
	// called immediately after the connection is established, and returns the
	// remote peer's node info and public key. The caller is responsible for
	// validation.
	//
	// FIXME: The handshake should really be the Router's responsibility, but
	// that requires the connection interface to be byte-oriented rather than
	// message-oriented (see comment above).
	Handshake(context.Context, types.NodeInfo, crypto.PrivKey) (types.NodeInfo, error)

	// ReceiveMessage returns the next message received on the connection,
	// blocking until one is available. Returns io.EOF if closed.
	ReceiveMessage(context.Context) (ChannelID, []byte, error)

	// SendMessage sends a message on the connection. Returns io.EOF if closed.
	SendMessage(context.Context, ChannelID, []byte) error

	// LocalEndpoint returns the local endpoint for the connection.
	LocalEndpoint() Endpoint

	// RemoteEndpoint returns the remote endpoint for the connection.
	RemoteEndpoint() Endpoint

	// Close closes the connection.
	Close() error

	// Stringer is used to display the connection, e.g. in logs.
	//
	// Without this, the logger may use reflection to access and display
	// internal fields. These can be written to concurrently, which can trigger
	// the race detector or even cause a panic.
	fmt.Stringer
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
