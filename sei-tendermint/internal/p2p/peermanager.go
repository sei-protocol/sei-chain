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

// Conn {ID(), channels(), Close(), Run()}
// PeerManager.New(Dialer func(ctx,add) (Conn,error))
// Accept(Conn)
// Delete(id) - close and forget

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

// PeerScore is a numeric score assigned to a peer (higher is better).
type PeerScore uint8

const (
	PeerScoreUnconditional    PeerScore = math.MaxUint8                  // unconditional peers, 255
	PeerScorePersistent       PeerScore = PeerScoreUnconditional - 1     // persistent peers, 254
	MaxPeerScoreNotPersistent PeerScore = PeerScorePersistent - 1        // not persistent peers, 253
	DefaultMutableScore       PeerScore = MaxPeerScoreNotPersistent - 10 // mutable score, 243
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
	BlockSyncPeers map[types.NodeID][]NodeAddress

	// PrivatePeerIDs defines a set of NodeID objects which the PEX reactor will
	// consider private and never gossip.
	PrivatePeers map[types.NodeID]bool

	// MaxPeers is the maximum number of peers to track information about, i.e.
	// store in the peer store. When exceeded, the lowest-scored unconnected peers
	// will be deleted.
	MaxPeers utils.Option[uint]

	// MaxConnected is the maximum number of connected peers (inbound and
	// outbound). 0 means no limit.
	MaxConnected utils.Option[uint]

	Retry utils.Option[*RetryOptions]

	// MaxConnectedUpgrade is the maximum number of additional connections to
	// use for probing any better-scored peers to upgrade to when all connection
	// slots are full. 0 disables peer upgrading.
	//
	// For example, if we are already connected to MaxConnected peers, but we
	// know or learn about better-scored peers (e.g. configured persistent
	// peers) that we are not connected too, then we can probe these peers by
	// using up to MaxConnectedUpgrade connections, and once connected evict the
	// lowest-scored connected peers. This also works for inbound connections,
	// i.e. if a higher-scored peer attempts to connect to us, we can accept
	// the connection and evict a lower-scored peer.
	MaxConnectedUpgrade uint

	// PeerScores sets fixed scores for specific peers. It is mainly used
	// for testing. A score of 0 is ignored.
	PeerScores map[types.NodeID]PeerScore

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

func (o *RetryOptions) delay(failures uint32, persistent bool) utils.Option[time.Duration] {
	// We compare values as float64 to avoid overflows.
	delay := float64(int64(o.Min)) * math.Pow(2, float64(failures))
	// TODO: this shit should be deterministic
	delay += float64(rand.Int63n(int64(o.Jitter+1)))
	if persistent {
		// We need to retry persistent peers indefinitely, so just cap to MaxPersistent.
		maxDelay := o.MaxPersistent.Or(o.Max)
		if delay > float64(maxDelay) {
			return utils.Some(maxDelay)
		}
	} else {
		// Other peers should be abandoned after Max.
		if delay > float64(o.Max) {
			return utils.None[time.Duration]()
		}
	}
	return utils.Some(time.Duration(delay))
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

func (m *PeerManager) GetBlockSyncPeers() map[types.NodeID]bool {
	return m.options.BlockSyncPeers
}

func (m *PeerManager) AddPrivatePeer(id types.NodeID) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.dynamicPrivatePeers[id] = struct{}{}
}

// Add adds a peer to the manager, given as an address. If the peer already
// exists, the address is added to it if it isn't already present. This will push
// low scoring peers out of the address book if it exceeds the maximum size.
func (m *PeerManager) Add(address NodeAddress) (bool, error) {
	for s := range m.store.Lock() {
		return s.Add(address)
	}
	panic("unreachable")
}

func (m *PeerManager) Delete(id types.NodeID) error {
	for s := range m.store.Lock() {
		return s.Delete(id)
	}
	panic("unreachable")
}

// PeerRatio returns the ratio of peer addresses stored to the maximum size.
func (m *PeerManager) PeerRatio() float64 {
	for s := range m.store.Lock() {
		maxPeers,ok := s.options.MaxPeers.Get()
		if !ok { return 0 }
		return float64(s.Size()) / float64(maxPeers)
	}
	panic("unreachable")
}

// DialNext finds an appropriate peer address to dial, and marks it as dialing.
// If no peer is found, or all connection slots are full, it blocks until one
// becomes available. The caller must call Dialed() or DialFailed() for the
// returned peer.
func (m *PeerManager) DialNext(ctx context.Context) (NodeAddress, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// We allow dialing MaxConnected+MaxConnectedUpgrade peers. Including
	// MaxConnectedUpgrade allows us to probe additional peers that have a
	// higher score than any other peers, and if successful evict it.
	if maxConnected,ok := m.options.MaxConnected.Get(); ok && m.numConnected()+len(m.dialing) >=
		int(maxConnected)+int(m.options.MaxConnectedUpgrade) {
		return NodeAddress{}, nil
	}

	for _, peer := range m.store.Ranked() {
		if m.dialing[peer.ID] || m.connected[peer.ID] {
			continue
		}

		for addr, info := range peer.AddressInfo {
			if t,ok := info.LastDialFailure.Get(); ok {
				d,ok := m.store.RetryDelay(addr).Get()
				if !ok || time.Since(t) < d { continue }
			}

			// We now have an eligible address to dial. If we're full but have
			// upgrade capacity (as checked above), we find a lower-scored peer
			// we can replace and mark it as upgrading so no one else claims it.
			//
			// If we don't find one, there is no point in trying additional
			// peers, since they will all have the same or lower score than this
			// peer (since they're ordered by score via peerStore.Ranked).
			if maxConnected,ok := m.options.MaxConnected.Get(); ok && m.numConnected() >= int(maxConnected) {
				upgradeFromPeer := m.findUpgradeCandidate(peer.ID)
				if upgradeFromPeer == "" {
					return NodeAddress{}, nil
				}
				m.upgrading[upgradeFromPeer] = peer.ID
			}

			m.dialing[peer.ID] = true
			m.logger.Debug(fmt.Sprintf("Going to dial peer %s with address %s", peer.ID, addressInfo.Address))
			return addressInfo.Address, nil
		}
	}
	return NodeAddress{}, nil
}

// DialFailed reports a failed dial attempt. This will make the peer available
// for dialing again when appropriate (possibly after a retry timeout).
func (m *PeerManager) DialFailed(ctx context.Context, address NodeAddress) error {
	for s := range m.store.Lock() {
		return s.DialFailed(address)
	}
	panic("unreachable")
}

// Dialed marks a peer as successfully dialed. Any further connections will be
// rejected, and once disconnected the peer may be dialed again.
func (m *PeerManager) Dialed(address NodeAddress) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	delete(m.dialing, address.NodeID)

	var upgradeFromPeer utils.Option[types.NodeID]
	for from, to := range m.upgrading {
		if to == address.NodeID {
			delete(m.upgrading, from)
			upgradeFromPeer = utils.Some(from)
			// Don't break, just in case this peer was marked as upgrading for
			// multiple lower-scored peers (shouldn't really happen).
		}
	}
	if address.NodeID == m.options.SelfID {
		return fmt.Errorf("rejecting connection to self (%v)", address.NodeID)
	}
	if m.connected[address.NodeID] {
		return fmt.Errorf("cant dial, peer=%q is already connected", address.NodeID)
	}
	if m.options.MaxConnected > 0 && m.numConnected() >= int(m.options.MaxConnected) {
		if upgradeFromPeer == "" || m.numConnected() >=
			int(m.options.MaxConnected)+int(m.options.MaxConnectedUpgrade) {
			return fmt.Errorf("dialed peer %q failed, is already connected to maximum number of peers", address.NodeID)
		}
	}

	if ok,err := m.store.Dialed(address); err!=nil {
		return err
	} else if !ok {
		return fmt.Errorf("peer %q was removed while dialing", address.NodeID)
	}

	if from,ok := upgradeFromPeer.Get(); ok {
		m.evict[from] = errors.New("too many peers")
	}
	m.connected[address.NodeID] = true
	m.evictWaker.Wake()
	return nil
}

