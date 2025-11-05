package p2p

import (
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
	// * len(peers) <= maxPeers
	// * len(peers[id].addrs) <= maxAddrsPerPeer
	peers map[types.NodeID]*peerAddrs
	reserved map[types.NodeID]NodeAddress
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
		// Find some unreserved peer with all addresses failed and replace it.
		if _,ok := p.reserved[old]; !ok && len(peer.addrs)==len(peer.fails) {
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

func (p *poolInner) TryAcquire() utils.Option[NodeAddress] {
	// Choose peer with the oldest lastFail.
	var bestPeer utils.Option[*peerAddrs]
	for id,peer := range p.peers {
		if _,ok := p.reserved[id]; ok { continue }
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

// TODO: Separately store successful dials.
func (p *poolInner) Release(addr NodeAddress, connected bool) {
	if p.reserved[addr.NodeID] != addr { panic("Releasing unallocated addr") }
	delete(p.reserved, addr.NodeID)
	peer,ok := p.peers[addr.NodeID]
	if !ok { return }
	if connected {
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

func (p *poolInner) Drop(id types.NodeID) {
	delete(p.peers, id)
}

type pool struct { inner utils.Watch[*poolInner] }

func newPool(options poolOptions) *pool {
	inner := &poolInner{
		options: options,
		peers: map[types.NodeID]*peerAddrs{},
		reserved: map[types.NodeID]NodeAddress{},
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

func (p *pool) Drop(id types.NodeID) {
	for inner,ctrl := range p.inner.Lock() {
		ctrl.Updated()
		inner.Drop(id)
	}
	panic("unreachable")

}

func (p *pool) Acquire(ctx context.Context) (NodeAddress,error) {
	for inner,ctrl := range p.inner.Lock() {
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
func (p *pool) Release(addr NodeAddress, ok bool) {
	for inner,ctrl := range p.inner.Lock() {
		inner.Release(addr, ok)
		ctrl.Updated()
	}
}
