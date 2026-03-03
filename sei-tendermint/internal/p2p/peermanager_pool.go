package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"
	"slices"
	"maps"
	"cmp"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/ordered"
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
	maxOutbound utils.Option[int]
	maxConns utils.Option[int]
	maxAddrs utils.Option[int]
}

func (cfg *poolConfig) getMaxOutbound() utils.Option[int] {
	if cfg.maxOutbound.IsPresent() { return cfg.maxOutbound }
	return cfg.maxConns
}

// Dialing persistent peers: use config.persistentAddrs
// Dialing regular peers: use config.bootstrapAddrs + pex of regular peers + pex of persistent peers
type poolConn[C peerConn] struct {
	conn C
	pexAddrs []pNodeAddress 
}

type pool[C peerConn] struct {
	poolConfig
	dialQueue chan pNodeAddress
	// TODO: include bootstrapAddrs in makeDialQueue()
	bootstrapAddrs []pNodeAddress
	// TODO: rename to regularConns
	conns utils.Watch[ordered.Map[pNodeID,*poolConn[C]]]
	// TODO: add persistentConns
}

func newPool[C peerConn](cfg poolConfig) *pool[C] {
	return &pool[C]{
		poolConfig: cfg,
		conns: utils.NewWatch(ordered.NewMap[pNodeID,*poolConn[C]]()), 
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
func (p *pool[C]) SetPexAddrs(id pNodeID, addrs []pNodeAddress) {
	for conns := range p.conns.Lock() {
		if c,ok := conns.Get(id); ok { c.pexAddrs = addrs }
	}
}

func (p *pool[C]) Evict(id pNodeID) {
	for conns := range p.conns.Lock() {
		if c, ok := conns.Get(id); ok {
			c.conn.Close()
		}
	}
}

func (p *pool[C]) priorityThreshold(conns ordered.Map[pNodeID,*poolConn[C]]) uint64 {
	inf := utils.Max[uint64]() 
	m,ok := p.getMaxOutbound().Get()
	if !ok { return inf }
	id,_,ok := conns.GetAt(m)
	if !ok { return inf }
	return id.priority 
}

func (p *pool[C]) makeDialQueue(ctx context.Context) ([]pNodeAddress,error) {
	// We rate-limit proportionally to the amount of work done,
	// with a large factor (1ms) per address. For example with 100 connections, and
	// max 100 pex addresses per connection: 100 * 100 * 1ms = 10s.
	// This allows us to amortize the work done.
	const delayPerWorkUnit = time.Millisecond
	for {
		workDone := 0
		start := time.Now()
		for conns,ctrl := range p.conns.Lock() {
			t := p.priorityThreshold(conns)
			addrs := map[pNodeAddress]struct{}{}
			for _,c := range conns.All() {
				for _,addr := range c.pexAddrs {
					if _,ok := conns.Get(addr.pNodeID()); ok || addr.priority >= t { continue }
					addrs[addr] = struct{}{}
				}
			}
			workDone = len(addrs)
			if q := slices.SortedFunc(maps.Keys(addrs),func(a,b pNodeAddress) int {
				return cmp.Compare(a.priority,b.priority)
			}); len(q)>0 {
				return q,nil
			}
			// Wait for updates, since there is nothing to do otherwise.
			if err:=ctrl.Wait(ctx); err!=nil {
				return nil,err
			}
		}
		// Amortize the work by sleeping.
		// TODO: It is still possible that the returned addresses will be just filtered
		// out after return, which would skip this amortization.
		if err:=utils.SleepUntil(ctx,start.Add(delayPerWorkUnit * time.Duration(workDone))); err!=nil {
			return nil,err
		}
	}
}

// TODO: initialize dialQueue with peerDB content.
func (p *pool[C]) Run(ctx context.Context) error {
	for {
		// Construct a queue
		q,err := p.makeDialQueue(ctx)
		if err!=nil { return err }
		// Feed it to dialers.
		for _,addr := range q {
			if err:=utils.Send(ctx,p.dialQueue,addr); err!=nil {
				return err
			}
		}
	}
}

func (p *pool[C]) canDial(id pNodeID) bool {
	for conns := range p.conns.Lock() {
		_,ok := conns.Get(id)
		return !ok && p.priorityThreshold(conns) <= id.priority
	}
	panic("unreachable")
}

func (p *pool[C]) StartDial(ctx context.Context) (NodeAddress, error) {
	// TODO: rate limit dialing here?.
	for {
		// What about concurrent dialing of the same peer?
		// Even if we selected just 1 addr per NodeID,
		// same address might be dialed twice in the current impl, since we
		// regenerate the queue periodically.
		addr,err := utils.Recv(ctx,p.dialQueue)
		if err!=nil { return NodeAddress{},err }
		if p.canDial(addr.pNodeID()) {
			return addr.NodeAddress,nil
		}
	}
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
	id := pNodeID{priority:0,NodeID:info.ID}
	newIsOutbound := info.DialAddr.IsPresent()
	for conns := range p.conns.Lock() {
		if old,ok := conns.Get(id); ok {
			// * allow to override connections in the same direction.
			// * inbound priority > outbound priority <=> peerID > selfID.
			//   This resolves the situation when peers try to connect to each other
			//   at the same time.
			oldIsOutbound := old.conn.Info().DialAddr.IsPresent()
			if oldIsOutbound != newIsOutbound && (info.ID < p.selfID) != newIsOutbound {
				return fmt.Errorf("duplicate connection from peer %q", info.ID)
			}
			old.conn.Close()
			conns.Set(id,&poolConn[C]{conn:conn})
			return nil
		}
		if newIsOutbound {
			// For outbound peers we require them to have high priority.
			if p.priorityThreshold(conns) <= id.priority {
				return fmt.Errorf("too low priority peer")
			}
		}	
		if m, ok := p.maxConns.Get(); ok && conns.Len() >= m {
			// If we are out of capacity, we need to drop peer with lowest priority
			oldID,old,ok := conns.Max()
			if !ok || oldID.priority <= id.priority {
				return errors.New("too many connections")
			}
			old.conn.Close()
			conns.Delete(oldID)
		}
		conns.Set(id,&poolConn[C]{conn:conn})
	}
	return nil
}

func (p *pool[C]) Disconnected(conn C) {
	info := conn.Info()
	id := pNodeID{priority:0,NodeID:info.ID}
	for conns := range p.conns.Lock() {
		if old, ok := conns.Get(id); ok && old.conn == conn {
			old.conn.Close()
			conns.Delete(id)
		}
	}
}
