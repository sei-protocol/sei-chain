package p2p

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/im"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "p2p")

type peerConnInfo struct {
	ID       types.NodeID
	Channels ChannelIDSet
	DialAddr utils.Option[NodeAddress]
	SelfAddr utils.Option[NodeAddress]
}

func (i peerConnInfo) connID() connID {
	return connID{NodeID: i.ID, outbound: i.DialAddr.IsPresent()}
}

type peerConn interface {
	comparable
	Info() peerConnInfo
	Close()
}

type connSet[C peerConn] = im.Map[connID, C]

func GetAny[C peerConn](conns connSet[C], id types.NodeID) (C, bool) {
	if c, ok := conns.Get(connID{id, true}); ok {
		return c, true
	}
	return conns.Get(connID{id, false})
}

func GetAll[C peerConn](cs connSet[C], id types.NodeID) []C {
	var out []C
	for _, outbound := range utils.Slice(true, false) {
		if c, ok := cs.Get(connID{id, outbound}); ok {
			out = append(out, c)
		}
	}
	return out
}

type peerManagerInner[C peerConn] struct {
	isPersistent map[types.NodeID]bool
	conns        utils.AtomicSend[connSet[C]]
	regular      *poolManager
	persistent   *poolManager
	lastDialPool *poolManager
}

func (i *peerManagerInner[C]) poolByID(id types.NodeID) *poolManager {
	if i.isPersistent[id] {
		return i.persistent
	}
	return i.regular
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
	selfID          types.NodeID
	options         *RouterOptions
	isBlockSyncPeer map[types.NodeID]bool
	isPrivate       map[types.NodeID]bool

	inner utils.Watch[*peerManagerInner[C]]
	// Receiver of the inner.conns. It is copyable and allows accessing connections
	// without taking lock on inner.
	conns utils.AtomicRecv[connSet[C]]
}

func (p *peerManager[C]) LogState() {
	for inner := range p.inner.Lock() {
		logger.Info("p2p connections",
			"regular", fmt.Sprintf("in=%v/%v + out=%v/%v",
				len(inner.regular.in), inner.regular.cfg.MaxIn,
				len(inner.regular.out), inner.regular.cfg.MaxOut,
			),
			"unconditional", fmt.Sprintf("in=%v + out=%v",
				len(inner.persistent.in),
				len(inner.persistent.out),
			),
		)
	}
}

func newPeerManager[C peerConn](selfID types.NodeID, options *RouterOptions) *peerManager[C] {
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
	var persistentAddrs []NodeAddress
	for _, addr := range options.PersistentPeers {
		if err := addr.Validate(); err != nil {
			logger.Error("invalid persistent peer address", "addr", addr, "err", err)
			continue
		}
		isPersistent[addr.NodeID] = true
		persistentAddrs = append(persistentAddrs, addr)
	}
	var bootstrapAddrs []NodeAddress
	for _, addr := range options.BootstrapPeers {
		if err := addr.Validate(); err != nil {
			logger.Error("invalid bootstrap peer address", "addr", addr, "err", err)
			continue
		}
		if isPersistent[addr.NodeID] {
			persistentAddrs = append(persistentAddrs, addr)
		} else {
			bootstrapAddrs = append(bootstrapAddrs, addr)
		}
	}

	inner := &peerManagerInner[C]{
		isPersistent: isPersistent,
		conns:        utils.NewAtomicSend(im.NewMap[connID, C]()),
		persistent: newPoolManager(&poolConfig{
			MaxIn:      utils.Max[int](),
			MaxOut:     utils.Max[int](),
			FixedAddrs: persistentAddrs,
			InPool: func(id types.NodeID) bool {
				return id != selfID && isPersistent[id]
			},
		}),
		regular: newPoolManager(&poolConfig{
			MaxIn:      options.maxInbound(),
			MaxOut:     options.maxOutbound(),
			FixedAddrs: bootstrapAddrs,
			InPool: func(id types.NodeID) bool {
				return id != selfID && !isPersistent[id]
			},
		}),
	}
	return &peerManager[C]{
		selfID:          selfID,
		options:         options,
		isBlockSyncPeer: isBlockSyncPeer,
		isPrivate:       isPrivate,
		inner:           utils.NewWatch(inner),
		conns:           inner.conns.Subscribe(),
	}
}

func (m *peerManager[C]) Conns() connSet[C] { return m.conns.Load() }

// PushPex registeres address list received from sender in the pex table.
// Address list replaces the previous address list received from that sender
// (every sender has a bounded capacity in peermanager).
// The addresses on the list are expected to be fresh, ideally they should be addresses
// of the current peers of the sender. This property allows us to quickly prune stale
// addresses. PeerManager keeps address list from every connected peer and a small
// "extra" cache for senders which are not connected to facilitate random local search.
// If any of the addresses is invalid (does not parse), the whole slice is rejected.
// Addresses to persistent peers are ignored, since they are populated in constructor.
func (m *peerManager[C]) PushPex(sender utils.Option[types.NodeID], addrs []NodeAddress) error {
	for _, addr := range addrs {
		if err := addr.Validate(); err != nil {
			return err
		}
	}
	for inner, ctrl := range m.inner.Lock() {
		// pex data is indexed by senders which are connected peers.
		// Other pex data is restricted to a small unindexed cache.
		// Therefore we downgrade sender to None, if it is not a connected peer.
		if id, ok := sender.Get(); ok {
			if _, ok := GetAny(inner.conns.Load(), id); !ok {
				sender = utils.None[types.NodeID]()
			}
		}
		inner.regular.PushPex(sender, addrs)
		ctrl.Updated()
	}
	return nil
}

