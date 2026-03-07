package p2p

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/im"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type connSet[C peerConn] = im.Map[connID, C]

func GetAny[C peerConn](cs connSet[C], id types.NodeID) (C,bool) {
	if c,ok := cs.Get(connID{id,true}); ok { return c,true }
	return cs.Get(connID{id,false})
}

type peerManagerInner[C peerConn] struct {
	isPersistent map[types.NodeID]bool
	conns utils.AtomicSend[connSet[C]]
	regular *poolManager
	persistent *poolManager
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
	selfID types.NodeID
	logger          log.Logger
	options         *RouterOptions
	isBlockSyncPeer map[types.NodeID]bool
	isPrivate       map[types.NodeID]bool

	inner           utils.Watch[*peerManagerInner[C]]
	conns           utils.AtomicRecv[connSet[C]]
}

func (p *peerManager[C]) LogState() {
	for inner := range p.inner.Lock() {
		p.logger.Info("p2p connections",
			"regular", fmt.Sprintf("in=%v/%v + out=%v/%v",
				len(inner.regular.in), inner.regular.cfg.MaxIn,
				inner.regular.out.Len(), inner.regular.cfg.MaxOut,
			),
			"unconditional", fmt.Sprintf("in=%v + out=%v", 
				len(inner.persistent.in),
				inner.persistent.out.Len(),
			),
		)
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
		if err := addr.Validate(); err != nil {
			logger.Error("invalid persistent peer address", "addr", addr, "err", err)
		}
	}
	for _, addr := range options.BootstrapPeers {
		if err := addr.Validate(); err != nil {
			logger.Error("invalid bootstrap peer address", "addr", addr, "err", err)
		}
	}

	inner := &peerManagerInner[C] {
		isPersistent: isPersistent,
		conns: utils.NewAtomicSend(im.NewMap[connID,C]()),
		persistent: newPoolManager(&poolConfig{
			MaxIn: utils.Max[int](),
			MaxOut: utils.Max[int](),
			MaxDials: options.maxDials(),
			InPool: func(id types.NodeID) bool {
				return id!=selfID && isPersistent[id]
			},
		}),
		regular: newPoolManager(&poolConfig{
			// TODO: this needs more precision
			MaxIn: options.maxConns(),
			MaxOut: options.maxOutboundConns(),
			MaxDials: options.maxDials(),
			InPool: func(id types.NodeID) bool {
				return id!=selfID && !isPersistent[id]
			},
		}),
	}
	inner.regular.PushExtraPex(initialAddrs)
	return &peerManager[C]{
		selfID: selfID,
		logger:          logger,
		options:         options,
		isBlockSyncPeer: isBlockSyncPeer,
		isPrivate:       isPrivate,
		inner: utils.NewWatch(inner),
		conns: inner.conns.Subscribe(),
	}
}

func (m *peerManager[C]) Conns() connSet[C] { return m.conns.Load() }

// AddAddrs adds addresses, so that they are available for dialing.
// Addresses to persistent peers are ignored, since they are populated in constructor.
// Known addresses are ignored.
// If maxAddrsPerPeer limit is exceeded, new address replaces a random failed address of that peer.
// If options.MaxPeers limit is exceeded, some peer with ALL addresses failed is replaced.
// If there is no such address/peer to replace, the new address is ignored.
// If some address is invalid, an error is returned.
// Even if an error is returned, some addresses might have been added.
func (m *peerManager[C]) PushPex(sender types.NodeID, addrs []NodeAddress) error {
	for _, addr := range addrs {
		if err := addr.Validate(); err != nil {
			return err
		}
	}
	for inner,ctrl := range m.inner.Lock() {
		// pex data is indexed by senders which are connected peers.
		// Other pex data is restricted to a small unindexed cache.
		var msender utils.Option[types.NodeID]
		if _,ok := GetAny(inner.conns.Load(), sender); ok {
			msender = utils.Some(sender)
		}
		inner.regular.PushPex(msender,addrs)
		ctrl.Updated()
	}
	return nil
}

// StartDial waits until there is a address available for dialing.
// Returns a collection of addresses known for this peer.
// On success, it marks the peer as dialing and this peer won't be available
// for dialing until DialFailed is called.
func (m *peerManager[C]) StartDial(ctx context.Context) ([]NodeAddress, error) {
	for inner,ctrl := range m.inner.Lock() {
		// Start with pool which has NOT dialed previously (for fairness).
		pools := utils.Slice(inner.persistent,inner.regular)
		if pools[0]==inner.lastDialPool {
			pools[0],pools[1] = pools[1],pools[0]
		}
		for {
			for _,pool := range pools {
				if addrs,ok := pool.StartDial(); ok {
					inner.lastDialPool = pool 
					ctrl.Updated()
					return addrs,nil
				}
			}
			if err:=ctrl.Wait(ctx); err!=nil { return nil,err }
		}
	}
	panic("unreachable")
}

