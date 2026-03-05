package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/time/rate"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/im"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type connSet[C peerConn] = im.Map[types.NodeID, C]

var errPersistentPeerAddr = errors.New("cannot add a persistent peer address to the regular address pool")

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
	logger          log.Logger
	options         *RouterOptions
	isBlockSyncPeer map[types.NodeID]bool
	isPrivate       map[types.NodeID]bool
	isPersistent    map[types.NodeID]bool
	conns           utils.Mutex[*utils.AtomicSend[connSet[C]]]
	regular         *pool[C]
	persistent      *pool[C]
	pexAddrs        utils.Watch[*pexTable]
}

// TODO: check if router will behave sanely with duplicate connections.
// Note that the only tasks notified about ANY change to pools status are the dialing tasks.
// TODO: make peerManager push dialQueue to regular pool AFTER filtering out persistent peers:
// * filterByID
// Keeping pexAddrs outside of Conns seems highly undesirable.
// * move bySender pexAddrs to peerConn
// * make connSet contain ALL connections (somehow)
// * essentialy merge pexTable with connSet.
// * StartDial() should return errDialQueueEmpty, which should trigger repopulating the queue.
// * except for StartDial, pool is nonblocking, so perhaps we can move StartDial outside.
// * pools and conns should be under the same mutex,
//   so that updates to pool and conns are consistent.

