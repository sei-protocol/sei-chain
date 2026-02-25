package p2p

import (
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

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

func getOpt[K comparable, V any](m map[K]V, k K) utils.Option[V] {
	if v, ok := m[k]; ok {
		return utils.Some(v)
	}
	return utils.None[V]()
}

type poolConfig struct {
	selfID          types.NodeID
	maxConns        utils.Option[int]
	maxAddrs        utils.Option[int]
	maxAddrsPerPeer utils.Option[int]
}

type pool[C peerConn] struct {
	poolConfig

	outbound int
	conns    map[types.NodeID]C
	addrs    map[types.NodeID]*peerAddrs
	dialing  map[types.NodeID]NodeAddress
}

func newPool[C peerConn](cfg poolConfig) *pool[C] {
	return &pool[C]{
		poolConfig: cfg,
		conns:      map[types.NodeID]C{},
		addrs:      map[types.NodeID]*peerAddrs{},
		dialing:    map[types.NodeID]NodeAddress{},
	}
}

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
}

type peerConn interface {
	comparable
	Info() peerConnInfo
	Close()
}

// Returns true iff the address was actually added.
func (p *pool[C]) AddAddr(addr NodeAddress) bool {
	// Ignore self.
	if addr.NodeID == p.selfID {
		return false
	}
	pa, ok := p.addrs[addr.NodeID]
	// Add new peerAddrs if missing.
	if !ok {
		// Prune some peer if maxPeers limit has been reached.
		if m, ok := p.maxAddrs.Get(); ok && len(p.addrs) == m {
			toPrune, ok := p.findFailedPeer()
			if !ok {
				return false
			}
			delete(p.addrs, toPrune)
		}
		pa = newPeerAddrs()
		p.addrs[addr.NodeID] = pa
	}
	// Ignore duplicate address.
	if _, ok := pa.addrs[addr]; ok {
		return false
	}
	// Prune any failing address if maxAddrsPerPeer has been reached.
	if m, ok := p.maxAddrsPerPeer.Get(); ok && len(pa.addrs) >= m {
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

func (p *pool[C]) DialFailed(addr NodeAddress) {
	if p.dialing[addr.NodeID] == addr {
		delete(p.dialing, addr.NodeID)
	}
	peerAddrs, ok := p.addrs[addr.NodeID]
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

func (p *pool[C]) Evict(id types.NodeID) {
	delete(p.addrs, id)
	if conn, ok := p.conns[id]; ok {
		conn.Close()
	}
}

func (p *pool[C]) findFailedPeer() (types.NodeID, bool) {
	for old, pa := range p.addrs {
		if len(pa.fails) < len(pa.addrs) {
			continue
		}
		return old, true
	}
	return "", false
}

func (p *pool[C]) TryStartDial() (NodeAddress, bool) {
	// Check the connections limit.
	if m, ok := p.maxConns.Get(); ok && len(p.dialing)+len(p.conns) >= m {
		return NodeAddress{}, false
	}

	// Choose peer with the oldest lastFail.
	var bestPeer utils.Option[*peerAddrs]
	for id, peerAddrs := range p.addrs {
		if _, ok := p.dialing[id]; ok {
			continue
		}
		if _, ok := p.conns[id]; ok {
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
			if x, ok := best.Get(); !ok || before(getOpt(peer.fails, addr), getOpt(peer.fails, x)) {
				best = utils.Some(addr)
			}
		}
	}
	if x, ok := best.Get(); ok {
		// clear the failed status for the chosen address and mark it as dialing.
		delete(p.addrs[x.NodeID].fails, x)
		p.dialing[x.NodeID] = x
	}
	return best.Get()
}

func (p *pool[C]) Connected(conn C) (err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	info := conn.Info()
	if info.ID == p.selfID {
		return errors.New("connection to self")
	}
	if addr, ok := info.DialAddr.Get(); ok && p.dialing[addr.NodeID] == addr {
		delete(p.dialing, addr.NodeID)
	}
	newIsOutbound := info.DialAddr.IsPresent()
	if old, ok := p.conns[info.ID]; ok {
		// * allow to override connections in the same direction.
		// * inbound priority > outbound priority <=> peerID > selfID.
		//   This resolves the situation when peers try to connect to each other
		//   at the same time.
		oldIsOutbound := old.Info().DialAddr.IsPresent()
		if oldIsOutbound != newIsOutbound && (info.ID < p.selfID) != newIsOutbound {
			return fmt.Errorf("duplicate connection from peer %q", info.ID)
		}
		old.Close()
		delete(p.conns,info.ID)
		if oldIsOutbound {
			p.outbound -= 1
		}
	}
	if m, ok := p.maxConns.Get(); ok && len(p.conns) >= m {
		return errors.New("too many connections")
	}
	if newIsOutbound {
		p.outbound += 1
	}
	p.conns[info.ID] = conn
	return nil
}

func (p *pool[C]) Disconnected(conn C) {
	info := conn.Info()
	if old, ok := p.conns[info.ID]; ok && old == conn {
		old.Close()
		delete(p.conns, info.ID)
		if info.DialAddr.IsPresent() {
			p.outbound -= 1
		}
	}
}