// DialFailed notifies the peer manager that dialing addresses of id has failed.
func (m *peerManager[C]) DialFailed(id types.NodeID) error {
	for inner,ctrl := range m.inner.Lock() {
		if err:=inner.poolByID(id).DialFailed(id); err!=nil {
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
	info := conn.Info()
	if info.ID==m.selfID {
		conn.Close()
		return fmt.Errorf("connection to self")
	}
	id := connID{NodeID:info.ID,outbound:info.DialAddr.IsPresent()}
	for inner,ctrl := range m.inner.Lock() {
		// Notify the pool.
		pool := inner.poolByID(id.NodeID)
		toDisconnect,err:=pool.Connect(id)
		if err!=nil {
			conn.Close()
			return err
		}
		// Update the connection set.
		conns := inner.conns.Load()
		// Check if pool requested a disconnect.
		if toDisconnect,ok := toDisconnect.Get(); ok {
			conns.GetOpt(toDisconnect).OrPanic("pool/connection set mismatch").Close()
			conns = conns.Delete(toDisconnect)
		}
		// Insert new connection.
		inner.conns.Store(conns.Set(id,conn))
		ctrl.Updated()
	}
	return nil
}

// Disconnected removes conn from the connection pool.
// Noop if conn was not in the connection pool.
// conn.PeerInfo().NodeID peer is available for dialing again.
func (m *peerManager[C]) Disconnected(conn C) {
	info := conn.Info()
	id := connID{NodeID:info.ID,outbound:info.DialAddr.IsPresent()}
	for inner,ctrl := range m.inner.Lock() {
		// It is fine to call Disconnected for conn which is not present.
		conns := inner.conns.Load()
		if got,ok := conns.Get(id); !ok || conn!=got { return }
		// Notify pool about disconnect.
		// Panic is OK, because inconsistency between conns and pool would be a bug.
		pool := inner.poolByID(id.NodeID)
		utils.OrPanic(pool.Disconnect(id))
		conns = conns.Delete(id)
		if _,ok := GetAny(conns,id.NodeID); !ok {
			inner.regular.ClearPex(id.NodeID)	
		}
		inner.conns.Store(conns)
		ctrl.Updated()
	}
}

// Evict closes connection to id.
func (m *peerManager[C]) Evict(id types.NodeID) {
	conns := m.Conns()
	for _, outbound := range utils.Slice(true,false) {
		if c,ok := conns.Get(connID{id,outbound}); ok {
			c.Close()
		}
	}
}

func (m *peerManager[C]) IsBlockSyncPeer(id types.NodeID) bool {
	return len(m.isBlockSyncPeer) == 0 || m.isBlockSyncPeer[id]
}

func (m *peerManager[C]) State(id types.NodeID) string {
	if _,ok := GetAny(m.Conns(),id); ok {
		return "ready,connected"
	}
	for inner := range m.inner.Lock() {
		if _,ok := inner.poolByID(id).dialing[id]; ok {
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
		if m.isPrivate[id.NodeID] { continue }
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

func (p *peerManager[C]) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Task feeding the upgrade permit to the regular peers pool.
		s.Spawn(func() error {
			const upgradeInterval = time.Minute
			for {
				for inner,ctrl := range p.inner.Lock() {
					if !inner.regular.upgradePermit {
						inner.regular.upgradePermit = true
						ctrl.Updated()
					}
				}
				if err := utils.Sleep(ctx,upgradeInterval); err!=nil { return err }
			}
		})

		// Task feeding hardcoded addresses periodically to pools.
		const feedInterval = 10 * time.Second
		for {
			for inner,ctrl := range p.inner.Lock() {
				inner.regular.PushPex(p.selfID,p.options.BootstrapPeers)
				inner.persistent.PushPex(p.selfID,p.options.PersistentPeers)
				ctrl.Updated()
			}
			if err:=utils.Sleep(ctx,feedInterval); err!=nil {
				return err
			}
		}
	})
}

// DEPRECATED, currently returns id of peers that we are connected to.
func (m *peerManager[C]) Peers() []types.NodeID {
	idSet := map[types.NodeID]struct{}{}
	for id,_ := range m.Conns().All() {
		idSet[id.NodeID] = struct{}{}
	}
	ids := make([]types.NodeID,0,len(idSet))
	for id := range idSet {
		ids = append(ids,id)
	}
	return ids
}

// DEPRECATED, currently returns an address iff we are connected to id.
func (m *peerManager[C]) Addresses(id types.NodeID) []NodeAddress {
	if conn,ok := GetAny(m.Conns(),id); ok {
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
