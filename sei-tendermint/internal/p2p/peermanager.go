package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/im"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const maxAddrsPerPeer = 10

type connSet[C peerConn] = im.Map[types.NodeID, C]

type peerAddrs struct {
	lastFail utils.Option[time.Time]
	// Invariants:
	// * fails is a subset of addrs.
	addrs map[NodeAddress]struct{}
	fails map[NodeAddress]time.Time
}

func newPeerAddrs() *peerAddrs {
	return &peerAddrs{
		addrs: map[NodeAddress]struct{}{},
		fails: map[NodeAddress]time.Time{},
	}
}

type peerConnInfo struct {
	ID       types.NodeID
	Channels ChannelIDSet
	DialAddr utils.Option[NodeAddress]
	SelfAddr utils.Option[NodeAddress]
}

type peerConn interface {
	comparable
	Info() peerConnInfo
	Close()
}

type peerManagerInner[C peerConn] struct {
	selfID           types.NodeID
	options          *RouterOptions
	isPersistent     map[types.NodeID]bool
	persistentAddrs  map[types.NodeID]*peerAddrs
	conns            utils.AtomicSend[connSet[C]]
	conditionalConns int // counts conditional connections in conns.
	addrs            map[types.NodeID]*peerAddrs
	dialing          map[types.NodeID]NodeAddress
}

func (i *peerManagerInner[C]) findFailedPeer() (types.NodeID, bool) {
	for old, pa := range i.addrs {
		if len(pa.fails) < len(pa.addrs) {
			continue
		}
		return old, true
	}
	return "", false
}

func (i *peerManagerInner[C]) AddAddr(addr NodeAddress) bool {
	id := addr.NodeID
	// Ignore self.
	if id == i.selfID {
		return false
	}
	// Adding persistent peer addrs is only allowed during initialization.
	// This is to make sure that malicious peers won't cause the preconfigured addrs to be dropped.
	if i.isPersistent[addr.NodeID] {
		return false
	}
	pa, ok := i.addrs[id]
	// Add new peerAddrs if missing.
	if !ok {
		// Prune some peer if maxPeers limit has been reached.
		if len(i.addrs) == i.options.maxPeers() {
			toPrune, ok := i.findFailedPeer()
			if !ok {
				return false
			}
			delete(i.addrs, toPrune)
		}
		pa = newPeerAddrs()
		i.addrs[id] = pa
	}
	// Ignore duplicate address.
	if _, ok := pa.addrs[addr]; ok {
		return false
	}
	// Prune any failing address if maxAddrsPerPeer has been reached.
	if len(pa.addrs) == maxAddrsPerPeer {
		var failedAddr utils.Option[NodeAddress]
		for old := range pa.fails {
			failedAddr = utils.Some(old)
			break
		}
		toPrune, ok := failedAddr.Get()
		if !ok {
			return false
		}
		delete(pa.addrs, toPrune)
		delete(pa.fails, toPrune)
	}
	pa.addrs[addr] = struct{}{}
	return true
}

// None = -inf (i.e. beginning of time)
func before(a, b utils.Option[time.Time]) bool {
	bt, ok := b.Get()
	if !ok {
		return false
	}
	at, ok := a.Get()
	if !ok {
		return true
	}
	return at.Before(bt)
}

func get[K comparable, V any](m map[K]V, k K) utils.Option[V] {
	if v, ok := m[k]; ok {
		return utils.Some(v)
	}
	return utils.None[V]()
}

func (i *peerManagerInner[C]) TryStartDial(persistentPeer bool) (NodeAddress, bool) {
	// Check concurrent dials limit.
	if len(i.dialing) >= i.options.maxDials() {
		return NodeAddress{}, false
	}
	conns := i.conns.Load()
	// Check max connections limit (unless we are dialing a persistent peer).
	if !persistentPeer && len(i.dialing)+conns.Len() >= i.options.maxConns() {
		return NodeAddress{}, false
	}
	// Choose peer with the oldest lastFail.
	var bestPeer utils.Option[*peerAddrs]
	addrs := i.addrs
	if persistentPeer {
		addrs = i.persistentAddrs
	}
	for id, peerAddrs := range addrs {
		if _, ok := i.dialing[id]; ok {
			continue
		}
		if _, ok := conns.Get(id); ok {
			continue
		}
		if x, ok := bestPeer.Get(); !ok || before(peerAddrs.lastFail, x.lastFail) {
			bestPeer = utils.Some(peerAddrs)
		}
	}
	// Choose address with the oldest lastFail.
	var best utils.Option[NodeAddress]
	if peer, ok := bestPeer.Get(); ok {
		for addr := range peer.addrs {
			if x, ok := best.Get(); !ok || before(get(peer.fails, addr), get(peer.fails, x)) {
				best = utils.Some(addr)
			}
		}
	}
	if x, ok := best.Get(); ok {
		// clear the failed status for the chosen address and mark it as dialing.
		delete(addrs[x.NodeID].fails, x)
		i.dialing[x.NodeID] = x
	}
	return best.Get()
}