// Accepted marks an incoming peer connection successfully accepted. If the peer
// is already connected or we don't allow additional connections then this will
// return an error.
//
// If full but MaxConnectedUpgrade is non-zero and the incoming peer is
// better-scored than any existing peers, then we accept it and evict a
// lower-scored peer.
func (m *PeerManager) TryAccept(peerID types.NodeID, channels ChannelIDSet) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if peerID == m.options.SelfID {
		return fmt.Errorf("rejecting connection from self (%v)", peerID)
	}
	if m.connected[peerID] {
		return fmt.Errorf("can't accept, peer=%q is already connected", peerID)
	}
	if !m.options.UnconditionalPeers[peerID] && m.options.MaxConnected > 0 &&
		m.numConnected() >= int(m.options.MaxConnected)+int(m.options.MaxConnectedUpgrade) {
		return fmt.Errorf("accepted peer %q failed, already connected to maximum number of peers", peerID)
	}

	peer, ok := m.store.Get(peerID)
	if !ok {
		peer = m.newPeerInfo(peerID)
	}

	// reset this to avoid penalizing peers for their past transgressions
	for _, addr := range peer.AddressInfo {
		addr.DialFailures = 0
	}

	// If all connections slots are full, but we allow upgrades (and we checked
	// above that we have upgrade capacity), then we can look for a lower-scored
	// peer to replace and if found accept the connection anyway and evict it.
	var upgradeFromPeer types.NodeID
	if m.options.MaxConnected > 0 && m.numConnected() >= int(m.options.MaxConnected) {
		upgradeFromPeer = m.findUpgradeCandidate(peer.ID)
		if upgradeFromPeer == "" {
			return fmt.Errorf("upgrade peer %q failed, already connected to maximum number of peers", peer.ID)
		}
	}

	peer.LastConnected = utils.Some(time.Now().UTC())
	if err := m.store.Set(peer); err != nil {
		return err
	}

	m.connected[peerID] = true
	if upgradeFromPeer != "" {
		m.evict[upgradeFromPeer] = errors.New("found better peer")
	}
	m.evictWaker.Wake()
	return nil
}

