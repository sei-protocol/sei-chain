package p2p

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/tendermint/tendermint/libs/log"

	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/types"
)

type ErrBadNetwork struct {error}

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
	PeerStatusGood PeerStatus = "good" // peer observed as good
	PeerStatusBad  PeerStatus = "bad"  // peer observed as bad
)

// PeerUpdate is a peer update event sent via PeerUpdates.
type PeerUpdate struct {
	NodeID   types.NodeID
	Status   PeerStatus
	Channels ChannelIDSet
}

// PeerUpdates is a peer update subscription with notifications about peer
// events (currently just status changes).
type PeerUpdates struct {
	preexistingPeers []PeerUpdate
	reactorUpdatesCh chan PeerUpdate
}

// NewPeerUpdates creates a new PeerUpdates subscription. It is primarily for
// internal use, callers should typically use PeerManager.Subscribe(). The
// subscriber must call Close() when done.
func NewPeerUpdates(updatesCh chan PeerUpdate, buf int) *PeerUpdates {
	return &PeerUpdates{
		reactorUpdatesCh: updatesCh,
	}
}

func (pu *PeerUpdates) PreexistingPeers() []PeerUpdate {
	return pu.preexistingPeers
}

// Updates returns a channel for consuming peer updates.
func (pu *PeerUpdates) Updates() <-chan PeerUpdate {
	return pu.reactorUpdatesCh
}

type RetryOptions struct {
	// Min is the minimum time to wait between retries. Retry times
	// double for each retry, up to MaxRetryTime. 0 disables retries.
	Min time.Duration // 0.25s

	// Max is the maximum time to wait between retries. 0 means
	// no maximum, in which case the retry time will keep doubling.
	Max time.Duration // 2m

	// MaxPersistent is the maximum time to wait between retries for
	// peers listed in PersistentPeers. Defaults to MaxRetryTime.
	MaxPersistent utils.Option[time.Duration] // 2m

	// Jitter is the upper bound of a random interval added to
	// retry times, to avoid thundering herds.
	Jitter time.Duration // 5s
}

// PeerManagerOptions specifies options for a PeerManager.
type PeerManagerOptions struct {
	SelfID types.NodeID
	// PersistentPeers are peers that we want to maintain persistent connections to.
	// We will not preserve any addresses different than those specified in the config,
	// since they are forgeable.
	PersistentPeers map[types.NodeID][]NodeAddress

	// Peers which we will unconditionally accept connections from.
	UnconditionalPeers map[types.NodeID]bool

	// Only include those peers for block sync.
	// These are also persistent peers.
	// If empty, all peers are used for block sync.
	BlockSyncPeers map[types.NodeID]bool

	// PrivatePeerIDs defines a set of NodeID objects which the PEX reactor will
	// consider private and never gossip.
	PrivatePeers map[types.NodeID]bool

	// MaxPeers is the maximum number of peers to track information about, i.e.
	// store in the peer store. When exceeded, the lowest-scored unconnected peers
	// will be deleted.
	MaxPeers utils.Option[int]

	// MaxConnected is the maximum number of connected peers (inbound and
	// outbound). 0 means no limit.
	MaxConnected utils.Option[uint]

	Retry utils.Option[*RetryOptions]

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
}

func (o *PeerManagerOptions) persistent(id types.NodeID) bool {
	if _,ok := o.PersistentPeers[id]; ok { return true }
	if _,ok := o.BlockSyncPeers[id]; ok { return true }
	if _,ok := o.UnconditionalPeers[id]; ok { return true }
	return false
}

func (o *PeerManagerOptions) numConccurentDials() int {
	if f,ok := o.NumConcurrentDials.Get(); ok {
		return f()
	}
	return runtime.NumCPU()
}

func (o *PeerManagerOptions) getIncomingConnectionWindow() time.Duration {
	return o.IncomingConnectionWindow.Or(100 * time.Millisecond)
}

func (o *PeerManagerOptions) getMaxIncomingConnectionAttempts() uint {
	return o.MaxIncomingConnectionAttempts.Or(100)
}

func (o *RetryOptions) Validate() error {
	if o.Min <= 0 {
		return fmt.Errorf("Min = %v, want > 0",o.Min)
	}
	if o.Max < o.Min {
		return fmt.Errorf("Max = %v, want >= Min = %v",o.Max,o.Min)
	}
	if mp,ok := o.MaxPersistent.Get(); ok {
		if mp < o.Min {
			return fmt.Errorf("MaxPersistent = %v, want >= Min = %v",mp,o.Min)
		}
	}
	if o.Jitter < 0 {
		return fmt.Errorf("Jitter = %v, want >= 0",o.Jitter)
	}
	return nil
}