func (i *peerManagerInner[C]) DialFailed(addr NodeAddress) {
	if i.dialing[addr.NodeID] == addr {
		delete(i.dialing, addr.NodeID)
	}
	var peerAddrs *peerAddrs
	var ok bool
	if i.isPersistent[addr.NodeID] {
		peerAddrs, ok = i.persistentAddrs[addr.NodeID]
	} else {
		peerAddrs, ok = i.addrs[addr.NodeID]
	}
	if !ok {
		return
	}
	// Record the failure time.
	now := time.Now()
	peerAddrs.lastFail = utils.Some(now)
	if _, ok := peerAddrs.addrs[addr]; ok {
		peerAddrs.fails[addr] = now
	}
}

func (i *peerManagerInner[C]) Evict(id types.NodeID) {
	if !i.isPersistent[id] {
		delete(i.addrs, id)
		if conn, ok := i.conns.Load().Get(id); ok {
			conn.Close()
		}
	}
}

// Connected registers a new connection.
// If it is an outbound connection the dialing status is cleared (EVEN IF IT RETURNS AN ERROR).
// It atomically checks maximum connection limit and duplicate connections.
func (i *peerManagerInner[C]) Connected(conn C) (err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	info := conn.Info()
	if addr, ok := info.DialAddr.Get(); ok && i.dialing[addr.NodeID] == addr {
		delete(i.dialing, addr.NodeID)
	}
	peerID := info.ID
	if peerID == i.selfID {
		return errors.New("connection to self")
	}
	conns := i.conns.Load()
	if old, ok := conns.Get(peerID); ok {
		// * allow to override connections in the same direction.
		// * allow inbound conn to override outbound iff peerID > selfID.
		//   This resolves the situation when peers try to connect to each other
		//   at the same time.
		oldDirection := old.Info().DialAddr.IsPresent()
		newDirection := info.DialAddr.IsPresent()
		if oldDirection != newDirection && (peerID < i.selfID) != newDirection {
			return fmt.Errorf("duplicate connection from peer %q", peerID)
		}
		old.Close()
		i.conns.Store(conns.Set(peerID, conn))
		return nil
	}
	if !i.isPersistent[peerID] {
		if i.conditionalConns >= i.options.maxConns() {
			return errors.New("too many connections")
		}
		i.conditionalConns += 1
	}
	i.conns.Store(conns.Set(peerID, conn))
	return nil
}

func (i *peerManagerInner[C]) Disconnected(conn C) {
	peerID := conn.Info().ID
	conns := i.conns.Load()
	if old, ok := conns.Get(peerID); ok && old == conn {
		if !i.isPersistent[peerID] {
			i.conditionalConns -= 1
		}
		old.Close()
		i.conns.Store(conns.Delete(peerID))
	}
}

// PeerManager manages connections and addresses of potential peers.
// PeerManager may trigger disconnects by calling conn.Close() in case of conflicting connections.
// Possible lifecycles of the connection are as follows:
// For outbound connections:
// * StartDial() -> [dialing] -> Connected(conn) -> [communicate] -> Disconnected(conn)
// * StartDial() -> [dialing] -> DialFailed(addr)
// For inbound connections:
// * Connected(conn) -> [communicate] -> Disconnected(conn)
// For adding new peer addrs, call AddAddrs().
type peerManager[C peerConn] struct {
	options         *RouterOptions
	isBlockSyncPeer map[types.NodeID]bool
	isPrivate       map[types.NodeID]bool
	// Receiver of the inner.conns. It is copyable and allows accessing connections
	// without taking lock on inner.
	conns utils.AtomicRecv[connSet[C]]
	inner utils.Watch[*peerManagerInner[C]]
}