func (p *peerManager[C]) LogState() {
	for inner := range p.inner.Lock() {
		p.logger.Info("p2p connections",
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

func newPeerManager[C peerConn](logger log.Logger, selfID types.NodeID, options *RouterOptions, initialAddrs []NodeAddress) *peerManager[C] {
	isBlockSyncPeer := map[types.NodeID]bool{}
	isPrivate := map[types.NodeID]bool{}
	isPersistent := map[types.NodeID]bool{}
	for _, id := range options.PrivatePeers {
		isPrivate[id] = true
	}
	for _, id := range options.UnconditionalPeers {
		isPersistent[id] = true
	}
	for _, id := range options.BlockSyncPeers {
		isPersistent[id] = true
		isBlockSyncPeer[id] = true
	}
	// We do not allow multiple addresses for the same peer in the peer manager any more.
	// It would be backward incompatible to invalidate configs with multiple addresses per peer.
	// Instead we just log an error to indicate that some addresses have been ignored.
	for _, addr := range options.PersistentPeers {
		isPersistent[addr.NodeID] = true
		if err := inner.persistent.AddAddr(addr); err != nil {
			logger.Error("failed to add a persistent peer address to the pool", "addr", addr, "err", err)
		}
	}
	for _, addr := range options.BootstrapPeers {
		if err := inner.AddAddr(addr); err != nil {
			logger.Error("failed to add a bootstrap peer address to the pool", "addr", addr, "err", err)
		}
	}
	return &peerManager[C]{
		logger:          logger,
		options:         options,
		isBlockSyncPeer: isBlockSyncPeer,
		isPrivate:       isPrivate,
		isPersistent: isPersistent,
		conns:        utils.NewAtomicSend(im.NewMap[types.NodeID, C]()),
		
		persistent: newPool[C](&poolConfig{
			SelfID: selfID,
			MaxIn: utils.Max[int](),
			MaxOut: utils.Max[int](),
			MaxDials: options.maxDials(),
			UpgradeRate: 0,
		}),
		regular: newPool[C](&poolConfig{
			SelfID:   selfID,
			// TODO: this needs more precision
			MaxIn: options.maxConns(),
			MaxOut: options.maxOutboundConns(),
			MaxDials: options.maxDials(),
			UpgradeRate: rate.Every(time.Minute),
		}),

		pexAddrs: utils.NewWatch(&pexTable{
			initial: initialAddrs,
			bootstrap: options.BootstrapPeers,
		}),
	}
}

func (m *peerManager[C]) Conns() connSet[C] {
	for conns := range m.conns.Lock() {
		return conns.Load()
	}
	panic("unreachable")
}

// AddAddrs adds addresses, so that they are available for dialing.
// Addresses to persistent peers are ignored, since they are populated in constructor.
// Known addresses are ignored.
// If maxAddrsPerPeer limit is exceeded, new address replaces a random failed address of that peer.
// If options.MaxPeers limit is exceeded, some peer with ALL addresses failed is replaced.
// If there is no such address/peer to replace, the new address is ignored.
// If some address is invalid, an error is returned.
// Even if an error is returned, some addresses might have been added.
func (m *peerManager[C]) AddAddrs(sender types.NodeID, addrs []NodeAddress) error {
	for _, addr := range addrs {
		if err := addr.Validate(); err != nil {
			return err
		}
	}
	for pexAddrs,ctrl := range m.pexAddrs.Lock() {
		pexAddrs.bySender[sender] = addrs
		ctrl.Updated()
	}
	return nil
}

func (p *peerManager[C]) clearPexAddrs(id types.NodeID) {
	const maxClearedCache = 10
	for pexAddrs := range p.pexAddrs.Lock() {
		addrs,ok := pexAddrs.bySender[id]
		if !ok { return }
		delete(pexAddrs.bySender,id)
		if len(pexAddrs.cleared) < maxClearedCache {
			pexAddrs.cleared = append(pexAddrs.cleared, addrs)
		}
	}
}

// StartDial waits until there is a (persistent/non-persistent) address available for dialing.
// On success, it marks the peer as dialing - peer won't be available for dialing until DialFailed
// is called.
func (m *peerManager[C]) StartDial(ctx context.Context, persistentPeer bool) ([]NodeAddress, error) {
	if persistentPeer {
		return m.persistent.StartDial(ctx)
	} else {
		return m.regular.StartDial(ctx)
	}
}

// DialFailed notifies the peer manager that dialing addresses of id has failed.
func (m *peerManager[C]) DialFailed(id types.NodeID) {
	if m.isPersistent[id] {
		m.persistent.DialFailed(id)
	} else {
		m.regular.DialFailed(id)
	}
}

// updateConns updates the total connection set, based on the pools state.
func (m *peerManager[C]) updateConns(id types.NodeID) {
	for conns := range m.conns.Lock() {
		oldConns := conns.Load()
		var newConns []C
		if m.isPersistent[id] {
			newConns = m.persistent.Get(id)
		} else {
			newConns = m.regular.Get(id)
		}
		var newConn utils.Option[C]
		if len(newConns)>0 {
			newConn = utils.Some(newConns[0])
		}
		if oldConns.GetOpt(id)!=newConn {
			conns.Store(oldConns.SetOpt(id,newConn))
			if !newConn.IsPresent() {
				m.clearPexAddrs(id)
			}
		}
	}
}

// Connected adds conn to the connections pool.
// Connected peer won't be available for dialing until disconnect (we don't need duplicate connections).
// May close and drop a duplicate connection already present in the pool.
// Returns an error if the connection should be rejected.
func (m *peerManager[C]) Connected(conn C) error {
	info := conn.Info()
	if m.isPersistent[info.ID] {
		return m.persistent.Connected(conn)
	} else {
		return m.regular.Connected(conn)
	}
}

// Disconnected removes conn from the connection pool.
// Noop if conn was not in the connection pool.
// conn.PeerInfo().NodeID peer is available for dialing again.
func (m *peerManager[C]) Disconnected(conn C) {
	info := conn.Info()
	if m.isPersistent[info.ID] {
		m.persistent.Disconnected(conn)
	} else {
		m.regular.Disconnected(conn)
	}
}

// Evict closes connection to id (unless it was a persistent peer).
func (m *peerManager[C]) Evict(id types.NodeID) {
	for _,conn := range m.regular.Get(id) {
		conn.Close()
	}
}

func (m *peerManager[C]) IsBlockSyncPeer(id types.NodeID) bool {
	return len(m.isBlockSyncPeer) == 0 || m.isBlockSyncPeer[id]
}

func (m *peerManager[C]) State(id types.NodeID) string {
	if m.isPersistent[id] {
		return m.persistent.State(id)
	} else {
		return m.regular.State(id)
	}
}

func (m *peerManager[C]) Advertise() []NodeAddress {
	var addrs []NodeAddress
	// Advertise your own address.
	if addr, ok := m.options.SelfAddress.Get(); ok {
		addrs = append(addrs, addr)
	}
	var selfAddrs []NodeAddress
	for _, conn := range m.Conns().All() {
		info := conn.Info()
		if m.isPrivate[info.ID] {
			continue
		}
		if addr, ok := info.DialAddr.Get(); ok {
			// Prioritize dialed addresses of outbound connections.
			addrs = append(addrs, addr)
		} else if addr, ok := info.SelfAddr.Get(); ok {
			// Fallback to self-declared addresses of inbound connections.
			selfAddrs = append(selfAddrs, addr)
		}
	}
	return append(addrs, selfAddrs...)
}

// DEPRECATED, currently returns id of peers that we are connected to.
func (m *peerManager[C]) Peers() []types.NodeID {
	var ids []types.NodeID
	for id,_ := range m.Conns().All() {
		ids = append(ids, id)
	}
	return ids
}

// DEPRECATED, currently returns an address iff we are connected to id.
func (m *peerManager[C]) Addresses(id types.NodeID) []NodeAddress {
	if conn,ok := m.Conns().Get(id); ok {
		info := conn.Info()
		if addr, ok := info.DialAddr.Get(); ok {
			// Prioritize dialed addresses of outbound connections.
			return utils.Slice(addr)	
		} else if addr, ok := info.SelfAddr.Get(); ok {
			// Fallback to self-declared addresses of inbound connections.
			return utils.Slice(addr)
		}
	}
	return nil
}