// Validate validates the options.
func (o *PeerManagerOptions) Validate() error {
	if o.SelfID == "" {
		return errors.New("self ID not given")
	}
	for id := range o.PersistentPeers {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("invalid PersistentPeer ID %q: %w", id, err)
		}
	}
	for id := range o.BlockSyncPeers {
		if err := id.Validate(); err!=nil {
			return err
		}
	}
	for id := range o.UnconditionalPeers {
		if err := id.Validate(); err!=nil {
			return err
		}
	}
	for id := range o.PrivatePeers {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("invalid private peer ID %q: %w", id, err)
		}
	}

	if maxPeers,ok := o.MaxPeers.Get(); ok {
		if o.MaxConnected == 0 || o.MaxConnected+o.MaxConnectedUpgrade > maxPeers {
			return fmt.Errorf("MaxConnected %v and MaxConnectedUpgrade %v can't exceed MaxPeers %v",
				o.MaxConnected, o.MaxConnectedUpgrade, maxPeers)
		}
	}

	if ro,ok := o.Retry.Get(); ok {
		if err:=ro.Validate(); err!=nil {
			return fmt.Errorf("Retry: %w", err)
		}
	}
	return nil
}

// PeerManager manages peer lifecycle information, using a peerStore for
// underlying storage. Its primary purpose is to determine which peer to connect
// to next (including retry timers), make sure a peer only has a single active
// connection (either inbound or outbound), and evict peers to make room for
// higher-scored peers. It does not manage actual connections (this is handled
// by the Router), only the peer lifecycle state.
//
// For an outbound connection, the flow is as follows:
//   - DialNext: return a peer address to dial, mark peer as dialing.
//   - DialFailed: report a dial failure, unmark as dialing.
//   - Dialed: report a dial success, unmark as dialing and mark as connected
//     (errors if already connected, e.g. by Accepted).
//   - Ready: report routing is ready, mark as ready and broadcast PeerStatusUp.
//   - Disconnected: report peer disconnect, unmark as connected and broadcasts
//     PeerStatusDown.
//
// For an inbound connection, the flow is as follows:
//   - Accepted: report inbound connection success, mark as connected (errors if
//     already connected, e.g. by Dialed).
//   - Ready: report routing is ready, mark as ready and broadcast PeerStatusUp.
//   - Disconnected: report peer disconnect, unmark as connected and broadcasts
//     PeerStatusDown.
//
// When evicting peers, either because peers are explicitly scheduled for
// eviction or we are connected to too many peers, the flow is as follows:
//   - EvictNext: if marked evict and connected, unmark evict and mark evicting.
//     If beyond MaxConnected, pick lowest-scored peer and mark evicting.
//   - Disconnected: unmark connected, evicting, evict, and broadcast a
//     PeerStatusDown peer update.
//
// If all connection slots are full (at MaxConnections), we can use up to
// MaxConnectionsUpgrade additional connections to probe any higher-scored
// unconnected peers, and if we reach them (or they reach us) we allow the
// connection and evict a lower-scored peer. We mark the lower-scored peer as
// upgrading[from]=to to make sure no other higher-scored peers can claim the
// same one for an upgrade. The flow is as follows:
//   - Accepted: if upgrade is possible, mark connected and add lower-scored to evict.
//   - DialNext: if upgrade is possible, mark upgrading[from]=to and dialing.
//   - DialFailed: unmark upgrading[from]=to and dialing.
//   - Dialed: unmark upgrading[from]=to and dialing, mark as connected, add
//     lower-scored to evict.
//   - EvictNext: pick peer from evict, mark as evicting.
//   - Disconnected: unmark connected, upgrading[from]=to, evict, evicting.
type PeerManager struct {
	logger     log.Logger
	options    PeerManagerOptions
	connTracker   *connTracker

	mtx                 sync.Mutex
	store               utils.Mutex[*peerStore]
	subscriptions       map[*PeerUpdates]*PeerUpdates              // keyed by struct identity (address)
	metrics             *Metrics
}

// NewPeerManager creates a new peer manager.
func NewPeerManager(
	logger log.Logger,
	peerDB dbm.DB,
	options PeerManagerOptions,
	metrics *Metrics,
) (*PeerManager, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	store, err := newPeerStore(peerDB, &options, metrics)
	if err != nil {
		return nil, err
	}

	peerManager := &PeerManager{
		logger:     logger,
		options:    options,
		connTracker: newConnTracker(
			options.getMaxIncomingConnectionAttempts(),
			options.getIncomingConnectionWindow(),
		),

		store:               utils.NewMutex(store),
		dynamicPrivatePeers: map[types.NodeID]struct{}{},
		subscriptions:       map[*PeerUpdates]*PeerUpdates{},
		metrics:             metrics,
	}
	return peerManager, nil
}