func (p *peerManager[C]) LogState(logger log.Logger) {
	for inner := range p.inner.Lock() {
		logger.Info("p2p connections",
			"regular", fmt.Sprintf("%v/%v", inner.conditionalConns, p.options.maxConns()),
			"unconditional", inner.conns.Load().Len()-inner.conditionalConns,
		)
	}
}

// PeerUpdatesRecv.
// NOT THREAD-SAFE.
type peerUpdatesRecv[C peerConn] struct {
	recv utils.AtomicRecv[connSet[C]]
	last map[types.NodeID]struct{}
}

// PeerUpdate is a peer update event sent via PeerUpdates.
type PeerUpdate struct {
	NodeID   types.NodeID
	Status   PeerStatus
	Channels ChannelIDSet
}

func (s *peerUpdatesRecv[C]) Recv(ctx context.Context) (PeerUpdate, error) {
	var update PeerUpdate
	_, err := s.recv.Wait(ctx, func(conns connSet[C]) bool {
		// Check for disconnected peers.
		for id := range s.last {
			if _, ok := conns.Get(id); !ok {
				delete(s.last, id)
				update = PeerUpdate{
					NodeID: id,
					Status: PeerStatusDown,
				}
				return true
			}
		}
		// Check for connected peers.
		for id, conn := range conns.All() {
			if _, ok := s.last[id]; !ok {
				s.last[id] = struct{}{}
				update = PeerUpdate{
					NodeID:   id,
					Status:   PeerStatusUp,
					Channels: conn.Info().Channels,
				}
				return true
			}
		}
		return false
	})
	return update, err
}

func (m *peerManager[C]) Subscribe() *peerUpdatesRecv[C] {
	return &peerUpdatesRecv[C]{
		recv: m.conns,
		last: map[types.NodeID]struct{}{},
	}
}

func newPeerManager[C peerConn](selfID types.NodeID, options *RouterOptions) *peerManager[C] {
	inner := &peerManagerInner[C]{
		selfID:          selfID,
		options:         options,
		persistentAddrs: map[types.NodeID]*peerAddrs{},
		isPersistent:    map[types.NodeID]bool{},

		conns:   utils.NewAtomicSend(im.NewMap[types.NodeID, C]()),
		addrs:   map[types.NodeID]*peerAddrs{},
		dialing: map[types.NodeID]NodeAddress{},
	}
	isBlockSyncPeer := map[types.NodeID]bool{}
	isPrivate := map[types.NodeID]bool{}
	for _, id := range options.PrivatePeers {
		isPrivate[id] = true
	}
	for _, id := range options.UnconditionalPeers {
		inner.isPersistent[id] = true
	}
	for _, id := range options.BlockSyncPeers {
		inner.isPersistent[id] = true
		isBlockSyncPeer[id] = true
	}
	for _, addr := range options.PersistentPeers {
		inner.isPersistent[addr.NodeID] = true
		if _, ok := inner.persistentAddrs[addr.NodeID]; !ok {
			inner.persistentAddrs[addr.NodeID] = newPeerAddrs()
		}
		inner.persistentAddrs[addr.NodeID].addrs[addr] = struct{}{}
	}
	for _, addr := range options.BootstrapPeers {
		inner.AddAddr(addr)
	}
	return &peerManager[C]{
		options:         options,
		isBlockSyncPeer: isBlockSyncPeer,
		isPrivate:       isPrivate,
		conns:           inner.conns.Subscribe(),
		inner:           utils.NewWatch(inner),
	}
}

func (m *peerManager[C]) Conns() connSet[C] {
	return m.conns.Load()
}

// AddAddrs adds addresses, so that they are available for dialing.
// Addresses to persistent peers are ignored, since they are populated in constructor.
// Known addresses are ignored.
// If maxAddrsPerPeer limit is exceeded, new address replaces a random failed address of that peer.
// If options.MaxPeers limit is exceeded, some peer with ALL addresses failed is replaced.
// If there is no such address/peer to replace, the new address is ignored.
// If some address is invalid, an error is returned.
// Even if an error is returned, some addresses might have been added.
func (m *peerManager[C]) AddAddrs(addrs []NodeAddress) error {
	for inner, ctrl := range m.inner.Lock() {
		for _, addr := range addrs {
			if err := addr.Validate(); err != nil {
				return err
			}
			if inner.AddAddr(addr) {
				ctrl.Updated()
			}
		}
	}
	return nil
}

