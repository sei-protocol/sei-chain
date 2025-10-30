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

	tmsync "github.com/tendermint/tendermint/internal/libs/sync"
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
	Min time.Duration

	// Max is the maximum time to wait between retries. 0 means
	// no maximum, in which case the retry time will keep doubling.
	Max time.Duration

	// MaxPersistent is the maximum time to wait between retries for
	// peers listed in PersistentPeers. Defaults to MaxRetryTime.
	MaxPersistent utils.Option[time.Duration]

	// Jitter is the upper bound of a random interval added to
	// retry times, to avoid thundering herds.
	Jitter time.Duration
}

// PeerManagerOptions specifies options for a PeerManager.
type PeerManagerOptions struct {
	SelfID types.NodeID
	// PersistentPeers are peers that we want to maintain persistent connections
	// to. These will be scored higher than other peers, and if
	// MaxConnectedUpgrade is non-zero any lower-scored peers will be evicted if
	// necessary to make room for these.
	PersistentPeers map[types.NodeID]bool

	// Peers to which a connection will be (re)established, dropping an existing peer if any existing limit has been reached
	UnconditionalPeers map[types.NodeID]bool

	// Only include those peers for block sync
	BlockSyncPeers map[types.NodeID]bool

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

	if o.MaxConnected > 0 && len(o.PersistentPeers) > int(o.MaxConnected) {
		return fmt.Errorf("number of persistent peers %v can't exceed MaxConnected %v",
			len(o.PersistentPeers), o.MaxConnected)
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
	dialWaker  *tmsync.Waker // wakes up DialNext() on relevant peer changes
	evictWaker *tmsync.Waker // wakes up EvictNext() on relevant peer changes

	mtx                 sync.Mutex
	dynamicPrivatePeers map[types.NodeID]struct{} // dynamically added private peers
	store               *peerStore
	subscriptions       map[*PeerUpdates]*PeerUpdates              // keyed by struct identity (address)

	dialing             map[types.NodeID]bool                      // peers being dialed (DialNext → Dialed/DialFail)
	upgrading           map[types.NodeID]types.NodeID              // peers claimed for upgrade (DialNext → Dialed/DialFail)
	connected           map[types.NodeID]bool                      // connected peers (Dialed/Accepted → Disconnected)
	ready               utils.Watch[map[types.NodeID]ChannelIDSet] // ready peers (Ready → Disconnected)
	evict               map[types.NodeID]error                     // peers scheduled for eviction (Connected → EvictNext)
	evicting            map[types.NodeID]bool                      // peers being evicted (EvictNext → Disconnected)
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
		dialWaker:  tmsync.NewWaker(),
		evictWaker: tmsync.NewWaker(),

		store:               store,
		dynamicPrivatePeers: map[types.NodeID]struct{}{},
		dialing:             map[types.NodeID]bool{},
		upgrading:           map[types.NodeID]types.NodeID{},
		connected:           map[types.NodeID]bool{},
		ready:               utils.NewWatch(map[types.NodeID]ChannelIDSet{}),
		evict:               map[types.NodeID]error{},
		evicting:            map[types.NodeID]bool{},
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
	m.mtx.Lock()
	defer m.mtx.Unlock()
	ok,err := m.store.Add(address)
	if err!=nil || !ok { return ok,err }
	m.dialWaker.Wake()
	return true, nil
}

func (m *PeerManager) Delete(id types.NodeID) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.store.Delete(id)
}

// PeerRatio returns the ratio of peer addresses stored to the maximum size.
func (m *PeerManager) PeerRatio() float64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	maxPeers,ok := m.options.MaxPeers.Get()
	if !ok { return 0 }
	return float64(m.store.Size()) / float64(maxPeers)
}

// DialNext finds an appropriate peer address to dial, and marks it as dialing.
// If no peer is found, or all connection slots are full, it blocks until one
// becomes available. The caller must call Dialed() or DialFailed() for the
// returned peer.
func (m *PeerManager) DialNext(ctx context.Context) (NodeAddress, error) {
	for {
		address, err := m.TryDialNext()
		if err != nil || (address != NodeAddress{}) {
			return address, err
		}
		select {
		case <-m.dialWaker.Sleep():
		case <-ctx.Done():
			return NodeAddress{}, ctx.Err()
		}
	}
}