// DialNext finds an appropriate peer address to dial, and marks it as dialing.
// If no peer is found, or all connection slots are full, it blocks until one
// becomes available. The caller must call Dialed() or DialFailed() for the
// returned peer.
func (m *PeerManager) DialNext() utils.Option[NodeAddress] {
	// Wait until there is less connections than Max.
	// Wait until there are peers to dial.
	for s,ctrl := range m.store.Lock() {
		ctrl.WaitUntil(ctx,func()bool {

		for id,addrs := range s.addrs {
			if _,ok := s.conns[id]; ok { continue }
			// find not connected/dialing peer with the oldest failure (or none)
		}
	}
	return res
}

func (m *PeerManager) Run(ctx context.Context, r *Router) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		sem := semaphore.NewWeighted(int64(m.numConccurentDials()))
		for {
			if err := sem.Acquire(ctx, 1); err != nil {
				return err
			}
			// TODO: separate routine for dialing persistent and regular peers.
			addr,err := m.DialNext(ctx)
			if err!=nil { return err }
			s.Spawn(func() error {
				r.logger.Debug("Going to dial", "peer", addr.NodeID)
				// TODO(gprusak): this symmetric logic for handling duplicate connections is a source of race conditions:
				// if 2 nodes try to establish a connection to each other at the same time, both connections will be dropped.
				// Instead either:
				// * break the symmetry by favoring incoming connection iff my.NodeID > peer.NodeID
				// * keep incoming and outcoming connection pools separate to avoid the collision (recommended)
				conn, err := r.ConnectPeer(ctx, addr)
				sem.Release(1)
				if errors.Is(err, context.Canceled) {
					return nil
				}
				for s := range m.store.Lock() {
					if utils.ErrorAs[errBadNetwork](err).IsPresent() {
						if err := s.Forget(addr.NodeID); err!=nil {
							return fmt.Errorf("s.Delete(): %w",err)
						}
					} else if err!=nil {
						s.DialFailed(addr)
						m.logger.Error("failed to dial TODO","err",err)
						return nil
					} else if err := s.Dialed(addr, conn); err != nil {
						conn.Close()
						return fmt.Errorf("failed to dial outgoing/dialing peer %v: %w", address.NodeID, err)
					}
				}
				err = conn.Run(ctx, r)
				m.logger.Error("[dial] Run()", "err", err)
				for s := range s.store.Lock() {
					s.Disconnected(conn)
				}
				return nil
			})

			// this jitters the frequency that we call
			// DialNext and prevents us from attempting to
			// create connections too quickly.
			if err := m.options.dialSleep(ctx); err != nil {
				return err
			}
		}
	})
}

func (o *PeerManagerOptions) dialSleep(ctx context.Context) error {
	if f,ok := o.DialSleep.Get(); ok {
		return f(ctx)
	}
	const (
		maxDialerInterval = 3000
		minDialerInterval = 250
	)

	// nolint:gosec // G404: Use of weak random number generator
	dur := time.Duration(rand.Int63n(maxDialerInterval-minDialerInterval+1) + minDialerInterval)
	return utils.Sleep(ctx, dur*time.Millisecond)
}

func (m *PeerManager) AcceptAndRun(ctx context.Context, tcpConn *net.TCPConn) error {
	defer tcpConn.Close()
	m.metrics.NewConnections.With("direction", "in").Add(1)
	incomingAddr := remoteEndpoint(tcpConn).AddrPort
	if err := m.connTracker.AddConn(incomingAddr); err != nil {
		return fmt.Errorf("rate limiting incoming peer %v: %w", incomingAddr, err)
	}
	defer m.connTracker.RemoveConn(incomingAddr)

	if err := m.filterPeersIP(ctx, incomingAddr); err != nil {
		m.logger.Debug("peer filtered by IP", "ip", incomingAddr, "err", err)
		return nil
	}

	conn, err := m.handshakePeer(ctx, tcpConn, utils.None[types.NodeID]())
	if err != nil {
		return fmt.Errorf("r.handshakePeer(): %v: %w", tcpConn, err)
	}
	peerInfo := conn.PeerInfo()
	if err := m.filterPeersID(ctx, peerInfo.NodeID); err != nil {
		r.logger.Debug("peer filtered by node ID", "node", peerInfo.NodeID, "err", err)
		return nil
	}
	if err := m.Accepted(conn); err != nil {
		return fmt.Errorf("failed to accept connection: op=incoming/accepted, peer=%v: %w", peerInfo.NodeID, err)
	}
	return conn.Run(ctx, r)
}

func (s *peerStore) State(id types.NodeID) string {
	states := []string{}
	if peer,ok := s.peers[id]; ok {
		if peer.dialing {
			states = append(states, "dialing")
		} else if peer.conn.IsPresent() {
			states = append(states, "ready", "connected")
		}
	}
	return strings.Join(states, ",")
}
