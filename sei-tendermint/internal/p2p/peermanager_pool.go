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

type poolConfig struct {
	selfID   types.NodeID
	maxConns utils.Option[int]
	maxAddrs utils.Option[int]
}

// Dialing persistent peers: use config.persistentAddrs
// Dialing regular peers: use config.bootstrapAddrs + pex of regular peers + pex of persistent peers
type poolConn[C peerConn] struct {
	conn utils.Option[C]
	pexAddrs []NodeAddress 
	dialing bool // connection can be dialing/connected/dead
}

type pool[C peerConn] struct {
	poolConfig
	outbound int
	dialing int
	connected int
	conns    map[types.NodeID]*poolConn[C]
}

func newPool[C peerConn](cfg poolConfig) *pool[C] {
	return &pool[C]{
		poolConfig: cfg,
		conns:      map[types.NodeID]*poolConn[C]{},
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
	SelfAddr utils.Option[NodeAddress]
}

type peerConn interface {
	comparable
	Info() peerConnInfo
	Close()
}

var errSelfAddr = errors.New("self address is ignored")
var errDuplicate = errors.New("duplicate address for the same peer")
var errTooMany = errors.New("too many addresses in the peer manager")

// Sets pex addresses for the given peer.
// Noop if the peer is not in the pool.
// Returns an error iff the address could not be added.
func (p *pool[C]) SetPexAddrs(id types.NodeID, addrs []NodeAddress) {
	if c,ok := p.conns[id]; ok {
		c.pexAddrs = addrs
	}
}

func (p *pool[C]) DialFailed(addr NodeAddress) {
	c,ok := p.conns[addr.NodeID]
	if !ok || !c.dialing { return }
	c.dialing = false
	p.dialing -= 1
	// TODO: record the failure time.
}

func (p *pool[C]) Evict(id types.NodeID) {
	if conn, ok := p.getConn(id).Get(); ok {
		conn.Close()
	}
}

func (p *pool[C]) getConn(id types.NodeID) utils.Option[C] {
	if c,ok := p.conns[id]; ok {
		return c.conn
	}
	return utils.None[C]()
}

func (p *pool[C]) TryStartDial() (NodeAddress, bool) {
	// Round robin over addresses with priority>lowest(connections.priority), preferring with highest priority.
	// lowest is counted over all connections, but up to max outbound (if there is <maxOutbound connections, then lowest(...) == -inf)
	// Dial address will be provided by a background task of peermanager.
	// Candidates will be reevaluated after receiving from that task.
	// Maintain a set of addresses of peers (grouped by NodeID), ordered by priority.
	// Threshold changed -> see if there is sth in the collection above threshold.
	// Address added above the threshold -> ditto
	// NOTE that bootstrap peers are also included in this set. What about peerDB? We can include it as a dummy peer (old us).
	// We snapshot ALL pexAddresses above threshold, dedupe, dial them in order
	// After connecting:
	// * If max outbound has been reached, prune outbound connection with lowest priority.
	// * Otherwise if max connections has been reached, prune connection with lowest priority.
	// * Otherwise just add the new connection.
	// TODO: we should slow down dialing if there is maxOutbound outbound connections.
	// TODO: should we slow down dialing if there is maxConn total connections? // Slow down churn
	//   we should simulate process of having separate limits of inbound outbound with potentially overlapping pools.
	//   In this case inbound connections should sometimes count as outbound connections, in case they were overriden.
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
		oldIsOutbound := old.conn.Info().DialAddr.IsPresent()
		if oldIsOutbound != newIsOutbound && (info.ID < p.selfID) != newIsOutbound {
			return fmt.Errorf("duplicate connection from peer %q", info.ID)
		}
		old.conn.Close()
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
	p.conns[info.ID] = &poolConn[C]{conn:conn}
	return nil
}

func (p *pool[C]) Disconnected(conn C) {
	info := conn.Info()
	if old, ok := p.conns[info.ID]; ok && old.conn == conn {
		old.conn.Close()
		delete(p.conns, info.ID)
		if info.DialAddr.IsPresent() {
			p.outbound -= 1
		}
	}
}
