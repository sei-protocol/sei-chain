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
	selfID   types.NodeID
	maxConns utils.Option[int]
	maxAddrs utils.Option[int]
}

type pool[C peerConn] struct {
	poolConfig

	outbound int
	conns    map[types.NodeID]C
	addrs    map[types.NodeID]*peerAddr
	dialing  map[types.NodeID]NodeAddress
}

func newPool[C peerConn](cfg poolConfig) *pool[C] {
	return &pool[C]{
		poolConfig: cfg,
		conns:      map[types.NodeID]C{},
		addrs:      map[types.NodeID]*peerAddr{},
		dialing:    map[types.NodeID]NodeAddress{},
	}
}

type peerAddr struct {
	lastFail utils.Option[time.Time]
	addr     NodeAddress
	isPublic bool
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

var errSelfAddr = errors.New("self address is ignored")
var errDuplicate = errors.New("duplicate address for the same peer")
var errTooMany = errors.New("too many addresses in the peer manager")

// AddAddr adds an address to the pool.
// Returns an error iff the address could not be added.
func (p *pool[C]) AddAddr(addr NodeAddress) error {
	pa := &peerAddr{addr: addr, isPublic: addr.IsPublic()}
	// Ignore address to self.
	if addr.NodeID == p.selfID {
		return errSelfAddr
	}
	if old, ok := p.addrs[addr.NodeID]; ok {
		// Ignore duplicates.
		if old.addr == addr {
			return errDuplicate
		}
		// If the old address failed, prune it.
		if old.lastFail.IsPresent() {
			p.addrs[pa.addr.NodeID] = pa
			return nil
		}
		// Prune private address, if we insert a public address
		if !old.isPublic && addr.IsPublic() {
			p.addrs[pa.addr.NodeID] = pa
			return nil
		}
		// Otherwise, do not replace
		return errDuplicate
	}
	// If there limit on addresses has not been reached, allow the new address.
	if maxAddrs, ok := p.maxAddrs.Get(); !ok || len(p.addrs) < maxAddrs {
		p.addrs[pa.addr.NodeID] = pa
		return nil
	}
	// Find any failed address to prune.
	// It doesn't matter what was the time of the failure,
	// since lastFail time is used just for round robin ordering.
	for id, old := range p.addrs {
		if old.lastFail.IsPresent() {
			delete(p.addrs, id)
			p.addrs[pa.addr.NodeID] = pa
			return nil
		}
	}
	// If the new address is public, find a private address to prune.
	if addr.IsPublic() {
		for id, old := range p.addrs {
			if !old.isPublic {
				delete(p.addrs, id)
				p.addrs[pa.addr.NodeID] = pa
				return nil
			}
		}
	}
	// Nothing can be pruned.
	return errTooMany
}

func (p *pool[C]) DialFailed(addr NodeAddress) {
	// Clear dialing status.
	if p.dialing[addr.NodeID] == addr {
		delete(p.dialing, addr.NodeID)
	}
	// Record the failure time.
	if peerAddr, ok := p.addrs[addr.NodeID]; ok && peerAddr.addr == addr {
		peerAddr.lastFail = utils.Some(time.Now())
	}
}

func (p *pool[C]) Evict(id types.NodeID) {
	delete(p.addrs, id)
	if conn, ok := p.conns[id]; ok {
		conn.Close()
	}
}

func (p *pool[C]) TryStartDial() (NodeAddress, bool) {
	// Check the connections limit.
	if m, ok := p.maxConns.Get(); ok && len(p.dialing)+len(p.conns) >= m {
		return NodeAddress{}, false
	}

	// Choose peer with the oldest lastFail.
	var best utils.Option[*peerAddr]
	for id, peerAddrs := range p.addrs {
		if _, ok := p.dialing[id]; ok {
			continue
		}
		if _, ok := p.conns[id]; ok {
			continue
		}
		if x, ok := best.Get(); !ok || before(peerAddrs.lastFail, x.lastFail) {
			best = utils.Some(peerAddrs)
		}
	}
	x, ok := best.Get()
	if !ok {
		return NodeAddress{}, false
	}
	// clear the failed status for the chosen address and mark it as dialing.
	p.addrs[x.addr.NodeID].lastFail = utils.None[time.Time]()
	p.dialing[x.addr.NodeID] = x.addr
	return x.addr, true
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
		delete(p.conns, info.ID)
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