// StartDial waits until there is a (persistent/non-persistent) address available for dialing.
// On success, it marks the peer as dialing - peer won't be available for dialing until DialFailed
// is called.
func (m *peerManager[C]) StartDial(ctx context.Context, persistentPeer bool) (NodeAddress, error) {
	for inner, ctrl := range m.inner.Lock() {
		for {
			if addr, ok := inner.TryStartDial(persistentPeer); ok {
				return addr, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return NodeAddress{}, err
			}
		}
	}
	panic("unreachable")
}

// DialFailed marks the address as "failed to dial".
// The addr.NodeID peer will be added back to the pool of peers
// available for dialing.
func (p *peerManager[C]) DialFailed(addr NodeAddress) {
	for inner, ctrl := range p.inner.Lock() {
		inner.DialFailed(addr)
		ctrl.Updated()
	}
}

// Connected adds conn to the connections pool.
// Connected peer won't be available for dialing until disconnect (we don't need duplicate connections).
// May close and drop a duplicate connection already present in the pool.
// Returns an error if the connection should be rejected.
func (m *peerManager[C]) Connected(conn C) error {
	for inner, ctrl := range m.inner.Lock() {
		ctrl.Updated()
		return inner.Connected(conn)
	}
	panic("unreachable")
}

// Disconnected removes conn from the connection pool.
// Noop if conn was not in the connection pool.
// conn.PeerInfo().NodeID peer is available for dialing again.
func (m *peerManager[C]) Disconnected(conn C) {
	for inner, ctrl := range m.inner.Lock() {
		inner.Disconnected(conn)
		ctrl.Updated()
	}
}

// Evict removes known addresses of the peer (if not persistent) and
// closes and drops connection to the peer (if not unconditional).
func (m *peerManager[C]) Evict(id types.NodeID) {
	for inner, ctrl := range m.inner.Lock() {
		inner.Evict(id)
		ctrl.Updated()
	}
}

func (m *peerManager[C]) IsBlockSyncPeer(id types.NodeID) bool {
	return len(m.isBlockSyncPeer) == 0 || m.isBlockSyncPeer[id]
}

func (m *peerManager[C]) State(id types.NodeID) string {
	for inner := range m.inner.Lock() {
		if _, ok := inner.conns.Load().Get(id); ok {
			return "ready,connected"
		}
		if _, ok := inner.dialing[id]; ok {
			return "dialing"
		}
	}
	return ""
}

func (m *peerManager[C]) Advertise() []NodeAddress {
	var addrs []NodeAddress
	// Advertise your own address.
	if addr, ok := m.options.SelfAddress.Get(); ok {
		addrs = append(addrs, addr)
	}
	var selfAddrs []NodeAddress
	conns := m.conns.Load()
	for _, conn := range conns.All() {
		info := conn.Info()
		if m.isPrivate[info.ID] {
			continue
		}
		if addr, ok := info.DialAddr.Get(); ok {
			// Prioritize dialed addresses of outbound connections.
			addrs = append(addrs, addr)
		} else if addr, ok := info.SelfAddr.Get(); ok {
			// Fallback to self-declared addresses of inbound connections.
			selfAddrs = append(selfAddrs,addr)
		}
	}
	return append(addrs, selfAddrs...)
}

func (m *peerManager[C]) Peers() []types.NodeID {
	var ids []types.NodeID
	for inner := range m.inner.Lock() {
		ids = make([]types.NodeID, 0, len(inner.persistentAddrs)+len(inner.addrs))
		for id := range inner.persistentAddrs {
			ids = append(ids, id)
		}
		for id := range inner.addrs {
			ids = append(ids, id)
		}
	}
	return ids
}

func (m *peerManager[C]) Addresses(id types.NodeID) []NodeAddress {
	var addrs []NodeAddress
	for inner := range m.inner.Lock() {
		peerAddrs := inner.addrs
		if inner.isPersistent[id] {
			peerAddrs = inner.persistentAddrs
		}
		if pa, ok := peerAddrs[id]; ok {
			for addr := range pa.addrs {
				addrs = append(addrs, addr)
			}
		}
	}
	return addrs
}
