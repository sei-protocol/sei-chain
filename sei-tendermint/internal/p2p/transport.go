package p2p

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/types"
)

//go:generate ../../scripts/mockery_generate.sh Transport|Connection

const (
	// defaultProtocol is the default protocol used for NodeAddress when
	// a protocol isn't explicitly given as a URL scheme.
	defaultProtocol Protocol = MConnProtocol
)

// Protocol identifies a transport protocol.
type Protocol string

// Transport is a connection-oriented mechanism for exchanging data with a peer.
type Transport interface {
	// Run executes the background tasks of transport.
	Run(ctx context.Context) error
	// Protocols returns the protocols supported by the transport. The Router
	// uses this to pick a transport for an Endpoint.
	Protocols() []Protocol

	// Endpoints returns the local endpoints the transport is listening on.
	Endpoint() Endpoint

	// Accept waits for the next inbound connection on a listening endpoint, blocking
	// until either a connection is available or the transport is closed. On closure,
	// io.EOF is returned and further Accept calls are futile.
	Accept(context.Context) (Connection, error)

	// Dial creates an outbound connection to an endpoint.
	Dial(context.Context, Endpoint) (Connection, error)

	// AddChannelDescriptors is only part of this interface
	// temporarily
	AddChannelDescriptors([]*ChannelDescriptor)

	// Stringer is used to display the transport, e.g. in logs.
	//
	// Without this, the logger may use reflection to access and display
	// internal fields. These can be written to concurrently, which can trigger
	// the race detector or even cause a panic.
	fmt.Stringer
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
//
// Endpoints are not necessarily networked (see e.g. MemoryTransport) but all
// networked endpoints must use IP as the underlying transport protocol to allow
// e.g. IP address filtering. Either IP or Path (or both) must be set.
type Endpoint struct {
	// Protocol specifies the transport protocol.
	Protocol Protocol
	// TCP endpoint address.
	Addr netip.AddrPort

	// Path is an optional transport-specific path or identifier.
	Path string
}

// NewEndpoint constructs an Endpoint from a types.NetAddress structure.
func NewEndpoint(addr string) (Endpoint, error) {
	addrPort, err := types.ParseAddressString(addr)
	if err != nil {
		return Endpoint{}, err
	}

	return Endpoint{
		Protocol: MConnProtocol,
		Addr:     addrPort,
	}, nil
}

// NodeAddress converts the endpoint into a NodeAddress for the given node ID.
func (e Endpoint) NodeAddress(nodeID types.NodeID) NodeAddress {
	address := NodeAddress{
		NodeID:   nodeID,
		Protocol: e.Protocol,
		Path:     e.Path,
	}
	if e.Addr != (netip.AddrPort{}) {
		address.Hostname = e.Addr.Addr().String()
		address.Port = e.Addr.Port()
	}
	return address
}

// String formats the endpoint as a URL string.
func (e Endpoint) String() string {
	// If this is a non-networked endpoint with a valid node ID as a path,
	// assume that path is a node ID (to handle opaque URLs of the form
	// scheme:id).
	if e.Addr == (netip.AddrPort{}) {
		if nodeID, err := types.NewNodeID(e.Path); err == nil {
			return e.NodeAddress(nodeID).String()
		}
	}
	return e.NodeAddress("").String()
}

// Validate validates the endpoint.
func (e Endpoint) Validate() error {
	if e.Protocol == "" {
		return errors.New("endpoint has no protocol")
	}
	if (e.Addr == netip.AddrPort{}) && (e.Path == "") {
		return errors.New("endpoint has neither path nor IP")
	}
	if e.Addr != (netip.AddrPort{}) {
		if !e.Addr.IsValid() {
			return fmt.Errorf("endpoint has invalid address %q", e.Addr.String())
		}
	}
	return nil
}
