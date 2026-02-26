package p2p

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/im"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const maxAddrsPerPeer = 10

type connSet[C peerConn] = im.Map[types.NodeID, C]

type peerManagerInner[C peerConn] struct {
	selfID       types.NodeID
	options      *RouterOptions
	isPersistent map[types.NodeID]bool
	// sum of regular and persistent connection sets.
	conns utils.AtomicSend[connSet[C]]

	regular    *pool[C]
	persistent *pool[C]
}

func (i *peerManagerInner[C]) AddAddr(addr NodeAddress) bool {
	// Adding persistent peer addrs is only allowed during initialization.
	// This is to make sure that malicious peers won't cause the preconfigured addrs to be dropped.
	if i.isPersistent[addr.NodeID] {
		return false
	}
	return i.regular.AddAddr(addr)
}

func (i *peerManagerInner[C]) TryStartDial(persistentPeer bool) (NodeAddress, bool) {
	// Check concurrent dials limit.
	if len(i.regular.dialing)+len(i.persistent.dialing) >= i.options.maxDials() {
		return NodeAddress{}, false
	}
	if persistentPeer {
		return i.persistent.TryStartDial()
	}
	// Regular peers are additionally subject to outbound connections limit.
	// We should not dial if it would result in too many outbound connections.
	if len(i.regular.dialing)+i.regular.outbound >= i.options.maxOutboundConns() {
		return NodeAddress{}, false
	}
	return i.regular.TryStartDial()
}

func (i *peerManagerInner[C]) DialFailed(addr NodeAddress) {
	if i.isPersistent[addr.NodeID] {
		i.persistent.DialFailed(addr)
	} else {
		i.regular.DialFailed(addr)
	}
}

func (i *peerManagerInner[C]) Evict(id types.NodeID) {
	if !i.isPersistent[id] {
		i.regular.Evict(id)
	}
}

// Connected registers a new connection.
// If it is an outbound connection the dialing status is cleared (EVEN IF IT RETURNS AN ERROR).
func (i *peerManagerInner[C]) Connected(conn C) error {
	info := conn.Info()
	pool := i.regular
	if i.isPersistent[info.ID] {
		pool = i.persistent
	}
	err := pool.Connected(conn)
	// Copy the update to the total connection pool.
	conns := i.conns.Load()
	if got, want := conns.GetOpt(info.ID), getOpt(pool.conns, info.ID); got != want {
		i.conns.Store(conns.SetOpt(info.ID, want))
	}
	return err
}

func (i *peerManagerInner[C]) Disconnected(conn C) {
	info := conn.Info()
	pool := i.regular
	if i.isPersistent[info.ID] {
		pool = i.persistent
	}
	pool.Disconnected(conn)
	// Copy the update to the total connection pool.
	conns := i.conns.Load()
	if got, want := conns.GetOpt(info.ID), getOpt(pool.conns, info.ID); got != want {
		i.conns.Store(conns.SetOpt(info.ID, want))
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
			"regular", fmt.Sprintf("%v/%v", len(inner.regular.conns), p.options.maxConns()),
			"unconditional", len(inner.persistent.conns),
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
		selfID:       selfID,
		options:      options,
		isPersistent: map[types.NodeID]bool{},
		conns:        utils.NewAtomicSend(im.NewMap[types.NodeID, C]()),

		persistent: newPool[C](poolConfig{selfID: selfID}),
		regular: newPool[C](poolConfig{
			selfID:   selfID,
			maxConns: utils.Some(options.maxConns()),
			maxAddrs: utils.Some(options.maxPeers()),
		}),
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
		inner.persistent.AddAddr(addr)
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
	if len(addrs) == 0 {
		return nil
	}
	for _, addr := range addrs {
		if err := addr.Validate(); err != nil {
			return err
		}
	}
	for inner, ctrl := range m.inner.Lock() {
		updated := false
		for _, addr := range addrs {
			if inner.AddAddr(addr) {
				updated = true
			}
		}
		if updated {
			ctrl.Updated()
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

// Evict removes known addresses of the regular peer and closed connection to the regular peer.
// NOTE: noop for persistent peers.
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
		if inner.isPersistent[id] {
			if _, ok := inner.persistent.dialing[id]; ok {
				return "dialing"
			}
		} else {
			if _, ok := inner.regular.dialing[id]; ok {
				return "dialing"
			}
		}
	}
	return ""
}

func (m *peerManager[C]) Advertise(maxAddrs int) []NodeAddress {
	if maxAddrs <= 0 {
		return nil
	}
	var addrs []NodeAddress
	if selfAddr, ok := m.options.SelfAddress.Get(); ok {
		addrs = append(addrs, selfAddr)
	}
	for _, conn := range m.conns.Load().All() {
		if len(addrs) >= maxAddrs {
			break
		}
		if addr, ok := conn.Info().DialAddr.Get(); ok && !m.isPrivate[addr.NodeID] {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

func (m *peerManager[C]) Peers() []types.NodeID {
	var ids []types.NodeID
	for inner := range m.inner.Lock() {
		ids = make([]types.NodeID, 0, len(inner.persistent.addrs)+len(inner.regular.addrs))
		for id := range inner.persistent.addrs {
			ids = append(ids, id)
		}
		for id := range inner.regular.addrs {
			ids = append(ids, id)
		}
	}
	return ids
}

func (m *peerManager[C]) Addresses(id types.NodeID) []NodeAddress {
	var addrs []NodeAddress
	for inner := range m.inner.Lock() {
		for _, pool := range utils.Slice(inner.persistent, inner.regular) {
			if pa, ok := pool.addrs[id]; ok {
				addrs = append(addrs, pa.addr)
			}
		}
	}
	return addrs
}