// TryDialNext is equivalent to DialNext(), but immediately returns an empty
// address if no peers or connection slots are available.
func (m *PeerManager) TryDialNext() (NodeAddress, error) {
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
	m.mtx.Lock()
	defer m.mtx.Unlock()

	delete(m.dialing, address.NodeID)
	for from, to := range m.upgrading {
		if to == address.NodeID {
			delete(m.upgrading, from) // Unmark failed upgrade attempt.
		}
	}

	if ok,err := m.store.DialFailed(address); err!=nil {
		return err
	} else if !ok {
		// Peer may have been removed while dialing, ignore.
		return nil
	}

	// We spawn a goroutine that notifies DialNext() again when the retry
	// timeout has elapsed, so that we can consider dialing it again. We
	// calculate the retry delay outside the goroutine, since it must hold
	// the mutex lock.
	if d,ok := m.store.RetryDelay(address).Get(); ok {
		go func() {
			select {
			case <-time.After(d):
				m.dialWaker.Wake()
			case <-ctx.Done():
			}
		}()
	} else {
		if err := m.store.Delete(address.NodeID); err != nil {
			return err
		}
	}
	return nil
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
//
// NOTE: We can't take an address here, since e.g. TCP uses a different port
// number for outbound traffic than inbound traffic, so the peer's endpoint
// wouldn't necessarily be an appropriate address to dial.
//
// FIXME: When we accept a connection from a peer, we should register that
// peer's address in the peer store so that we can dial it later. In order to do
// that, we'll need to get the remote address after all, but as noted above that
// can't be the remote endpoint since that will have the wrong port
// number.
func (m *PeerManager) Accepted(peerID types.NodeID) error {
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

// EvictNext returns the next peer to evict (i.e. disconnect). If no evictable
// peers are found, the call will block until one becomes available.
func (m *PeerManager) EvictNext(ctx context.Context) (Eviction, error) {
	for {
		ev, err := m.TryEvictNext()
		if err != nil {
			return Eviction{}, err
		}
		if ev, ok := ev.Get(); ok {
			return ev, nil
		}
		select {
		case <-m.evictWaker.Sleep():
		case <-ctx.Done():
			return Eviction{}, ctx.Err()
		}
	}
}

type Eviction struct {
	ID    types.NodeID
	Cause error
}

// TryEvictNext is equivalent to EvictNext, but immediately returns an empty
// node ID if no evictable peers are found.
func (m *PeerManager) TryEvictNext() (utils.Option[Eviction], error) {
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

// Disconnected unmarks a peer as connected, allowing it to be dialed or
// accepted again as appropriate.
func (m *PeerManager) Disconnected(ctx context.Context, peerID types.NodeID) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.store.Disconnected(peerID)
	delete(m.connected, peerID)
	delete(m.upgrading, peerID)
	delete(m.evict, peerID)
	delete(m.evicting, peerID)
	var isReady bool
	for ready, ctrl := range m.ready.Lock() {
		_, isReady = ready[peerID]
		delete(ready, peerID)
		ctrl.Updated()
	}

	if isReady {
		m.broadcast(ctx, PeerUpdate{
			NodeID: peerID,
			Status: PeerStatusDown,
		})
	}

	m.dialWaker.Wake()
}

// SendError reports a peer misbehavior to the router.
func (m *PeerManager) SendError(pe PeerError) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	shouldEvict := pe.Fatal
	if maxConnected,ok := m.options.MaxConnected.Get(); ok {
		shouldEvict = shouldEvict || (m.numConnected() >= int(maxConnected))
	}
	m.logger.Error("peer error",
		"peer", pe.NodeID,
		"err", pe.Err,
		"evicting", shouldEvict,
	)
	if shouldEvict {
		if m.connected[pe.NodeID] {
			m.evict[pe.NodeID] = pe.Err
		}
		m.evictWaker.Wake()
	} else {
		m.store.UpdateScore(pe.NodeID,-1)
	}
}

// Advertise returns a list of peer addresses to advertise to a peer.
//
// FIXME: This is fairly naïve and only returns the addresses of the
// highest-ranked peers.
func (m *PeerManager) Advertise(peerID types.NodeID, limit uint16) []NodeAddress {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	addresses := make([]NodeAddress, 0, limit)

	// advertise ourselves, to let everyone know how to dial us back
	// and enable mutual address discovery
	if addr,ok := m.options.SelfAddress.Get(); ok {
		addresses = append(addresses, addr)
	}

	for _, peer := range m.store.Ranked() {
		if peer.ID == peerID {
			continue
		}

		for nodeAddr, addressInfo := range peer.AddressInfo {
			if len(addresses) >= int(limit) {
				return addresses
			}

			// only add non-private NodeIDs
			if _, ok := m.options.PrivatePeers[nodeAddr.NodeID]; ok {
				continue
			}
			if _, ok := m.dynamicPrivatePeers[nodeAddr.NodeID]; ok {
				continue
			}
			addresses = append(addresses, addressInfo.Address)
		}
	}

	return addresses
}

func (m *PeerManager) numConnected() int {
	cnt := 0
	for peer := range m.connected {
		if !m.options.UnconditionalPeers[peer] {
			cnt++
		}
	}
	return cnt
}

func (m *PeerManager) NumConnected() int {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.numConnected()
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

// Status returns the status for a peer, primarily for testing.
func (m *PeerManager) Status(id types.NodeID) PeerStatus {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for ready := range m.ready.Lock() {
		if _, ok := ready[id]; ok {
			return PeerStatusUp
		}
		return PeerStatusDown
	}
	panic("unreachable")
}

func (m *PeerManager) State(id types.NodeID) string {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	states := []string{}
	for ready := range m.ready.Lock() {
		if _, ok := ready[id]; ok {
			states = append(states, "ready")
		}
	}
	if _, ok := m.dialing[id]; ok {
		states = append(states, "dialing")
	}
	if _, ok := m.upgrading[id]; ok {
		states = append(states, "upgrading")
	}
	if _, ok := m.connected[id]; ok {
		states = append(states, "connected")
	}
	if _, ok := m.evict[id]; ok {
		states = append(states, "evict")
	}
	if _, ok := m.evicting[id]; ok {
		states = append(states, "evicting")
	}
	return strings.Join(states, ",")
}

// findUpgradeCandidate looks for a lower-scored peer that we could evict
// to make room for the given peer. Returns an empty ID if none is found.
// If the peer is already being upgraded to, we return that same upgrade.
// The caller must hold the mutex lock.
func (m *PeerManager) findUpgradeCandidate(id types.NodeID) utils.Option[types.NodeID] {
	score := m.store.Score(id)
	for from, to := range m.upgrading {
		if to == id {
			return utils.Some(from)
		}
	}

	ranked := m.store.Ranked()
	for i := len(ranked) - 1; i >= 0; i-- {
		candidate := ranked[i]
		switch {
		case m.store.Score(candidate.ID) >= score:
			return utils.None[types.NodeID]() // no further peers can be scored lower, due to sorting
		case !m.connected[candidate.ID]:
		case m.evict[candidate.ID] != nil:
		case m.evicting[candidate.ID]:
		case m.upgrading[candidate.ID] != "":
		default:
			return utils.Some(candidate.ID)
		}
	}
	return utils.None[types.NodeID]()
}

// Added for unit test
func (m *PeerManager) MarkReadyConnected(nodeId types.NodeID) {
	for ready, ctrl := range m.ready.Lock() {
		ready[nodeId] = ChannelIDSet{}
		ctrl.Updated()
	}
	m.connected[nodeId] = true
}

// retryDelay calculates a dial retry delay using exponential backoff, based on
// retry settings in PeerManagerOptions. If retries are disabled (i.e.
// MinRetryTime is 0), this returns retryNever (i.e. an infinite retry delay).
func (m *PeerManager) BanPeer(id types.NodeID) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.store.BanPeer(id)
}

func (m *PeerManager) IncrementBlockSyncs(peerID types.NodeID) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.store.IncBlockSyncs(peerID)
}

// Addresses returns all known addresses for a peer, primarily for testing.
// The order is arbitrary.
func (m *PeerManager) Addresses(peerID types.NodeID) []NodeAddress {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.store.Addresses(peerID)
}

// Peers returns all known peers, primarily for testing. The order is arbitrary.
func (m *PeerManager) Peers() []types.NodeID {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.store.Peers()
}

// Scores returns the peer scores for all known peers, primarily for testing.
func (m *PeerManager) Scores() map[types.NodeID]PeerScore {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.store.Scores()
}

func (m *PeerManager) Score(id types.NodeID) int {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return int(m.store.Score(id)) // TODO: it used to be -1 for missing peers.
}

func (m *PeerManager) SendUpdate(pu PeerUpdate) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	switch pu.Status {
	case PeerStatusBad: m.store.UpdateScore(pu.NodeID,-1)
	case PeerStatusGood: m.store.UpdateScore(pu.NodeID,1)
	}
}
