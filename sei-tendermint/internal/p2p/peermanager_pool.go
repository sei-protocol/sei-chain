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
	if old, ok := p.addrs[addr.NodeID]; ok {
		// Ignore duplicates.
		// Ignore if the previous address has not been tried out yet.
		if old.addr == addr || !old.lastFail.IsPresent() {
			return false
		}
		old.addr = addr
		old.lastFail = utils.None[time.Time]()
		return true
	}
	// Prune some peer if maxPeers limit has been reached.
	if m, ok := p.maxAddrs.Get(); ok && len(p.addrs) == m {
		toPrune, ok := p.findFailedPeer()
		if !ok {
			return false
		}
		delete(p.addrs, toPrune)
	}
	p.addrs[addr.NodeID] = &peerAddr{addr: addr}
	return true
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

func (p *pool[C]) findFailedPeer() (types.NodeID, bool) {
	// It doesn't matter what was the time of the failure,
	// since lastFail time is used just for round robin ordering.
	for old, pa := range p.addrs {
		if pa.lastFail.IsPresent() {
			return old, true
		}
	}
	return "", false
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