// Ready marks a peer as ready, broadcasting status updates to
// subscribers. The peer must already be marked as connected. This is
// separate from Dialed() and Accepted() to allow the router to set up
// its internal queues before reactors start sending messages. The
// channels set here are passed in the peer update broadcast to
// reactors, which can then mediate their own behavior based on the
// capability of the peers.
func (m *PeerManager) Ready(ctx context.Context, peerID types.NodeID, channels ChannelIDSet) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.connected[peerID] {
		for ready, ctrl := range m.ready.Lock() {
			ready[peerID] = channels
			ctrl.Updated()
		}
		m.broadcast(ctx, PeerUpdate{
			NodeID:   peerID,
			Status:   PeerStatusUp,
			Channels: channels,
		})
	}
}

type Eviction struct {
	ID    types.NodeID
	Cause error
}

// TryEvictNext is equivalent to EvictNext, but immediately returns an empty
// node ID if no evictable peers are found.
func (m *PeerManager) evicPeersRoutine(ctx context.Context) (error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// If any connected peers are explicitly scheduled for eviction, we return a
	// random one.
	for peerID, cause := range m.evict {
		delete(m.evict, peerID)
		if m.connected[peerID] && !m.evicting[peerID] {
			m.evicting[peerID] = true
			return utils.Some(Eviction{peerID, cause}), nil
		}
	}

	// If we're below capacity, we don't need to evict anything.
	if m.options.MaxConnected == 0 ||
		m.numConnected()-len(m.evicting) <= int(m.options.MaxConnected) {
		return utils.None[Eviction](), nil
	}

	// If we're above capacity (shouldn't really happen), just pick the
	// lowest-ranked peer to evict.
	ranked := m.store.Ranked()
	for i := len(ranked) - 1; i >= 0; i-- {
		peer := ranked[i]
		if m.connected[peer.ID] && !m.evicting[peer.ID] {
			m.evicting[peer.ID] = true
			return utils.Some(Eviction{peer.ID, errors.New("too many peers")}), nil
		}
	}

	return utils.None[Eviction](), nil
}

