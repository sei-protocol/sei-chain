package p2p

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"golang.org/x/time/rate"
)

type DialFailuresError struct {
	Failures uint32
	Address  types.NodeID
}

func (e DialFailuresError) Error() string {
	return fmt.Sprintf("dialing failed %d times will not retry for address=%s, deleting peer", e.Failures, e.Address)
}

// PeerStatus is a peer status.
//
// The peer manager has many more internal states for a peer (e.g. dialing,
// connected, evicting, and so on), which are tracked separately. PeerStatus is
// for external use outside of the peer manager.
type PeerStatus string

const (
	PeerStatusUp   PeerStatus = "up"   // connected and ready
	PeerStatusDown PeerStatus = "down" // disconnected
)

// RouterOptions specifies options for a PeerManager.
type RouterOptions struct {
	// SelfAddress is the address that will be advertised to peers for them to dial back to us.
	SelfAddress utils.Option[NodeAddress]

	// Whether sei giga connections should be established.
	Giga utils.Option[*GigaRouterConfig]

	// Local endpoint to listen for p2p connections on.
	// SelfAddress should point to this endpoint.
	Endpoint Endpoint

	// MaxIncomingConnectionAttempts rate limits the number of incoming connection
	// attempts per IP address. Defaults to 100.
	MaxIncomingConnectionAttempts utils.Option[uint]

	// IncomingConnectionWindow describes how often an IP address
	// can attempt to create a new connection. Defaults to 100ms.
	IncomingConnectionWindow utils.Option[time.Duration]

	// FilterPeerByIP is used by the router to inject filtering
	// behavior for new incoming connections. The router passes
	// the remote IP of the incoming connection the port number as
	// arguments. Functions should return an error to reject the
	// peer.
	FilterPeerByIP utils.Option[func(context.Context, netip.AddrPort) error]

	// FilterPeerByID is used by the router to inject filtering
	// behavior for new incoming connections. The router passes
	// the NodeID of the node before completing the connection,
	// but this occurs after the handshake is complete. Filter by
	// IP address to filter before the handshake. Functions should
	// return an error to reject the peer.
	FilterPeerByID utils.Option[func(context.Context, types.NodeID) error]

	// MaxDialRate limits the rate at which router is dialing peers. Defaults to 0.1/s.
	MaxDialRate utils.Option[rate.Limit]

	// MaxAcceptRate limits the rate at which router is accepting TCP connections. Defaults to 1/s.
	MaxAcceptRate utils.Option[rate.Limit]

	// ResolveTimeout is the timeout for resolving NodeAddress URLs.
	ResolveTimeout utils.Option[time.Duration]

	// DialTimeout is the timeout for dialing a peer.
	DialTimeout utils.Option[time.Duration]

	// HandshakeTimeout is the timeout for handshaking with a peer.
	HandshakeTimeout utils.Option[time.Duration]

	// List of peers to be added to the peer store on startup.
	BootstrapPeers []NodeAddress

	// PersistentPeers are peers that we want to maintain persistent connections to.
	// We will not preserve any addresses different than those specified in the config,
	// since they are forgeable.
	PersistentPeers []NodeAddress

	// Peers which we will unconditionally accept connections from.
	UnconditionalPeers []types.NodeID

	// Only include those peers for block sync.
	// These are also unconditional peers.
	// If empty, all peers are used for block sync.
	BlockSyncPeers []types.NodeID

	// PrivatePeerIDs defines a set of NodeID objects which the PEX reactor will
	// consider private and never gossip.
	PrivatePeers []types.NodeID

	// MaxPeers is the maximum number of peers to track address information about.
	// When exceeded, unreachable peers will be deleted.
	// Defaults to 128.
	MaxPeers utils.Option[int]

	// MaxConnected is the maximum number of connected peers (inbound and outbound).
	// Persistent and unconditional connections are not counted towards this limit.
	// Defaults to 64.
	MaxConnected utils.Option[int]

	// MaxOutboundConnections is the maximum number of outbound connections.
	// Note that MaxConnected is still respected and the actual number of outbound connections
	// is bounded by min(MaxConnected,MaxOutboundConnections)
	// Defaults to 10.
	MaxOutboundConnections utils.Option[int]

	// MaxConcurrentDials limits the number of concurrent outbound connection handshakes.
	// Defaults to 10.
	MaxConcurrentDials utils.Option[int]

	// MaxConncurrentAccepts limites the number of concurrent inbound connection handshakes.
	// Defaults to 10.
	MaxConcurrentAccepts utils.Option[int]

	// Per-connection config.
	Connection conn.MConnConfig

	// Frequency of dumping connected peers list to the db.
	// Defaults to 10s.
	PeerStoreInterval utils.Option[time.Duration]
}

func (o *RouterOptions) maxDials() int   { return o.MaxConcurrentDials.Or(10) }
func (o *RouterOptions) maxAccepts() int { return o.MaxConcurrentAccepts.Or(10) }
func (o *RouterOptions) maxConns() int   { return o.MaxConnected.Or(64) }
func (o *RouterOptions) maxOutboundConns() int {
	return min(o.maxConns(), o.MaxOutboundConnections.Or(10))
}

func (o *RouterOptions) maxPeers() int {
	return o.MaxPeers.Or(128)
}

func (o *RouterOptions) peerStoreInterval() time.Duration {
	return o.PeerStoreInterval.Or(10 * time.Second)
}

// Validate validates the options.
func (o *RouterOptions) Validate() error {
	for _, addr := range o.BootstrapPeers {
		if err := addr.Validate(); err != nil {
			return fmt.Errorf("invalid BoodstrapPeer address %v: %w", addr, err)
		}
	}
	for _, addr := range o.PersistentPeers {
		if err := addr.Validate(); err != nil {
			return fmt.Errorf("invalid PersistentPeer address %v: %w", addr, err)
		}
	}
	for _, id := range o.BlockSyncPeers {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("invalid block sync peer ID %q: %w", id, err)
		}
	}
	for _, id := range o.UnconditionalPeers {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("invalid unconditional peer ID %q: %w", id, err)
		}
	}
	for _, id := range o.PrivatePeers {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("invalid private peer ID %q: %w", id, err)
		}
	}
	return nil
}

func (o *RouterOptions) maxDialRate() rate.Limit {
	return o.MaxDialRate.Or(rate.Every(10 * time.Second))
}

func (o *RouterOptions) maxAcceptRate() rate.Limit {
	return o.MaxAcceptRate.Or(rate.Every(time.Second))
}

func (o *RouterOptions) incomingConnectionWindow() time.Duration {
	return o.IncomingConnectionWindow.Or(100 * time.Millisecond)
}

func (o *RouterOptions) maxIncomingConnectionAttempts() uint {
	return o.MaxIncomingConnectionAttempts.Or(100)
}

func (o *RouterOptions) filterPeerByIP(ctx context.Context, addrPort netip.AddrPort) error {
	if f, ok := o.FilterPeerByIP.Get(); ok {
		return f(ctx, addrPort)
	}
	return nil
}

func (o *RouterOptions) filterPeerByID(ctx context.Context, id types.NodeID) error {
	if f, ok := o.FilterPeerByID.Get(); ok {
		return f(ctx, id)
	}
	return nil
}
