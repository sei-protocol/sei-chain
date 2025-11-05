package p2p

import (
	"time"
	"context"

	"github.com/tendermint/tendermint/libs/utils/im"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/types"
)

const maxAddrsPerPeer = 10

type peerAddrs struct {
	persistent bool
	lastFail utils.Option[time.Time]
	// Invariants:
	// * fails is a subset of addrs.
	addrs map[NodeAddress]struct{}
	fails map[NodeAddress]time.Time
}

func newPeer() *peerAddrs {
	return &peerAddrs{
		addrs: map[NodeAddress]struct{}{},
		fails: map[NodeAddress]time.Time{},
	}
}

func (p *peerManagerInner) allocPeer(id types.NodeID) (*peerAddrs,bool) {
	if peer,ok := p.peers[id]; ok {
		return peer,true
	}
	if m,ok := p.options.maxPeers.Get(); !ok || len(p.peers) < m {
		p.peers[id] = newPeer()
		return p.peers[id],true
	}
	for old,peer := range p.peers {
		// Find some unreserved peer with all addresses failed and replace it.
		if _,ok := p.reserved[old]; !ok && len(peer.addrs)==len(peer.fails) {
			delete(p.peers, old)
			p.peers[id] = newPeer()
			return p.peers[id],true
		}
	}
	return nil,false
}

func (p *peerManagerInner) AddAddr(addr NodeAddress) bool {
	id := addr.NodeID
	peer,ok := p.allocPeer(id)
	if !ok || peer.persistent { return false }
	if _,ok := peer.addrs[addr]; ok { return false }
	if !ok || len(peer.addrs) < maxAddrsPerPeer {
		peer.addrs[addr] = struct{}{}
		return true
	}
	// Replace any failing address.
	for old,_ := range peer.fails {
		delete(peer.addrs,old)
		delete(peer.fails,old)
		peer.addrs[addr] = struct{}{}
		return true
	}
	return false
}

func before(a, b utils.Option[time.Time]) bool {
	at,ok := a.Get()
	if !ok { return false }
	bt,ok := b.Get()
	if !ok { return true }
	return at.Before(bt)
}

func get[K comparable, V any](m map[K]V, k K) utils.Option[V] {
	if v,ok := m[k]; ok {
		return utils.Some(v)
	}
	return utils.None[V]()
}

func (i *peerManagerInner) TryAcquirePersistent() {

}

func (i *peerManagerInner) TryAcquire() utils.Option[NodeAddress] {
	if len(i.dialing) + i.inboundConns>=i.options.MaxConnections ||
		len(i.dialing) >= i.options.MaxDials
	{
		return utils.None[NodeAddress]()
	}
	// Choose peer with the oldest lastFail.
	var bestPeer utils.Option[*peerAddrs]
	for id,peer := range p.peers {
		if _,ok := p.dialing[id]; ok { continue }
		if x,ok := bestPeer.Get(); !ok || before(peer.lastFail, x.lastFail) {
			bestPeer = utils.Some(peer)
		}
	}
	// Choose address with the oldest lastFail.
	var best utils.Option[NodeAddress]
	if peer,ok := bestPeer.Get(); ok {
		for addr,_ := range peer.addrs {
			if x,ok := best.Get(); !ok || before(get(peer.fails,addr), get(peer.fails,x)) {
				best = utils.Some(addr)
			}
		}
	}
	if x,ok := best.Get(); ok {
		p.reserved[x.NodeID] = x
	}
	return best
}

func (i *peerManagerInner) Release(addr NodeAddress, dialed bool) {
	if p.reserved[addr.NodeID] != addr { panic("Releasing unallocated addr") }
	delete(p.reserved, addr.NodeID)
	peer,ok := p.peers[addr.NodeID]
	if !ok { return }
	if dialed {
		// Clear the failure record for the peer and the address.
		peer.lastFail = utils.None[time.Time]()
		delete(peer.fails,addr)
	} else {
		// Record the failure time.
		now := time.Now()
		peer.lastFail = utils.Some(now)
		peer.fails[addr] = now
	}
}

func (p *peerManagerInner) Drop(id types.NodeID) {
	delete(p.peers, id)
}

type peerManagerInner struct {
	// TODO: consider replacing Connection with an interface
	// * Close()
	// * PeerInfo() types.NodeInfo
	// * DialAddr() utils.Option[NodeAddress]
	// to make it testable without router.
	conns utils.AtomicSend[im.Map[types.NodeID,*Connection]]
	regularConns int
	options *PeerManagerOptions
	addrs map[types.NodeID]*peerAddrs
	dialing map[types.NodeID]NodeAddress
	dialHistory utils.AtomicSend[im.Map[types.NodeID,dialTime]]
}

