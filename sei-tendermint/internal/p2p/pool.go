package p2p

import (
	"fmt"
	"time"
	"context"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/types"
)

type peerAddrs struct {
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

type poolOptions struct {
	selfID types.NodeID
	maxPeers utils.Option[int]
	maxAddrsPerPeer utils.Option[int]
}

type poolInner struct {
	selfID types.NodeID
	options poolOptions
	// Invariants:
	// * dials[id] exists => peers[id].LastFail[dials[id]] == None
	// * len(peers) <= maxPeers
	// * len(peers[id].addrs) <= maxAddrsPerPeer
	peers map[types.NodeID]*peerAddrs
	dials map[types.NodeID]NodeAddress
	conns map[types.NodeID]*Connection
}

func (p *poolInner) allocPeer(id types.NodeID) (*peerAddrs,bool) {
	if peer,ok := p.peers[id]; ok {
		return peer,true
	}
	if m,ok := p.options.maxPeers.Get(); !ok || len(p.peers) < m {
		p.peers[id] = newPeer()
		return p.peers[id],true
	}
	for old,peer := range p.peers {
		// Replace some peer with all addresses failed.
		if len(peer.addrs)==len(peer.fails) {
			delete(p.peers, old)
			p.peers[id] = newPeer()
			return p.peers[id],true
		}
	}
	return nil,false
}

func (p *poolInner) AddAddr(addr NodeAddress) bool {
	id := addr.NodeID
	peer,ok := p.allocPeer(id)
	if !ok { return false }
	if _,ok := peer.addrs[addr]; ok { return false }
	if m,ok := p.options.maxAddrsPerPeer.Get(); !ok || len(peer.addrs) < m {
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

func (p *poolInner) Dial() utils.Option[NodeAddress] {
	// Choose peer with the oldest lastFail.
	var toDial utils.Option[*peerAddrs]
	for id,peer := range p.peers {
		if _,ok := p.conns[id]; ok { continue }
		if _,ok := p.dials[id]; ok { continue }
		if x,ok := toDial.Get(); !ok || before(peer.lastFail, x.lastFail) {
			toDial = utils.Some(peer)
		}
	}
	// Choose address with the oldest lastFail.
	var best utils.Option[NodeAddress]
	if peer,ok := toDial.Get(); ok {
		for addr,_ := range peer.addrs {
			if x,ok := best.Get(); !ok || before(get(peer.fails,addr), get(peer.fails,x)) {
				best = utils.Some(addr)
			}
		}
	}
	return best
}

// TODO: DialDone separate from AddConn causes a race condition.
// TODO: Do not remove dial entry after dial completion.
// TODO: Separately store successful dials.
func (p *poolInner) DialDone(addr NodeAddress, ok bool) {
	if p.dials[addr.NodeID] != addr { return }
	delete(p.dials, addr.NodeID)
	if ok { return }
	peer := p.peers[addr.NodeID]
	now := time.Now()
	peer.lastFail = utils.Some(now)
	peer.fails[addr] = now
}

func (p *poolInner) AddConn(conn *Connection) error {
	id := conn.PeerInfo().NodeID
	if old,ok := p.conns[id]; ok {
		old.Close()
		delete(p.conns, id)
	}
	if m,ok := p.options.maxConns.Get(); ok && len(p.conns) == m {
		return fmt.Errorf("dialed peer %q failed, already connected to maximum number of peers", id)
	}
	p.conns[id] = conn
	return nil
}

func (p *poolInner) DelConn(conn *Connection) {
	id := conn.PeerInfo().NodeID
	if old,ok := p.conns[id]; ok && old == conn {
		delete(p.conns, id)
	}
}

func (p *poolInner) Forget(id types.NodeID) {
	if conn,ok := p.conns[id]; ok {
		conn.Close()
		delete(p.conns, id)
	}
	delete(p.peers, id)
	delete(p.dials, id)
}

type pool struct { inner utils.Watch[*poolInner] }

func newPool(options poolOptions) *pool {
	inner := &poolInner{
		options: options,
		peers: map[types.NodeID]*peerAddrs{},
		dials: map[types.NodeID]NodeAddress{},
		conns: map[types.NodeID]*Connection{},
	}
	return &pool{ utils.NewWatch(inner) }
}

func (p *pool) AddAddrs(addrs []NodeAddress) {
	for inner,ctrl := range p.inner.Lock() {
		for _,addr := range addrs {
			if inner.AddAddr(addr) {
				ctrl.Updated()
			}
		}
	}
}

func (p *pool) Forget(id types.NodeID) {
	for inner,ctrl := range p.inner.Lock() {
		ctrl.Updated()
		inner.Forget(id)
	}
	panic("unreachable")

}

func (p *pool) AddConn(conn *Connection) error {
	for inner,ctrl := range p.inner.Lock() {
		ctrl.Updated()
		return inner.AddConn(conn)
	}
	panic("unreachable")
}

func (p *pool) DelConn(conn *Connection) {
	for inner,ctrl := range p.inner.Lock() {
		ctrl.Updated()
		inner.DelConn(conn)
	}
}

func (p *pool) Connected(id types.NodeID) bool {
	for inner := range p.inner.Lock() {
		_,ok := inner.conns[id]
		return ok
	}
	panic("unreachable")
}

func (p *pool) DialNext(ctx context.Context) (NodeAddress,error) {
	for inner,ctrl := range p.inner.Lock() {
		for {
			if addr,ok := inner.Dial().Get(); ok {
				return addr, nil
			}
			if err:=ctrl.Wait(ctx); err!=nil {
				return NodeAddress{}, err
			}
		}
	}
	panic("unreachable")
}

func (p *pool) DialDone(addr NodeAddress, err error) {
	for inner,ctrl := range p.inner.Lock() {
		inner.DialDone(addr, err==nil)
		if utils.ErrorAs[errBadNetwork](err).IsPresent() {
			inner.Forget(addr.NodeID)
		}
		ctrl.Updated()
	}
}
