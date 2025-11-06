package p2p

import (
	"context"
	"fmt"
	"runtime"
	"math"
	"time"
	"net/netip"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/types"
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

// PeerManagerOptions specifies options for a PeerManager.
type PeerManagerOptions struct {
	SelfID types.NodeID
	// List of peers to be added to the peer store on startup.
	BootstrapPeers []NodeAddress

	// PersistentPeers are peers that we want to maintain persistent connections to.
	// We will not preserve any addresses different than those specified in the config,
	// since they are forgeable.
	PersistentPeers []NodeAddress

	// Peers which we will unconditionally accept connections from.
	UnconditionalPeers []types.NodeID

	// Only include those peers for block sync.
	// These are also persistent peers.
	// If empty, all peers are used for block sync.
	BlockSyncPeers []types.NodeID

	// PrivatePeerIDs defines a set of NodeID objects which the PEX reactor will
	// consider private and never gossip.
	PrivatePeers []types.NodeID

	// MaxPeers is the maximum number of peers to track information about, i.e.
	// store in the peer store. When exceeded, unreachable peers will be deleted.
	MaxPeers utils.Option[int]

	// MaxConnected is the maximum number of connected peers (inbound and outbound).
	MaxConnected utils.Option[int]

	// SelfAddress is the address that will be advertised to peers for them to dial back to us.
	// If Hostname and Port are unset, Advertise() will include no self-announcement
	SelfAddress utils.Option[NodeAddress]

	// MaxIncomingConnectionAttempts rate limits the number of incoming connection
	// attempts per IP address. Defaults to 100.
	MaxIncomingConnectionAttempts utils.Option[uint]

	// IncomingConnectionWindow describes how often an IP address
	// can attempt to create a new connection. Defaults to 100ms.
	IncomingConnectionWindow utils.Option[time.Duration]

	// NumConcrruentDials controls how many parallel go routines
	// are used to dial peers. This defaults to the value of
	// runtime.NumCPU.
	NumConcurrentDials utils.Option[func() int]

	// DialSleep controls the amount of time that the router
	// sleeps between dialing peers. If not set, a default value
	// is used that sleeps for a (random) amount of time up to 3
	// seconds between submitting each peer to be dialed.
	DialSleep utils.Option[func(context.Context) error]

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
}

func (o *PeerManagerOptions) filterPeersIP(ctx context.Context, addrPort netip.AddrPort) error {
	if f,ok := o.FilterPeerByIP.Get(); ok {
		return f(ctx, addrPort)
	}
	return nil
}

func (o *PeerManagerOptions) filterPeersID(ctx context.Context, id types.NodeID) error {
	if f,ok := o.FilterPeerByID.Get(); ok {
		return f(ctx, id)
	}
	return nil
}

func (o *PeerManagerOptions) persistent(id types.NodeID) bool {
	if _,ok := o.PersistentPeers[id]; ok { return true }
	if _,ok := o.BlockSyncPeers[id]; ok { return true }
	if _,ok := o.UnconditionalPeers[id]; ok { return true }
	return false
}

func (o *PeerManagerOptions) maxDials() int {
	if f,ok := o.NumConcurrentDials.Get(); ok {
		return f()
	}
	return runtime.NumCPU()
}

func (o *PeerManagerOptions) maxConns() int {
	return o.MaxConnected.Or(math.MaxInt)
}

func (o *PeerManagerOptions) maxPeers() int {
	return o.MaxPeers.Or(math.MaxInt)
}

func (o *PeerManagerOptions) getIncomingConnectionWindow() time.Duration {
	return o.IncomingConnectionWindow.Or(100 * time.Millisecond)
}

func (o *PeerManagerOptions) getMaxIncomingConnectionAttempts() uint {
	return o.MaxIncomingConnectionAttempts.Or(100)
}

// Validate validates the options.
func (o *PeerManagerOptions) Validate() error {
	if err:=o.SelfID.Validate(); err!=nil {
		return fmt.Errorf("SelfID: %v", err)
	}
	for id,addrs := range o.PersistentPeers {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("invalid PersistentPeer ID %q: %w", id, err)
		}
		for _,addr := range addrs {
			if err := addr.Validate(); err != nil {
				return fmt.Errorf("invalid PersistentPeer address %v: %w", addr, err)
			}
		}
	}
	for id := range o.BlockSyncPeers {
		if err := id.Validate(); err!=nil {
			return fmt.Errorf("invalid block sync peer ID %q: %w", id, err)
		}
	}
	for id := range o.UnconditionalPeers {
		if err := id.Validate(); err!=nil {
			return fmt.Errorf("invalid unconditional peer ID %q: %w", id, err)
		}
	}
	for id := range o.PrivatePeers {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("invalid private peer ID %q: %w", id, err)
		}
	}

	if maxPeers,ok := o.MaxPeers.Get(); ok {
		if maxConnected,ok := o.MaxConnected.Get(); !ok || maxConnected > maxPeers {
			return fmt.Errorf("MaxConnected %v can't exceed MaxPeers %v", maxConnected, maxPeers)
		}
	}
	return nil
}

func (o *PeerManagerOptions) dialSleep(ctx context.Context) error {
	if f,ok := o.DialSleep.Get(); ok {
		return f(ctx)
	}
	return utils.Sleep(ctx, 1500 * time.Millisecond)
}
