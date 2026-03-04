package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
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
	minLifetime time.Duration
}

func (cfg *poolConfig) getMaxOutbound() utils.Option[int] {
	if cfg.maxOutbound.IsPresent() { return cfg.maxOutbound }
	return cfg.maxConns
}

type poolConns[C peerConn] struct {
	in map[pNodeID]C
	out ordered.Map[pNodeID,C]	
	dialing map[pNodeID]NodeAddress
}

func (cs *poolConns[C]) Get(id pNodeID) (C,bool) {
	if c,ok := cs.in[id]; ok { return c,ok }
	return cs.out.Get(id)
}

func (cs *poolConns[C]) Busy(id pNodeID) bool {
	if _,ok := cs.dialing[id]; ok { return true }
	if _,ok := cs.in[id]; ok { return true }
	if _,ok := cs.out.Get(id); ok { return true }
	return false
}

type pool[C peerConn] struct {
	poolConfig
	// TODO: include bootstrapAddrs in makeDialQueue()
	// TODO: rename to regularConns
	// TODO: add persistentConns
	conns utils.Watch[*poolConns[C]]
	
	bootstrapAddrs []pNodeAddress
	pexAddrs utils.Watch[ordered.Map[pNodeID,[]pNodeAddress]]
	dialQueue chan pNodeAddress
}

func newPool[C peerConn](cfg poolConfig) *pool[C] {
	return &pool[C]{
		poolConfig: cfg,
		conns: utils.NewWatch(ordered.NewMap[pNodeID,C]()), 
	}
}

type peerConnInfo struct {
	ID       pNodeID
	Channels ChannelIDSet
	DialAddr utils.Option[NodeAddress]
	SelfAddr utils.Option[NodeAddress]
	ConnectedAt time.Time
}

type peerConn interface {
	comparable
	Info() peerConnInfo
	Close()
}

// Pushes pex addresses received from id. 
func (p *pool[C]) PushPexAddrs(id pNodeID, addrs []pNodeAddress) {
	for pexAddrs,ctrl := range p.pexAddrs.Lock() {
		pexAddrs.Set(id,addrs)
		if m,ok := p.maxConns.Get(); ok && pexAddrs.Len()>m {
			pexAddrs.PopMax()	
		}
		ctrl.Updated()
	}
}

func (p *pool[C]) Evict(id pNodeID) {
	for conns := range p.conns.Lock() {
		if c, ok := conns.Get(id); ok {
			c.Close()
		}
	}
}

func (p *pool[C]) SetDialing(addr pNodeAddress) bool {
	// Try to set dialing status.
	for conns,ctrl := range p.conns.Lock() {
		if conns.Busy(addr.pNodeID()) {
			return false
		}
		if m,ok := p.getMaxOutbound().Get(); ok && conns.out.Len()+len(conns.dialing)>=m {
			
		}

	}
}

func (p *pool[C]) Run(ctx context.Context, initialQueue []pNodeAddress) error {
	maxOutbound := p.getMaxOutbound()
	if maxOutbound==utils.Some(0) { return nil }

	q := utils.Slice(initialQueue)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if len(p.bootstrapAddrs)>0 {
			s.Spawn(func() error {
				const bootstrapInterval = 10 * time.Second
				for {
					for pexAddrs,ctrl := range p.pexAddrs.Lock() {
						// We use selfID as a placeholder to keep bootstrapAddrs and initialQueue.
						// This is not very elegant.
						pexAddrs.Set(pNodeID{priority:0,NodeID:p.selfID},p.bootstrapAddrs)
						ctrl.Updated()
					}
					if err:=utils.Sleep(ctx,bootstrapInterval); err!=nil {
						return err
					}
				}
			})
		}

		for {
			// Send addresses from the queue.
			for len(q)>0 {
				// Pop address.
				if len(q[0])==0 {
					q = q[1:]
					continue
				}
				addr := q[0][0]
				q[0] = q[0][1:]

				
				if err:=utils.Send(ctx,p.dialQueue,addr); err!=nil {
					return err
				}
			}

			// Repopulate the queue.
			q = nil
			for pexAddrs,ctrl := range p.pexAddrs.Lock() {
				if err:=ctrl.WaitUntil(ctx,func() bool { return pexAddrs.Len()>0 }); err!=nil {
					return err
				}
				for _,addrs := range pexAddrs.All() {
					q = append(q,addrs)
				}
				pexAddrs.Clear()
			}
		}
	})
}

func getOpt[K comparable, V any](m map[K]V,k K) utils.Option[V] {
	if v,ok := m[k]; ok { return utils.Some(v) }
	return utils.None[V]()
}

func (p *pool[C]) canDialAfter(conns *poolConns[C]) utils.Option[time.Time] {
	m,ok := p.getMaxOutbound().Get()
	if !ok || conns.out.Len()<m { return utils.Some(time.Now()) }
	_,conn,ok := conns.out.Max()
	if !ok { return utils.None[time.Time]() } // corner case where m == 0
	return utils.Some(conn.Info().ConnectedAt.Add(p.minLifetime))
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