func (m *PeerManager) SendError(pe PeerError) {
	for s := range m.store.Lock() {
		s.SendError(pe)
	}
}

// Subscribe subscribes to peer updates. The caller must consume the peer
// updates in a timely fashion and close the subscription when done, otherwise
// the PeerManager will halt.
func (m *PeerManager) Subscribe(ctx context.Context) *PeerUpdates {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	peerUpdates := NewPeerUpdates(make(chan PeerUpdate, 1), 1)
	m.subscriptions[peerUpdates] = peerUpdates

	var updates []PeerUpdate
	for ready := range m.ready.Lock() {
		for id, channels := range ready {
			updates = append(updates, PeerUpdate{
				NodeID:   id,
				Status:   PeerStatusUp,
				Channels: channels,
			})
		}
	}
	peerUpdates.preexistingPeers = updates

	go func() {
		<-ctx.Done()
		m.mtx.Lock()
		defer m.mtx.Unlock()
		delete(m.subscriptions, peerUpdates)
	}()
	return peerUpdates
}

// broadcast broadcasts a peer update to all subscriptions. The caller must
// already hold the mutex lock, to make sure updates are sent in the same order
// as the PeerManager processes them, but this means subscribers must be
// responsive at all times or the entire PeerManager will halt.
//
// FIXME: Consider using an internal channel to buffer updates while also
// maintaining order if this is a problem.
func (m *PeerManager) broadcast(ctx context.Context, peerUpdate PeerUpdate) {
	for _, sub := range m.subscriptions {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case sub.reactorUpdatesCh <- peerUpdate:
		}
	}
}

/*
// Added for unit test
func (m *PeerManager) MarkReadyConnected(nodeId types.NodeID) {
	for ready, ctrl := range m.ready.Lock() {
		ready[nodeId] = ChannelIDSet{}
		ctrl.Updated()
	}
	m.connected[nodeId] = true
}*/

func (m *PeerManager) Score(id types.NodeID) int {
	for s := range m.store.Lock() {
		return int(s.Score(id)) // TODO: it used to be -1 for missing peers.
	}
	panic("unreachable")
}

func (m *PeerManager) SendUpdate(pu PeerUpdate) {
	for s := range m.store.Lock() {
		switch pu.Status {
		case PeerStatusBad: s.UpdateScore(pu.NodeID,-1)
		case PeerStatusGood: s.UpdateScore(pu.NodeID,1)
		}
	}
	panic("unreachable")
}

func (m *PeerManager) numConccurentDials() int {
	if f,ok := m.options.NumConcurrentDials.Get(); ok {
		return f()
	}
	return runtime.NumCPU()
}

func (m *PeerManager) Run(ctx context.Context, r *Router) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		sem := semaphore.NewWeighted(int64(m.numConccurentDials()))
		for {
			if err := sem.Acquire(ctx, 1); err != nil {
				return err
			}
			addr, err := m.DialNext(ctx)
			if err != nil {
				return fmt.Errorf("failed to find next peer to dial: %w", err)
			}
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
						if err := s.Delete(addr.NodeID); err!=nil {
							return fmt.Errorf("s.Delete(): %w",err)
						}
					} else if err!=nil {
						if err := s.DialFailed(addr); err != nil {
							return fmt.Errorf("r.peerManger.DialFailed(): %w",err)
						}
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

// Status returns the status for a peer, primarily for testing.
func (s *peerStore) IsReady(id types.NodeID) bool {
	if peer,ok := s.peers[id]; ok {
		return peer.conn.IsPresent()
	}
	return false
}