type PeerManager struct {
	inner utils.Watch[*peerManagerInner]
}

type PeerUpdates struct {
	recv utils.AtomicRecv[im.Map[types.NodeID,*Connection]]
	last map[types.NodeID]struct{}
}

func (m *PeerManager) Subscribe() *PeerUpdates {
	for inner := range m.inner.Lock() {
		return &PeerUpdates{
			recv: inner.conns.Subscribe(),
			last: map[types.NodeID]struct{},
		}
	}
	panic("unreachable")
}

func NewPeerManager(options *PeerManagerOptions) (*PeerManager,error) {
	return &PeerManager{
		inner: utils.NewWatch(&peerManagerInner{
			options: options,
			addrs: map[types.NodeID]*peerAddrs{},
		}),
		conns: utils.NewAtomicSend(im.NewMap[types.NodeID,*Connection]()),
		dialing: map[types.NodeID]NodeAddress{},
	}
}

func (m *PeerManager) AddAddrs(addrs []NodeAddress) {
	for inner,ctrl := range m.inner.Lock() {
		updated := false
		for _,addr := range addrs {
			updated = updated || inner.AddAddr(addr)
		}
		if updated { ctrl.Updated() }
	}
}

func (m *PeerManager) Drop(id types.NodeID) {
	for inner,ctrl := range p.inner.Lock() {
		ctrl.Updated()
		inner.Drop(id)
	}
	panic("unreachable")
}

func (m *PeerManager) ReserveDial(ctx context.Context) (NodeAddress,error) {
	for inner,ctrl := range m.inner.Lock() {
		for {
			if addr,ok := inner.TryAcquire().Get(); ok {
				return addr, nil
			}
			if err:=ctrl.Wait(ctx); err!=nil {
				return NodeAddress{}, err
			}
		}
	}
	panic("unreachable")
}

// Release releases the reservation made via Acquire.
// ok specifies whether the connection attempt was successful.
// Panics if the address was not reserved.
func (p *PeerManager) DialFailed(addr NodeAddress) {
	for inner,ctrl := range p.inner.Lock() {
		inner.Release(addr, dialed)
		ctrl.Updated()
	}
}

// registerConn adds the connection to the router's connection registry.
// It atomically checks maximum connection limit and duplicate connections.
func (m *PeerManager) Connected(c *Connection) (err error) {
	defer func(){ if err == nil { r.metrics.Peers.Add(1) } }()
	selfID := r.peerManager.options.SelfID
	peerID := c.PeerInfo().NodeID
	if peerID == selfID {
		return errors.New("connection to self")
	}
	r.conns.Update(func(reg connRegistry) (connRegistry,bool) {
		if old,ok := reg.conns.Get(peerID); ok {
			// * allow to override connections in the same direction.
			// * allow inbound conn to override outbound iff peerID > selfID.
			//   This resolves the situation when peers try to connect to each other
			//   at the same time.
			if old.dialAddr.IsPresent() != c.dialAddr.IsPresent() &&
				(peerID < selfID) != c.dialAddr.IsPresent() {
				err = fmt.Errorf("duplicate connection from peer %q", peerID)
				return connRegistry{},false
			}
			old.Close()
		} else if !r.peerManager.options.persistent(peerID) {
			if m,ok := r.options.MaxConnections.Get(); ok && reg.regularCount >= m {
				err = errors.New("too many connections")
				return connRegistry{},false
			}
			reg.regularCount += 1
		}
		reg.conns = reg.conns.Set(peerID, c)
		return reg,true
	})
	return
}

func (m *PeerManager) Disconnected(c *Connection) {
	r.metrics.Peers.Add(-1)
	peerID := c.PeerInfo().NodeID
	r.conns.Update(func(reg connRegistry) (connRegistry,bool) {
		if old, ok := reg.conns.Get(peerID); ok && old == c {
			old.Close()
			reg.regularCount -= 1
			reg.conns = reg.conns.Delete(peerID)
			return reg,true
		}
		return connRegistry{},false
	})
}

// Advertise returns a list of peer addresses to advertise to a peer.
func (m *PeerManager) Advertise(limit int) []NodeAddress {
	var addrs []NodeAddress

	// advertise ourselves, to let everyone know how to dial us back
	// and enable mutual address discovery
	if addr,ok := s.options.SelfAddress.Get(); ok {
		addrs = append(addrs, addr)
	}

	// TODO: add successful dial tracking to connRegistry.
	return addrs
}