func (m *peerManager[C]) PushUpgradePermit() {
	for inner, ctrl := range m.inner.Lock() {
		if !inner.regular.upgradePermit {
			inner.regular.upgradePermit = true
			ctrl.Updated()
		}
	}
}

// StartDial waits until there is a address available for dialing.
// Returns a collection of addresses known for this peer.
// On success, it marks the peer as dialing and this peer won't be available
// for dialing until DialFailed is called.
func (m *peerManager[C]) StartDial(ctx context.Context) ([]NodeAddress, error) {
	for inner, ctrl := range m.inner.Lock() {
		// Start with pool which has NOT dialed previously (for fairness).
		pools := utils.Slice(inner.persistent, inner.regular)
		if pools[0] == inner.lastDialPool {
			pools[0], pools[1] = pools[1], pools[0]
		}
		for {
			for _, pool := range pools {
				if addrs, ok := pool.TryStartDial(); ok {
					inner.lastDialPool = pool
					ctrl.Updated()
					return addrs, nil
				}
			}
			logger.Info("no addrs available, WAITING")
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}

// DialFailed notifies the peer manager that dialing addresses of id has failed.
func (m *peerManager[C]) DialFailed(id types.NodeID) error {
	for inner, ctrl := range m.inner.Lock() {
		if err := inner.poolByID(id).DialFailed(id); err != nil {
			return err
		}
		ctrl.Updated()
	}
	return nil
}

// Connected adds conn to the connections pool.
// Connected peer won't be available for dialing until disconnect (we don't need duplicate connections).
// May close and drop a duplicate connection already present in the pool.
// Returns an error if the connection should be rejected.
func (m *peerManager[C]) Connected(conn C) error {
	id := conn.Info().connID()
	if id.NodeID == m.selfID {
		conn.Close()
		return fmt.Errorf("connection to self")
	}
	for inner, ctrl := range m.inner.Lock() {
		// Notify the pool.
		pool := inner.poolByID(id.NodeID)
		toDisconnect, err := pool.Connect(id)
		if err != nil {
			conn.Close()
			return err
		}
		// Update the connection set.
		conns := inner.conns.Load()
		// Check if pool requested a disconnect.
		if toDisconnect, ok := toDisconnect.Get(); ok {
			conns.GetOpt(toDisconnect).OrPanic("pool/connection set mismatch").Close()
			conns = conns.Delete(toDisconnect)
		}
		// Insert new connection.
		inner.conns.Store(conns.Set(id, conn))
		ctrl.Updated()
	}
	return nil
}

// Disconnected removes conn from the connection pool.
// Noop if conn was not in the connection pool.
// conn.PeerInfo().NodeID peer is available for dialing again.
func (m *peerManager[C]) Disconnected(conn C) {
	id := conn.Info().connID()
	for inner, ctrl := range m.inner.Lock() {
		// It is fine to call Disconnected for conn which is not present.
		conns := inner.conns.Load()
		if got, ok := conns.Get(id); !ok || conn != got {
			return
		}
		// Notify pool about disconnect.
		// Panic is OK, because inconsistency between conns and pool would be a bug.
		pool := inner.poolByID(id.NodeID)
		utils.OrPanic(pool.Disconnect(id))
		conns = conns.Delete(id)
		if _, ok := GetAny(conns, id.NodeID); !ok {
			inner.regular.ClearPex(id.NodeID)
		}
		inner.conns.Store(conns)
		ctrl.Updated()
	}
}

// Evict closes connection to id.
func (m *peerManager[C]) Evict(id types.NodeID) {
	conns := m.Conns()
	for _, outbound := range utils.Slice(true, false) {
		if c, ok := conns.Get(connID{id, outbound}); ok {
			c.Close()
		}
	}
}

func (m *peerManager[C]) IsBlockSyncPeer(id types.NodeID) bool {
	return len(m.isBlockSyncPeer) == 0 || m.isBlockSyncPeer[id]
}

func (m *peerManager[C]) State(id types.NodeID) string {
	if _, ok := GetAny(m.Conns(), id); ok {
		return "ready,connected"
	}
	for inner := range m.inner.Lock() {
		if _, ok := inner.poolByID(id).dialing[id]; ok {
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
	for id, conn := range m.Conns().All() {
		if m.isPrivate[id.NodeID] {
			continue
		}
		info := conn.Info()
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
	idSet := map[types.NodeID]struct{}{}
	for id := range m.Conns().All() {
		idSet[id.NodeID] = struct{}{}
	}
	ids := make([]types.NodeID, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	return ids
}

// DEPRECATED, currently returns an address iff we are connected to id.
func (m *peerManager[C]) Addresses(id types.NodeID) []NodeAddress {
	if conn, ok := GetAny(m.Conns(), id); ok {
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
